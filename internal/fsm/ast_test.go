package fsm

import (
	"path/filepath"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestResolveExportFollowsDeclarationsAliasesAndBarrels(t *testing.T) {
	sources := map[string][]byte{
		"index.ts": []byte(`export { defineMachine } from './kernel';
export { localFactory as configuredFactory };
export * from './extra';`),
		"kernel.ts": []byte(`export function defineMachine() { return {}; }`),
		"extra.ts":  []byte(`export const extraFactory = () => ({});`),
		"local.ts": []byte(`function localFactory() { return {}; }
export { localFactory as configuredFactory };`),
	}
	read := func(file string) ([]byte, error) {
		b, ok := sources[slash(file)]
		if !ok {
			return nil, filepath.ErrBadPattern
		}
		return b, nil
	}
	resolve := func(from, spec string) (string, bool) {
		if len(spec) == 0 || spec[0] != '.' {
			return "", false
		}
		base := slash(filepath.Join(filepath.Dir(from), spec))
		if _, ok := sources[base+".ts"]; ok {
			return base + ".ts", true
		}
		return "", false
	}
	a := &Analyzer{read: read, resolve: resolve, cache: map[string][]byte{}}

	for _, tc := range []struct{ file, name, want string }{
		{"index.ts", "defineMachine", "kernel.ts#defineMachine"},
		{"index.ts", "extraFactory", "extra.ts#extraFactory"},
		{"local.ts", "configuredFactory", "local.ts#localFactory"},
	} {
		got, ok := a.resolveExport(tc.file, tc.name, map[string]bool{})
		if !ok || got != tc.want {
			t.Errorf("resolveExport(%q, %q) = %q, %v; want %q, true", tc.file, tc.name, got, ok, tc.want)
		}
	}
	if _, ok := a.resolveExport("index.ts", "missing", map[string]bool{}); ok {
		t.Fatal("resolveExport accepted a name not declared by any re-export")
	}
}

func TestTopLevelExportRecognizesFunctionAndVariableDeclarations(t *testing.T) {
	src := []byte(`export function run() {}
export const create = () => ({ nested: () => { const hiddenExport = 1; return hiddenExport; } });
function hidden() {}`)
	root, kinds, done := parse("test.ts", src)
	if done == nil {
		t.Fatal("parse failed")
	}
	defer done()
	for _, name := range []string{"run", "create"} {
		if !topLevelExport(root, name, src, kinds) {
			t.Errorf("topLevelExport did not recognize %q", name)
		}
	}
	if topLevelExport(root, "hidden", src, kinds) {
		t.Fatal("topLevelExport recognized a non-exported declaration")
	}
	if topLevelExport(root, "hiddenExport", src, kinds) {
		t.Fatal("topLevelExport treated an initializer-local declaration as exported")
	}
}

func TestHasLocalDeclarationIncludesExportWrappedDeclarationsOnly(t *testing.T) {
	sources := map[string][]byte{
		"test.ts": []byte(`export const createRegistration = () => ({
  nested: () => { const nestedOnly = 1; return nestedOnly; },
});
export function run() {}
const local = () => {};
export { local as registrationAlias };`),
	}
	a := NewAnalyzer(nil, []string{"test.ts"}, func(file string) ([]byte, error) {
		return sources[slash(file)], nil
	}, nil)
	for _, name := range []string{"createRegistration", "run", "local"} {
		if !a.hasLocalDeclaration("test.ts", name) {
			t.Errorf("hasLocalDeclaration did not find %q", name)
		}
	}
	for _, name := range []string{"nestedOnly", "registrationAlias", "missing"} {
		if a.hasLocalDeclaration("test.ts", name) {
			t.Errorf("hasLocalDeclaration accepted non-declaration %q", name)
		}
	}
	relation, ok := a.symbolRelation("test.ts", "createRegistration", "fsm_declared_in")
	if !ok || relation.TargetFile != "test.ts" || relation.Target != moduleName("test.ts")+".createRegistration" {
		t.Fatalf("exported registration source relation = %+v, %v", relation, ok)
	}
}

func TestModuleSpecUsesImportExportSourceField(t *testing.T) {
	src := []byte(`import config from './config.json' with { type: 'json' };
export { item } from './barrel';`)
	root, kinds, done := parse("module.ts", src)
	if done == nil {
		t.Fatal("parse failed")
	}
	defer done()
	got := map[string]string{}
	walk(root, func(n *sitter.Node) {
		switch kinds.Of(n) {
		case "import_statement", "export_statement":
			got[kinds.Of(n)] = moduleSpec(n, src, kinds)
		}
	})
	if got["import_statement"] != "./config.json" || got["export_statement"] != "./barrel" {
		t.Fatalf("module sources = %#v, want import ./config.json and export ./barrel", got)
	}
}
