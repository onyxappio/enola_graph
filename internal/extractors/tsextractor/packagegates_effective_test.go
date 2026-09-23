package tsextractor

import (
	"reflect"
	"testing"
)

// effectiveFor is the one place the owning package of a file is decided, and
// both the extraction (through forFile) and the session context projection go
// through it. These cases pin the selection rule itself, so a change to it fails
// here rather than as a parse count somewhere downstream.
func TestPackageGatesEffectiveForSelectsNearestOwningPackage(t *testing.T) {
	gates := packageGates{byDir: map[string]pkgGate{
		"":                              {Vue: true},
		"packages/database":             {Drizzle: true},
		"packages/database/nested":      {TypeORM: true},
		"packages/database/nested/deep": {},
	}}

	for _, tc := range []struct {
		file   string
		pkgDir string
		gate   pkgGate
	}{
		{"index.ts", "", pkgGate{Vue: true}},
		{"src/app/main.ts", "", pkgGate{Vue: true}},
		{"packages/api/handler.ts", "", pkgGate{Vue: true}},
		{"packages/database/schema.ts", "packages/database", pkgGate{Drizzle: true}},
		{"packages/database/src/deep/schema.ts", "packages/database", pkgGate{Drizzle: true}},
		// The nearest manifest wins outright: the nested package declares TypeORM
		// and does not inherit its parent's Drizzle or the root's Vue.
		{"packages/database/nested/schema.ts", "packages/database/nested", pkgGate{TypeORM: true}},
		// An empty declaration is still a declaration, and shadows everything above.
		{"packages/database/nested/deep/schema.ts", "packages/database/nested/deep", pkgGate{}},
	} {
		dir, gate, found := gates.effectiveFor(tc.file)
		if !found || dir != tc.pkgDir || gate != tc.gate {
			t.Errorf("effectiveFor(%q) = %q,%+v,%v; want %q,%+v,true", tc.file, dir, gate, found, tc.pkgDir, tc.gate)
		}
	}
}

// With no root manifest the files no package claims have no gate at all, rather
// than inheriting the first one the walk happens to find.
func TestPackageGatesEffectiveForReportsNoOwningPackage(t *testing.T) {
	gates := packageGates{byDir: map[string]pkgGate{"packages/database": {Drizzle: true}}}
	for _, file := range []string{"index.ts", "src/main.ts", "packages/api/handler.ts"} {
		if dir, gate, found := gates.effectiveFor(file); found {
			t.Errorf("effectiveFor(%q) claimed package %q %+v with no manifest above it", file, dir, gate)
		}
	}
}

// forFile is what extractFile is handed and the session context projects, so the
// two must be the same call. Prisma is deliberately not among the per-file
// values: no per-file reader consumes it, only the repository-wide anyPrisma.
func TestPackageGatesForFileMatchesEffectiveDeclaration(t *testing.T) {
	gates := packageGates{
		byDir: map[string]pkgGate{
			"":                  {Vue: true, Prisma: true},
			"packages/database": {Drizzle: true, TypeORM: true},
		},
		anyPrisma: true,
	}
	for _, tc := range []struct {
		file string
		orms ormFlags
		vue  bool
	}{
		{"src/main.ts", ormFlags{}, true},
		{"packages/database/schema.ts", ormFlags{typeORM: true, drizzle: true}, false},
	} {
		orms, vue := gates.forFile(nil, tc.file)
		if !reflect.DeepEqual(orms, tc.orms) || vue != tc.vue {
			t.Errorf("forFile(%q) = %+v,%v; want %+v,%v", tc.file, orms, vue, tc.orms, tc.vue)
		}
		_, gate, _ := gates.effectiveFor(tc.file)
		if gate.Vue != vue || gate.TypeORM != orms.typeORM || gate.Drizzle != orms.drizzle {
			t.Errorf("forFile(%q) disagrees with the declaration it selected: %+v vs %+v/%v", tc.file, gate, orms, vue)
		}
	}
}
