package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"PDT/internal/driver"
)

func TestEnsureMacDriversScaffold(t *testing.T) {
	root := t.TempDir()

	// Pre-existing tree, mimicking a real one: Canon has two version
	// folders, only one of which already has an Archive; Sharp has none
	// yet at all (bare manufacturer folder, e.g. a brand-new install).
	mustMkdirAll(t, filepath.Join(root, "Canon", "26-Tahoe"))
	mustMkdirAll(t, filepath.Join(root, "Canon", "27-GoldenGate"))
	mustMkdirAll(t, filepath.Join(root, "Canon", "26-Tahoe", "Archive"))
	mustMkdirAll(t, filepath.Join(root, "Sharp"))

	if err := ensureMacDriversScaffold(root); err != nil {
		t.Fatalf("ensureMacDriversScaffold: %v", err)
	}

	// Every manufacturer gets at least a bare folder.
	for _, mfg := range driver.Manufacturers {
		folder := filepath.Join(root, strings.ReplaceAll(mfg, " ", ""))
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			t.Errorf("expected manufacturer folder %q to exist", folder)
		}
	}

	// Every existing version folder under Canon gets its own Archive/README.txt.
	for _, version := range []string{"26-Tahoe", "27-GoldenGate"} {
		readme := filepath.Join(root, "Canon", version, "Archive", "README.txt")
		if _, err := os.Stat(readme); err != nil {
			t.Errorf("expected %q to exist: %v", readme, err)
		}
	}

	// Sharp had no version folders at all - scaffold must not invent one.
	sharpEntries, err := os.ReadDir(filepath.Join(root, "Sharp"))
	if err != nil {
		t.Fatalf("reading Sharp folder: %v", err)
	}
	if len(sharpEntries) != 0 {
		t.Errorf("expected Sharp folder to stay empty (no version folder to invent), got %v", sharpEntries)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("creating %q: %v", path, err)
	}
}
