// Package inputscope enforces immutable graph input policy at extractor reads.
// A nil scope preserves the legacy extraction profile.
package inputscope

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/enola-labs/enola/internal/graphinput"
)

type Scope struct {
	Root   string
	Policy *graphinput.Policy
}

func First(scopes []*Scope) *Scope {
	if len(scopes) > 0 {
		return scopes[0]
	}
	return nil
}
func (s *Scope) Allowed(path string, dir bool) bool {
	if s == nil || s.Policy == nil {
		return true
	}
	if !dir {
		return s.Policy.ClassifyDependency(path).Kind != graphinput.Excluded
	}
	return s.Policy.Classify(path, dir).Kind != graphinput.Excluded
}
func (s *Scope) ReadFile(path string) ([]byte, error) {
	if !s.Allowed(path, false) {
		return nil, &os.PathError{Op: "graph input", Path: path, Err: fs.ErrNotExist}
	}
	return os.ReadFile(path)
}
func (s *Scope) Stat(path string) (os.FileInfo, error) {
	if !s.Allowed(path, false) {
		return nil, &os.PathError{Op: "graph input", Path: path, Err: fs.ErrNotExist}
	}
	return os.Stat(path)
}
func (s *Scope) ReadDir(path string) ([]os.DirEntry, error) {
	if !s.Allowed(path, true) {
		return nil, &os.PathError{Op: "graph input", Path: path, Err: fs.ErrNotExist}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := entries[:0]
	for _, d := range entries {
		if s.Allowed(filepath.Join(path, d.Name()), d.IsDir()) {
			out = append(out, d)
		}
	}
	return out, nil
}
func (s *Scope) WalkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if d != nil && !s.Allowed(path, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return fn(path, d, err)
	})
}
func (s *Scope) Glob(pattern string) ([]string, error) {
	names, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	out := names[:0]
	for _, p := range names {
		if s.Allowed(p, false) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Scope) Open(path string) (*os.File, error) {
	if !s.Allowed(path, false) {
		return nil, &os.PathError{Op: "graph input", Path: path, Err: fs.ErrNotExist}
	}
	return os.Open(path)
}
