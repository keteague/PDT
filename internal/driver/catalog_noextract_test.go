package driver

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildCatalogNoExtract_SkipsZipExtraction guards the exact fix for the
// USB-flash-drive slow-scan bug: BuildCatalogNoExtract must never run
// ensureZipsExtracted (or any of the other ensure*Extracted helpers), even
// when a raw archive is sitting right there unextracted. Confirmed live: the
// extracting BuildCatalog popped up a visible expand.exe console window per
// MSI on every on-launch scan, which was slow and disruptive on a USB 2.0
// flash drive - BuildCatalogNoExtract exists so a flash drive's own
// on-launch scan can skip straight to reading already-extracted .infs.
func TestBuildCatalogNoExtract_SkipsZipExtraction(t *testing.T) {
	root := t.TempDir()
	canonDir := filepath.Join(root, "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, filepath.Join(canonDir, "ZippedPackage.zip"), "Driver/zipped.inf", testZipInf)

	cat, err := BuildCatalogNoExtract(root)
	if err != nil {
		t.Fatalf("BuildCatalogNoExtract: %v", err)
	}

	if _, ok := cat["Canon"]["Canon Zipped Test Driver"]; ok {
		t.Fatalf("did not expect the zipped driver to be found without extraction, got %v", cat["Canon"])
	}
	extractedDir := filepath.Join(canonDir, "ZippedPackage")
	if _, err := os.Stat(extractedDir); !os.IsNotExist(err) {
		t.Fatalf("expected %s to NOT have been created - BuildCatalogNoExtract must not extract", extractedDir)
	}
}

// TestBuildCatalogNoExtract_FindsAlreadyExtractedInf confirms
// BuildCatalogNoExtract still does the actual job of a catalog scan - it
// just skips the extraction step, not .inf discovery - matching the real
// expected case of a flash drive whose archives were already extracted by
// the technician's local install before Write to Flash Drive/Sync.
func TestBuildCatalogNoExtract_FindsAlreadyExtractedInf(t *testing.T) {
	root := t.TempDir()
	extractedDir := filepath.Join(root, "Canon", "AlreadyExtracted", "Driver")
	if err := os.MkdirAll(extractedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extractedDir, "zipped.inf"), []byte(testZipInf), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := BuildCatalogNoExtract(root)
	if err != nil {
		t.Fatalf("BuildCatalogNoExtract: %v", err)
	}
	if _, ok := cat["Canon"]["Canon Zipped Test Driver"]; !ok {
		t.Fatalf("expected the already-extracted driver name to be found, got %v", cat["Canon"])
	}
}
