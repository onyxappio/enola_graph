package tsextractor

// RouterDTO is the persistable form of routerFile. Unexported fields cannot be
// JSON-marshaled, so the session copies them at the cache boundary.
type RouterDTO struct {
	RelFile   string                       `json:"rel_file"`
	Roots     map[string]bool              `json:"roots,omitempty"`
	Routers   map[string]bool              `json:"routers,omitempty"`
	Pending   map[string][]PendingRouteDTO `json:"pending,omitempty"`
	Mounts    []MountDTO                   `json:"mounts,omitempty"`
	Exports   map[string]string            `json:"exports,omitempty"`
	Imports   map[string]ImportRefDTO      `json:"imports,omitempty"`
	Factories map[string]string            `json:"factories,omitempty"`
}

type PendingRouteDTO struct {
	Verb      string `json:"verb"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Framework string `json:"framework"`
}

type MountDTO struct {
	File      string `json:"file"`
	Parent    string `json:"parent"`
	Prefix    string `json:"prefix"`
	Child     string `json:"child"`
	ChildCall bool   `json:"child_call,omitempty"`
}

type ImportRefDTO struct {
	File   string `json:"file"`
	Export string `json:"export"`
}

func routerToDTO(f *routerFile) *RouterDTO {
	if f == nil || f.empty() {
		return nil
	}
	d := &RouterDTO{
		RelFile:   f.relFile,
		Roots:     f.roots,
		Routers:   f.routers,
		Exports:   f.exports,
		Factories: f.factories,
	}
	if len(f.pending) > 0 {
		d.Pending = make(map[string][]PendingRouteDTO, len(f.pending))
		for k, rs := range f.pending {
			out := make([]PendingRouteDTO, len(rs))
			for i, r := range rs {
				out[i] = PendingRouteDTO{Verb: r.verb, Path: r.path, Line: r.line, Framework: r.framework}
			}
			d.Pending[k] = out
		}
	}
	if len(f.mounts) > 0 {
		d.Mounts = make([]MountDTO, len(f.mounts))
		for i, m := range f.mounts {
			d.Mounts[i] = MountDTO{File: m.file, Parent: m.parent, Prefix: m.prefix, Child: m.child, ChildCall: m.childCall}
		}
	}
	if len(f.imports) > 0 {
		d.Imports = make(map[string]ImportRefDTO, len(f.imports))
		for k, v := range f.imports {
			d.Imports[k] = ImportRefDTO{File: v.file, Export: v.export}
		}
	}
	return d
}

func routerFromDTO(d *RouterDTO) *routerFile {
	if d == nil {
		return nil
	}
	f := &routerFile{
		relFile:   d.RelFile,
		roots:     d.Roots,
		routers:   d.Routers,
		exports:   d.Exports,
		factories: d.Factories,
	}
	if len(d.Pending) > 0 {
		f.pending = make(map[string][]pendingRoute, len(d.Pending))
		for k, rs := range d.Pending {
			out := make([]pendingRoute, len(rs))
			for i, r := range rs {
				out[i] = pendingRoute{verb: r.Verb, path: r.Path, line: r.Line, framework: r.Framework}
			}
			f.pending[k] = out
		}
	}
	if len(d.Mounts) > 0 {
		f.mounts = make([]routerMountEdge, len(d.Mounts))
		for i, m := range d.Mounts {
			f.mounts[i] = routerMountEdge{file: m.File, parent: m.Parent, prefix: m.Prefix, child: m.Child, childCall: m.ChildCall}
		}
	}
	if len(d.Imports) > 0 {
		f.imports = make(map[string]importRef, len(d.Imports))
		for k, v := range d.Imports {
			f.imports[k] = importRef{file: v.File, export: v.Export}
		}
	}
	if f.empty() {
		return nil
	}
	return f
}
