package driver

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestListRescanTargets_FindsPackagesAndSkipsCache(t *testing.T) {
	root := t.TempDir()
	windowsRoot := filepath.Join(root, "Windows", "11")
	canonPath := filepath.Join(windowsRoot, "Canon")
	if err := os.MkdirAll(canonPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonPath, "Driver.zip"), []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A .pdt-infcache entry sitting alongside it must never be listed as its
	// own selectable package - it's the derived cache, not a source archive.
	cacheDir := filepath.Join(canonPath, PdtInfCacheDirName, "Driver")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "driver.inf"), []byte(testZipInf), 0o644); err != nil {
		t.Fatal(err)
	}

	targets := ListRescanTargets(root)
	if len(targets) != 1 || targets[0].Name != "Canon" {
		t.Fatalf("expected exactly one Canon row, got %+v", targets)
	}
	if len(targets[0].Packages) != 1 || targets[0].Packages[0].RelPath != "Driver.zip" {
		t.Fatalf("expected exactly one Driver.zip package, got %+v", targets[0].Packages)
	}
}

func TestListRescanTargets_MergesManufacturerAcrossVersionFolders(t *testing.T) {
	root := t.TempDir()
	v10 := filepath.Join(root, "Windows", "10", "Canon")
	v11 := filepath.Join(root, "Windows", "11", "Canon")
	if err := os.MkdirAll(v10, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(v11, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v10, "Old.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v11, "New.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	targets := ListRescanTargets(root)
	if len(targets) != 1 {
		t.Fatalf("expected Canon merged into a single row, got %+v", targets)
	}
	names := []string{}
	for _, p := range targets[0].Packages {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "New.zip" || names[1] != "Old.zip" {
		t.Fatalf("expected both Old.zip and New.zip merged in, got %v", names)
	}
}

func TestRemoveInfCacheForSelection_SinglePackageLeavesOthersAlone(t *testing.T) {
	root := t.TempDir()
	canonPath := filepath.Join(root, "Windows", "11", "Canon")
	keepDir := filepath.Join(canonPath, PdtInfCacheDirName, "Keep")
	removeDir := filepath.Join(canonPath, PdtInfCacheDirName, "Remove")
	if err := os.MkdirAll(keepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(removeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	RemoveInfCacheForSelection(root, []string{"Canon/Remove.zip"})

	if _, err := os.Stat(removeDir); !os.IsNotExist(err) {
		t.Errorf("expected Remove.zip's own cache entry to be removed, got err=%v", err)
	}
	if _, err := os.Stat(keepDir); err != nil {
		t.Errorf("expected Keep's cache entry to be left alone, got err=%v", err)
	}
}
