package fsm

import (
	"path/filepath"
	"testing"
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
export const create = () => ({});
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
}
