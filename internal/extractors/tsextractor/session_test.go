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
