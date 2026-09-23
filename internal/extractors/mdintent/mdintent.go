// Package mdintent extracts declared intent from markdown pages — the wiki as
// a source tree, compiled by enola like any other source. A page opts in with
// an `enola_intent:` frontmatter key (namespaced so the wiki's own toolchain
// ignores it); the page body is prose and is never parsed. Every fact this
// extractor emits cites the PAGE as its file, so a verdict's evidence points
// at the decision that declared the intent, not at a config artifact.
package mdintent

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/enola-labs/enola/internal/extractors/inputscope"

	"github.com/enola-labs/enola/internal/extractors/extcoverage"
	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/intent"
	"github.com/enola-labs/enola/pkg/plugin"
)

// Extractor extracts enola_intent frontmatter from markdown pages.
type Extractor struct {
	inputScope *inputscope.Scope
	calls      atomic.Int64
}

// New creates the extractor.
func New() *Extractor { return &Extractor{} }

// Name returns the extractor name.
func (e *Extractor) Name() string { return "mdintent" }

func (e *Extractor) OwnsFactFile(relFile string) bool {
	return strings.HasSuffix(relFile, ".md")
}

// ContentInput implements plugin.DeltaInputs: markdown bytes, excluding archive/view trees.
func (e *Extractor) ContentInput(rel string) bool {
	slashed := filepath.ToSlash(rel)
	if !strings.HasSuffix(slashed, ".md") {
		return false
	}
	return !strings.Contains(slashed, "_archive/") && !strings.Contains(slashed, "_views/")
}

// NameSetInput implements plugin.DeltaInputs: document links resolve against inventory names.
func (e *Extractor) NameSetInput() bool { return true }

// Detect probes for any markdown file up to five directory levels (wiki trees
// nest: wiki/<scope>/permanent/<area>/page.md). A page carrying the
// enola_intent key compiles as intent; every other markdown file is a
// document, so a repository with a README is in scope. Graph mode instead
// follows input policy at any depth and uses the extraction content predicate.
func (e *Extractor) Detect(repoPath string) (bool, error) {
	inputScope := e.inputScope
	found := false
	root := filepath.Clean(repoPath) //factpath:host
	_ = inputScope.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if inputScope != nil && inputScope.Policy != nil {
				return nil
			}
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || mdSkipDirs[name]) {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(root, path)
			if rel != "." && strings.Count(filepath.ToSlash(rel), "/") >= 5 {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = factpath.Slash(rel)
		supported := strings.HasSuffix(path, ".md")
		if inputScope != nil && inputScope.Policy != nil {
			supported = e.ContentInput(rel)
		}
		if supported {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found, nil
}

var mdSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "testdata": true, "dist": true,
	"build": true, "tmp": true, "_archive": true, "_views": true,
}

// Extract runs in two modes per file. A page carrying enola_intent compiles
// its block into intent facts; an invalid block fails the snapshot, since a
// declaration that cannot be trusted is worse than none. Every other markdown
// file yields a document symbol, a section symbol per heading and a names
// relation per link that resolves against the walked file set, with the links
// that do not counted on the extraction fact.
func (e *Extractor) Extract(ctx context.Context, repoPath string, files []string) ([]facts.Fact, error) {
	inputScope := e.inputScope
	return e.extract(ctx, repoPath, files, func(relFile string) ([]byte, error) {
		return inputScope.ReadFile(filepath.Join(repoPath, relFile))
	}, false)
}

// CaptureInputs reads every content input once, so a caller that must prove its
// facts came from particular bytes can extract from exactly those bytes instead
// of a second read of a tree that may have moved underneath it.
//
// A page the walker listed but that cannot be read is an error here, where
// Extract skips it. To a caller comparing contributions, a silently dropped
// page is indistinguishable from one that legitimately emits nothing, and that
// difference decides whether its owner is replaced.
func (e *Extractor) CaptureInputs(repoPath string, files []string) (map[string][]byte, error) {
	inputScope := e.inputScope
	out := make(map[string][]byte)
	for _, relFile := range files {
		if !e.ContentInput(relFile) {
			continue
		}
		src, err := inputScope.ReadFile(filepath.Join(repoPath, relFile))
		if err != nil {
			return nil, fmt.Errorf("mdintent capture %s: %w", relFile, err)
		}
		out[filepath.ToSlash(relFile)] = src
	}
	return out, nil
}

// ExtractCaptured is Extract over captured bytes. Every content input must be
// present in src: a missing one fails rather than yielding an empty page.
func (e *Extractor) ExtractCaptured(ctx context.Context, repoPath string, files []string, src map[string][]byte) ([]facts.Fact, error) {
	return e.extract(ctx, repoPath, files, func(relFile string) ([]byte, error) {
		b, ok := src[filepath.ToSlash(relFile)]
		if !ok {
			return nil, fmt.Errorf("mdintent: %s is not in the captured inputs", relFile)
		}
		return b, nil
	}, true)
}

// ExtractCalls counts attempted extractions, captured or not, whether or not
// they returned facts. A caller that previews an extraction to plan with and
// then consumes it has to be able to show the page was compiled once, not twice
// - and a second attempt that failed is still a second read of the tree.
func (e *Extractor) ExtractCalls() int { return int(e.calls.Load()) }

// extract compiles the pages read supplies. strict fails the extraction on an
// unreadable input instead of skipping it; see CaptureInputs.
func (e *Extractor) extract(_ context.Context, repoPath string, files []string, read func(relFile string) ([]byte, error), strict bool) ([]facts.Fact, error) {
	e.calls.Add(1)
	var out []facts.Fact
	var links linkCount
	documents, sections := 0, 0
	// What a link may name: the files the walker kept, and their directories. See
	// inScope — resolving against the filesystem instead made the snapshot depend on
	// whether a previous snapshot existed.
	scope := newInScope(files)
	for _, relFile := range files {
		slashed := filepath.ToSlash(relFile)
		if !e.ContentInput(relFile) {
			continue
		}
		src, err := read(relFile)
		if err != nil {
			if strict {
				return nil, err
			}
			continue
		}
		page, err := intent.ParsePage(src)
		if err != nil {
			// Fatal, not recorded: an ordinary extractor error loses one file's
			// facts, but losing a declaration loses every verdict computed from it
			// while the snapshot still reports success. The repo-file carrier fails
			// the run for the same reason; both carriers must behave alike, or which
			// file you put a declaration in decides whether a typo is silent.
			return nil, plugin.Fatalf("%s: %w", relFile, err)
		}
		if page == nil {
			docs := documentFacts(scope, relFile, src, &links)
			out = append(out, docs...)
			documents++
			sections += len(docs) - 1
			continue
		}
		out = append(out, intent.CompilePageFacts(page, slashed)...)
		rels := 0
		if page.Page != nil {
			rels = len(page.Page.Relations)
		}
		log.Printf("[mdintent] %s: page=%t %d relation(s), %d seam(s), %d layer set(s), %d claim(s)",
			relFile, page.Page != nil, rels, len(page.Consumes), len(page.Layers), len(page.Claims))
	}
	if documents > 0 {
		unresolved := 0
		for _, n := range links.unresolved {
			unresolved += n
		}
		log.Printf("[mdintent] %d document(s), %d section(s), %d link(s) resolved on disk, %d not", documents, sections, links.resolved, unresolved)
	}
	if coverage, ok := extcoverage.Fact(repoPath, linksFactName, linksEdgeType, links.resolved, links.unresolved); ok {
		out = append(out, coverage)
	}
	return out, nil
}

func NewGraph(scope *inputscope.Scope) *Extractor { return &Extractor{inputScope: scope} }
