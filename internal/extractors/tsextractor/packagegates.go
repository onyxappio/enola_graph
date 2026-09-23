package tsextractor

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/factpath"
)

type pkgGate struct {
	Vue     bool `json:"vue"`
	TypeORM bool `json:"typeorm"`
	Drizzle bool `json:"drizzle"`
	Prisma  bool `json:"prisma"`
}

// packageGates records framework/ORM declarations per owning package directory.
type packageGates struct {
	byDir     map[string]pkgGate
	anyPrisma bool
}

func collectPackageGates(ctx context.Context, repoPath string, inputScopes ...*inputscope.Scope) packageGates {
	inputScope := inputscope.First(inputScopes)
	g := packageGates{byDir: map[string]pkgGate{}}
	for _, e := range sharedDiscoveryEntries(ctx, repoPath, inputScope) {
		if e.isDir || e.name != "package.json" {
			continue
		}
		data, err := overlayReadFile(ctx, e.path, inputScope)
		if err != nil {
			continue
		}
		var p map[string]any
		if err := json.Unmarshal(data, &p); err != nil || p == nil {
			continue
		}
		rel, err := filepath.Rel(repoPath, filepath.Dir(e.path)) //factpath:host
		if err != nil {
			continue
		}
		pkgDir := factpath.Slash(rel)
		gate := pkgGate{
			Vue:     pkgHasDep(p, "vue"),
			TypeORM: pkgHasDep(p, depTypeORM),
			Drizzle: pkgHasDep(p, depDrizzle),
			Prisma:  pkgHasDep(p, depPrisma),
		}
		g.byDir[pkgDir] = gate
		if gate.Prisma {
			g.anyPrisma = true
		}
	}
	return g
}

func pkgHasDep(p map[string]any, name string) bool {
	for _, key := range []string{"dependencies", "devDependencies", "peerDependencies"} {
		if deps, ok := p[key].(map[string]any); ok {
			if _, ok := deps[name]; ok {
				return true
			}
		}
	}
	return false
}

// effectiveFor selects the declaration an extraction will actually apply to
// relFile: the gate of the nearest owning package directory, walking up to the
// repository root. It reports the directory as well, so a caller that needs to
// say which package answered can do so without repeating the search.
//
// forFile and the session-context projection both go through this. They have to:
// a projection that re-derived the owning package on its own would be a second
// implementation of the selection rule, and the two would answer differently the
// first time one of them was changed.
func (g packageGates) effectiveFor(relFile string) (pkgDir string, gate pkgGate, found bool) {
	for d := filepath.ToSlash(factpath.Dir(relFile)); ; {
		if gate, ok := g.byDir[d]; ok {
			return d, gate, true
		}
		i := strings.LastIndexByte(d, '/')
		if i < 0 {
			if d == "." {
				if gate, ok := g.byDir[""]; ok {
					return "", gate, true
				}
				return "", pkgGate{}, false
			}
			d = "."
			continue
		}
		d = d[:i]
	}
}

func (g packageGates) forFile(_ map[string]string, relFile string) (orms ormFlags, isVue bool) {
	_, gate, _ := g.effectiveFor(relFile)
	return ormFlags{typeORM: gate.TypeORM, drizzle: gate.Drizzle}, gate.Vue
}
