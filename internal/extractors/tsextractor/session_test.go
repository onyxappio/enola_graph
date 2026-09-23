package tsextractor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractSession_ResultDoesNotMutateCachedRecords(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"a.ts": "export function a() { return 1; }",
		"b.ts": "import { a } from './a'; export function b() { return a(); }",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ext := New()
	files := []string{"a.ts", "b.ts"}
	first, err := ext.ExtractSession(context.Background(), dir, files, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(first.Records)
	if err != nil {
		t.Fatal(err)
	}
	for _, dirty := range []map[string]bool{{}, {"b.ts": true}} {
		next, err := ext.ExtractSession(context.Background(), dir, files, first.Records, dirty, SessionHooks{})
		if err != nil {
			t.Fatal(err)
		}
		props, relations := 0, 0
		for i := range next.Facts {
			f := &next.Facts[i]
			if f.Props != nil {
				f.Props["mutation_probe"] = true
				props++
			}
			for j := range f.Relations {
				f.Relations[j].Target = "mutation_probe"
				relations++
			}
		}
		if props == 0 || relations == 0 {
			t.Fatal("fixture must exercise both properties and relations")
		}
		after, err := json.Marshal(first.Records)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("composition/output mutations escaped into cached records")
		}
	}
}

func TestExtractSession_ReusesUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"t"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mustWrite := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("src/a.ts", "export function a() { return 1; }\n")
	mustWrite("src/b.ts", "import { a } from './a'; export function b() { return a(); }\n")
	files := []string{"src/a.ts", "src/b.ts", "tsconfig.json", "package.json"}
	ext := New()
	first, err := ext.ExtractSession(context.Background(), dir, files, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Stats.FilesParsed < 2 {
		t.Fatalf("first parse = %d", first.Stats.FilesParsed)
	}
	second, err := ext.ExtractSession(context.Background(), dir, files, first.Records, map[string]bool{}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Stats.FilesParsed != 0 {
		t.Fatalf("warm session parsed %d, want 0", second.Stats.FilesParsed)
	}
	if second.Stats.CachedFiles < 2 {
		t.Fatalf("cached = %d", second.Stats.CachedFiles)
	}
	mustWrite("src/b.ts", "import { a } from './a'; export function b() { return a() + 1; }\n")
	third, err := ext.ExtractSession(context.Background(), dir, files, first.Records, map[string]bool{"src/b.ts": true}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if third.Stats.FilesParsed != 1 {
		t.Fatalf("dirty b.ts parsed %d files, want 1", third.Stats.FilesParsed)
	}
}

func writeSessionFiles(t *testing.T, dir string, files map[string]string) []string {
	t.Helper()
	var names []string
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, rel)
	}
	return names
}

func sessionRecord(t *testing.T, files map[string]string, owner string) *FileRecord {
	t.Helper()
	dir := t.TempDir()
	names := writeSessionFiles(t, dir, files)
	res, err := New().ExtractSession(context.Background(), dir, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	rec := res.Records[owner]
	if rec == nil {
		t.Fatalf("no record for %s", owner)
	}
	return rec
}

func hasString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestExtractSession_ImportReplaySpecRetainsFolderStem(t *testing.T) {
	rec := sessionRecord(t, map[string]string{
		"foo/index.ts": "export const value = 'index';\n",
		"use.ts":       "import { value } from './foo';\nexport const used = value;\n",
		"other.ts":     "export const other = 1;\n",
	}, "use.ts")
	if !hasString(rec.ImportSpecs, "foo") {
		t.Fatalf("ImportSpecs = %v, want replay stem foo", rec.ImportSpecs)
	}
	if hasString(rec.ImportSpecs, "foo/index.ts") {
		t.Fatalf("ImportSpecs replaced the replay stem with exact target: %v", rec.ImportSpecs)
	}
	if !hasString(rec.ResolvedFiles, "foo/index.ts") {
		t.Fatalf("ResolvedFiles = %v, want exact foo/index.ts", rec.ResolvedFiles)
	}
	var sawSpec, sawFile bool
	for _, f := range rec.Facts {
		if f.PropString("import_spec") == "foo" {
			sawSpec = true
		}
		if f.PropString("target_file") == "foo/index.ts" {
			sawFile = true
		}
	}
	if !sawSpec || !sawFile {
		t.Fatalf("facts missing import_spec/target_file pair (spec=%v file=%v)", sawSpec, sawFile)
	}
}

func TestExtractSession_ExplicitIndexImportKeepsIndexStem(t *testing.T) {
	rec := sessionRecord(t, map[string]string{
		"foo/index.ts": "export const value = 'index';\n",
		"use.ts":       "import { value } from './foo/index';\nexport const used = value;\n",
	}, "use.ts")
	if !hasString(rec.ImportSpecs, "foo/index") {
		t.Fatalf("ImportSpecs = %v, want explicit foo/index", rec.ImportSpecs)
	}
	if hasString(rec.ImportSpecs, "foo") && !hasString(rec.ImportSpecs, "foo/index") {
		t.Fatalf("explicit index import collapsed to folder stem: %v", rec.ImportSpecs)
	}
	if !hasString(rec.ResolvedFiles, "foo/index.ts") {
		t.Fatalf("ResolvedFiles = %v, want foo/index.ts", rec.ResolvedFiles)
	}
}

func TestExtractSession_AliasReplaySpecIsNormalizedTarget(t *testing.T) {
	rec := sessionRecord(t, map[string]string{
		"tsconfig.json":         `{"compilerOptions":{"paths":{"@lib/util":["src/lib/util/index.ts"]}}}`,
		"src/lib/util/index.ts": "export const n = 1;\n",
		"src/app.ts":            "import { n } from '@lib/util';\nexport const v = n;\n",
	}, "src/app.ts")
	if !hasString(rec.ImportSpecs, "src/lib/util/index.ts") {
		t.Fatalf("alias ImportSpecs = %v, want normalized src/lib/util/index.ts", rec.ImportSpecs)
	}
	if hasString(rec.ImportSpecs, "@lib/util") {
		t.Fatalf("alias specifier was not normalized: %v", rec.ImportSpecs)
	}
	if !hasString(rec.ResolvedFiles, "src/lib/util/index.ts") {
		t.Fatalf("ResolvedFiles = %v, want exact alias file", rec.ResolvedFiles)
	}
}

func TestExtractSession_FileModuleNotReboundByFolderIndex(t *testing.T) {
	rec := sessionRecord(t, map[string]string{
		"bar.ts":       "export const value = 'file';\n",
		"bar/index.ts": "export const value = 'index';\n",
		"use.ts":       "import { value } from './bar';\nexport const used = value;\n",
	}, "use.ts")
	if !hasString(rec.ImportSpecs, "bar") {
		t.Fatalf("ImportSpecs = %v, want replay stem bar", rec.ImportSpecs)
	}
	if !hasString(rec.ResolvedFiles, "bar.ts") {
		t.Fatalf("ResolvedFiles = %v, want file module bar.ts over folder index", rec.ResolvedFiles)
	}
	if hasString(rec.ResolvedFiles, "bar/index.ts") {
		t.Fatalf("folder index must not win over existing file module: %v", rec.ResolvedFiles)
	}
}

func TestSessionFilesExcludesNonAngularHTML(t *testing.T) {
	files := []string{"src/a.ts", "src/page.html", "readme.md"}
	got := SessionFiles(files, false)
	if len(got) != 1 || got[0] != "src/a.ts" {
		t.Fatalf("non-angular session files = %v, want [src/a.ts]", got)
	}
	got = SessionFiles(files, true)
	if len(got) != 2 {
		t.Fatalf("angular session files = %v, want ts+html", got)
	}
}
