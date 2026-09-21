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

// TestListRescanTargets_IncludesManufacturerWithOnlyAMacCatalogFile guards
// the real scenario neither of the two tests above covers: a manufacturer
// with a real catalog.<mfg>.json already on disk (GitHub issue #3 - built by
// either platform now) but zero local Windows archives - the common case on
// a Mac-only Drivers folder, or a Windows machine that's only ever synced in
// someone else's already-built mac catalog. Must still appear as its own
// row, with Packages empty and HasMacCatalogFile true, so the Rescan dialog
// can offer "delete catalog file" for it even though there's nothing to
// offer "Remove INF" for.
func TestListRescanTargets_IncludesManufacturerWithOnlyAMacCatalogFile(t *testing.T) {
	root := t.TempDir()
	catalogPath := macCatalogPath(root, "Ricoh")
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	targets := ListRescanTargets(root)
	if len(targets) != 1 || targets[0].Name != "Ricoh" {
		t.Fatalf("expected exactly one Ricoh row, got %+v", targets)
	}
	if len(targets[0].Packages) != 0 {
		t.Errorf("expected no packages (no local Windows archives), got %+v", targets[0].Packages)
	}
	if !targets[0].HasMacCatalogFile {
		t.Error("expected HasMacCatalogFile to be true")
	}
}

// TestListRescanTargets_ManufacturerWithBothPackagesAndMacCatalogFile guards
// the two axes being independent, not mutually exclusive - a manufacturer
// can genuinely have both a local Windows archive and a real
// catalog.<mfg>.json at once (RescanManufacturer's own doc comment).
func TestListRescanTargets_ManufacturerWithBothPackagesAndMacCatalogFile(t *testing.T) {
	root := t.TempDir()
	canonPath := filepath.Join(root, "Windows", "11", "Canon")
	if err := os.MkdirAll(canonPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonPath, "Driver.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := macCatalogPath(root, "Canon")
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	targets := ListRescanTargets(root)
	if len(targets) != 1 || targets[0].Name != "Canon" {
		t.Fatalf("expected exactly one Canon row, got %+v", targets)
	}
	if len(targets[0].Packages) != 1 {
		t.Errorf("expected the one local Windows package to still be listed, got %+v", targets[0].Packages)
	}
	if !targets[0].HasMacCatalogFile {
		t.Error("expected HasMacCatalogFile to be true")
	}
}

// TestRemoveMacCatalogFilesForSelection_SingleManufacturerLeavesOthersAlone
// mirrors TestRemoveInfCacheForSelection_SinglePackageLeavesOthersAlone for
// the new "Delete catalog files" checkbox - only the selected manufacturer's
// own catalog.<mfg>.json is removed, a sibling manufacturer's own is left
// untouched, and a "Manufacturer/RelPath"-shaped entry (RemoveInfCacheForSelection's
// own selection shape, a different concern) is silently ignored rather than
// matched against anything real.
func TestRemoveMacCatalogFilesForSelection_SingleManufacturerLeavesOthersAlone(t *testing.T) {
	root := t.TempDir()
	removePath := macCatalogPath(root, "Canon")
	keepPath := macCatalogPath(root, "Ricoh")
	for _, p := range []string{removePath, keepPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	RemoveMacCatalogFilesForSelection(root, []string{"Canon", "Kyocera/SomePackage.zip"})

	if _, err := os.Stat(removePath); !os.IsNotExist(err) {
		t.Errorf("expected Canon's own catalog file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Errorf("expected Ricoh's own catalog file to be left alone, got err=%v", err)
	}
}
