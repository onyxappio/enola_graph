package tsextractor

import (
	"context"
	"encoding/json"
	"io/fs"
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
	_ = overlayWalkDir(ctx, repoPath, inputScope, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != repoPath && (strings.HasPrefix(name, ".") || tsSkipDirs[name] || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "package.json" {
			return nil
		}
		data, err := overlayReadFile(ctx, path, inputScope)
		if err != nil {
			return nil
		}
		var p map[string]any
		if err := json.Unmarshal(data, &p); err != nil || p == nil {
			return nil
		}
		rel, err := filepath.Rel(repoPath, filepath.Dir(path)) //factpath:host
		if err != nil {
			return nil
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
		return nil
	})
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

func (g packageGates) activeGates() map[string]pkgGate {
	out := map[string]pkgGate{}
	for dir, gate := range g.byDir {
		if gate.Vue || gate.TypeORM || gate.Drizzle || gate.Prisma {
			out[dir] = gate
		}
	}
	return out
}

func (g packageGates) forFile(_ map[string]string, relFile string) (orms ormFlags, isVue bool) {
	for d := filepath.ToSlash(factpath.Dir(relFile)); ; {
		if gate, ok := g.byDir[d]; ok {
			return ormFlags{typeORM: gate.TypeORM, drizzle: gate.Drizzle}, gate.Vue
		}
		i := strings.LastIndexByte(d, '/')
		if i < 0 {
			if d == "." {
				if gate, ok := g.byDir[""]; ok {
					return ormFlags{typeORM: gate.TypeORM, drizzle: gate.Drizzle}, gate.Vue
				}
				return ormFlags{}, false
			}
			d = "."
			continue
		}
		d = d[:i]
	}
}
