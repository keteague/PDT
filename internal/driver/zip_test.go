package driver

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

const testZipInf = `[Version]
Signature="$Windows NT$"
Provider=%CANON%
Class=Printer
DriverVer=03/01/2026,1.0.0.0
CatalogFile=zipped.cat

[Manufacturer]
"Canon" = CanonModels

[CanonModels]
"Canon Zipped Test Driver" = ZIPPED, USBPRINT\CanonZippedTest

[Strings]
CANON = "Canon"
`

// writeTestZip creates a .zip at zipPath containing a single .inf at
// entryPath, built in-memory - avoids committing an actual binary .zip
// fixture to the repo for what only needs to prove one thing: BuildCatalog
// can see inside a zip at all.
func writeTestZip(t *testing.T, zipPath, entryPath, infContent string) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("creating %s: %v", zipPath, err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	w, err := zw.Create(entryPath)
	if err != nil {
		t.Fatalf("adding %s to zip: %v", entryPath, err)
	}
	if _, err := w.Write([]byte(infContent)); err != nil {
		t.Fatalf("writing zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing zip: %v", err)
	}
}

func TestBuildCatalog_ExtractsAndScansZippedDriverPackage(t *testing.T) {
	root := t.TempDir()
	canonDir := filepath.Join(root, "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, filepath.Join(canonDir, "ZippedPackage.zip"), "Driver/zipped.inf", testZipInf)

	cat, err := BuildCatalog(root)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}

	if _, ok := cat["Canon"]["Canon Zipped Test Driver"]; !ok {
		t.Fatalf("expected the driver name inside ZippedPackage.zip to be found after extraction, got %v", cat["Canon"])
	}

	extractedDir := filepath.Join(canonDir, "ZippedPackage")
	if info, err := os.Stat(extractedDir); err != nil || !info.IsDir() {
		t.Fatalf("expected %s to have been created by extraction", extractedDir)
	}
	if _, err := os.Stat(filepath.Join(extractedDir, "Driver", "zipped.inf")); err != nil {
		t.Fatalf("expected the zip's Driver/zipped.inf to have been extracted: %v", err)
	}
}

func TestBuildCatalog_DoesNotReExtractExistingFolder(t *testing.T) {
	root := t.TempDir()
	canonDir := filepath.Join(root, "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, filepath.Join(canonDir, "ZippedPackage.zip"), "Driver/zipped.inf", testZipInf)

	// Pre-create the destination folder empty, simulating either a prior
	// extraction or a user-made folder of the same name - either way,
	// ensureZipsExtracted must leave it alone rather than overwrite it.
	extractedDir := filepath.Join(canonDir, "ZippedPackage")
	if err := os.MkdirAll(extractedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cat, err := BuildCatalog(root)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	if _, ok := cat["Canon"]["Canon Zipped Test Driver"]; ok {
		t.Fatalf("did not expect the zip to have been (re-)extracted into a pre-existing folder, got %v", cat["Canon"])
	}
}
