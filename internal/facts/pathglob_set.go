package facts

import (
	"path/filepath"
	"strings"
)

// GlobSet is a compiled glob list. Match is equivalent to MatchGlob on the
// original patterns, including which pattern wins when several match.
type GlobSet struct {
	items []compiledGlob
}

type compiledGlob struct {
	raw     string
	needExt string
	skipSeg string
}

// CompileGlobs prepares patterns for repeated matching. A nil or empty list
// yields a set that never matches.
func CompileGlobs(patterns []string) *GlobSet {
	s := &GlobSet{items: make([]compiledGlob, 0, len(patterns))}
	for _, p := range patterns {
		s.items = append(s.items, compileGlob(p))
	}
	return s
}

func compileGlob(pattern string) compiledGlob {
	g := compiledGlob{raw: pattern}
	if strings.HasPrefix(pattern, "**/") && strings.HasSuffix(pattern, "/**") {
		seg := strings.TrimSuffix(strings.TrimPrefix(pattern, "**/"), "/**")
		if seg != "" && !strings.ContainsAny(seg, "*?[\\") && !strings.Contains(seg, "/") {
			g.skipSeg = seg
		}
	}
	if g.skipSeg == "" && !strings.HasSuffix(pattern, "/**") {
		base := pattern
		if i := strings.LastIndex(pattern, "/"); i >= 0 {
			base = pattern[i+1:]
		}
		ext := filepath.Ext(base)
		if ext != "" && !strings.ContainsAny(ext, "*?[\\") {
			g.needExt = ext
		}
	}
	return g
}

// Match returns the first pattern that matches relPath, same as MatchGlob.
func (s *GlobSet) Match(relPath string) (string, bool) {
	if s == nil || len(s.items) == 0 {
		return "", false
	}
	var segs []string
	base := relPath
	if i := strings.LastIndex(relPath, "/"); i >= 0 {
		base = relPath[i+1:]
	}
	ext := filepath.Ext(base)
	for _, g := range s.items {
		if g.needExt != "" && g.needExt != ext {
			continue
		}
		if g.skipSeg != "" {
			if segs == nil {
				if relPath == "" {
					segs = []string{""}
				} else {
					segs = strings.Split(relPath, "/")
				}
			}
			for _, part := range segs {
				if part == g.skipSeg {
					return g.raw, true
				}
			}
			continue
		}
		if one, ok := MatchGlob(relPath, []string{g.raw}); ok {
			return one, true
		}
	}
	return "", false
}

// MatchAny reports whether any pattern matches.
func (s *GlobSet) MatchAny(relPath string) bool {
	_, ok := s.Match(relPath)
	return ok
}
