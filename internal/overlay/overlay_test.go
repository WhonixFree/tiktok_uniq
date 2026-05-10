package overlay

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestDiscoverScansRecursively(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "top.mp4"),
		filepath.Join(dir, "nested", "deep.mov"),
		filepath.Join(dir, "nested", "deeper", "clip.MKV"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("overlay"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("not video"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	want := append([]string(nil), paths...)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover() = %#v, want %#v", got, want)
	}
}

func TestDiscoverSkipsHiddenSubtrees(t *testing.T) {
	dir := t.TempDir()
	hidden := filepath.Join(dir, ".hidden", "clip.mp4")
	if err := os.MkdirAll(filepath.Dir(hidden), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hidden, []byte("overlay"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Discover(dir)
	if !errors.Is(err, ErrNoOverlayFiles) {
		t.Fatalf("expected ErrNoOverlayFiles, got %v", err)
	}
}
