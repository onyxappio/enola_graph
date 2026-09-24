package graphsession

import (
	"context"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentAngularTemplateMembershipColdEquality(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/page.ts":   "import { Component } from '@angular/core';\n@Component({templateUrl:'./page.html'}) export class Page { save() {} }\n",
		"src/readme.md": "# Page\n\nComponent guide.\n",
		"other/base.ts": "export const base = 1;\n",
	})
	writeRepoFile(t, root, "package.json", `{"name":"app","dependencies":{"@angular/core":"1"}}`)
	eng := multiEngine(t, root, mdintent.New())
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	run := func() {
		t.Helper()
		sink := &graphstream.MemorySink{}
		if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
			t.Fatal(err)
		}
		applyRun(t, cons, sink)
		assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	}
	run()
	writeRepoFile(t, root, "src/page.html", `<button (click)="save()">Save</button>`)
	run()
	writeRepoFile(t, root, "other/added.ts", "export const added=2;\n")
	run()
	if err := os.Remove(filepath.Join(root, "src/page.html")); err != nil {
		t.Fatal(err)
	}
	run()
	writeRepoFile(t, root, "package.json", `{"name":"app"}`)
	run()
}
