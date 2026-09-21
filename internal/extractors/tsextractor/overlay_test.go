package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOverlayReadUsesCapturedBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(`{"compilerOptions":{"paths":{"@a/*":["a/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	captured := map[string][]byte{"tsconfig.json": []byte(`{"compilerOptions":{"paths":{"@a/*":["a/*"]}}}`)}
	ctx := withFileOverlay(context.Background(), newFileOverlay(dir, captured))
	if err := os.WriteFile(path, []byte(`{"compilerOptions":{"paths":{"@b/*":["b/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := overlayReadFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(captured["tsconfig.json"]) {
		t.Fatalf("overlay used live disk: %s", got)
	}
	other := withFileOverlay(context.Background(), newFileOverlay(dir, map[string][]byte{"tsconfig.json": []byte("other")}))
	gotA, _ := overlayReadFile(ctx, path)
	gotB, _ := overlayReadFile(other, path)
	if string(gotA) == string(gotB) {
		t.Fatal("concurrent session overlays must be isolated")
	}
}
