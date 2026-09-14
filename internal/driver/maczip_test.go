package driver

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// writeTestMacZip creates a .zip at zipPath containing the given entries
// (name -> content), built in-memory - avoids committing an actual binary
// .zip/.dmg fixture to the repo, mirroring zip_test.go's own
// writeTestZip helper.
func writeTestMacZip(t *testing.T, zipPath string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("creating %s: %v", zipPath, err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("adding %s to zip: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("writing zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing zip: %v", err)
	}
}

// TestBuildMacCatalog_ExtractsZippedDmgAndIgnoresAppleDoubleJunk mirrors a
// real Canon macOS driver download exactly: a .zip directly wrapping one
// .dmg, plus the __MACOSX/._<name> AppleDouble resource-fork stub macOS's
// own Archive Utility adds when zipping something on a Mac - confirmed live
// that stub shares the real file's own ".dmg" extension, so without the
// __MACOSX/"._" filtering this test guards, it would show up as a second,
// bogus ~300-byte catalog entry alongside the real 80+ MB one.
func TestBuildMacCatalog_ExtractsZippedDmgAndIgnoresAppleDoubleJunk(t *testing.T) {
	root := t.TempDir()
	canonDir := filepath.Join(root, "macOS", "Canon", "15-Sequoia")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestMacZip(t, filepath.Join(canonDir, "UFRII_v10.19.25_mac.zip"), map[string]string{
		"UFRII_v10.19.25_mac.dmg":            "pretend this is a real disk image",
		"__MACOSX/._UFRII_v10.19.25_mac.dmg": "AppleDouble resource-fork junk, not a real disk image",
	})

	cat, err := BuildMacCatalog(root)
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}

	pkgs := cat.Packages["Canon"]
	if len(pkgs) != 1 {
		t.Fatalf("expected exactly 1 Canon package (the real .dmg, not the __MACOSX stub), got %d: %v", len(pkgs), pkgs)
	}
	if filepath.Base(pkgs[0].Path) != "UFRII_v10.19.25_mac.dmg" {
		t.Errorf("expected the real UFRII_v10.19.25_mac.dmg, got %q", pkgs[0].Path)
	}
	if pkgs[0].Kind != MacPackageDmg {
		t.Errorf("expected Kind=MacPackageDmg, got %v", pkgs[0].Kind)
	}

	extractedDir := filepath.Join(canonDir, "UFRII_v10.19.25_mac")
	if info, err := os.Stat(extractedDir); err != nil || !info.IsDir() {
		t.Fatalf("expected %s to have been created by extraction", extractedDir)
	}
}

// TestBuildMacCatalog_ExtractsNestedZipInsideZip guards a real, confirmed-
// live bug (2026-09-13): a real Konica Minolta download wraps two region
// subfolders, each holding its own *inner* zip wrapping the real .pkg - two
// levels of zip nesting, not the one level Canon's own shape needed. A
// single ensureMacZipsExtracted pass only sees .zip files that already
// exist before the walk starts, so the inner zip (only created *by* that
// same pass's own extraction of the outer zip) was silently never
// discovered at all - the real .pkg stayed permanently wrapped, invisible
// to BuildMacCatalog, with no error. ensureMacZipsExtracted now repeats the
// whole walk until a pass finds nothing new to extract.
func TestBuildMacCatalog_ExtractsNestedZipInsideZip(t *testing.T) {
	root := t.TempDir()
	kmDir := filepath.Join(root, "macOS", "KonicaMinolta", "26-Tahoe")
	if err := os.MkdirAll(kmDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Build the inner zip first (wrapping the real .pkg), then embed its
	// raw bytes as one entry of the outer zip - mirrors the real shape
	// exactly: outer.zip -> Region/ -> inner.pkg.zip -> real.pkg.
	innerZipPath := filepath.Join(t.TempDir(), "inner.pkg.zip")
	writeTestMacZip(t, innerZipPath, map[string]string{
		"real.pkg": "pretend this is a real installer package",
	})
	innerZipBytes, err := os.ReadFile(innerZipPath)
	if err != nil {
		t.Fatal(err)
	}

	outerZipPath := filepath.Join(kmDir, "C750i_Series_MacOS_v5.2.14A.zip")
	writeTestMacZip(t, outerZipPath, map[string]string{
		"C750i_Series_MacOS_v5.2.14A/WW_Letter/inner.pkg.zip": string(innerZipBytes),
	})

	cat, err := BuildMacCatalog(root)
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}

	pkgs := cat.Packages["Konica Minolta"]
	if len(pkgs) != 1 {
		t.Fatalf("expected exactly 1 Konica Minolta package (the real, doubly-nested .pkg), got %d: %v", len(pkgs), pkgs)
	}
	if filepath.Base(pkgs[0].Path) != "real.pkg" {
		t.Errorf("expected the real nested real.pkg, got %q", pkgs[0].Path)
	}
}

func TestBuildMacCatalog_DoesNotReExtractExistingZipFolder(t *testing.T) {
	root := t.TempDir()
	canonDir := filepath.Join(root, "macOS", "Canon", "15-Sequoia")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestMacZip(t, filepath.Join(canonDir, "UFRII_v10.19.25_mac.zip"), map[string]string{
		"UFRII_v10.19.25_mac.dmg": "pretend this is a real disk image",
	})

	// Pre-create the destination folder empty, simulating either a prior
	// extraction or a user-made folder of the same name - either way,
	// ensureMacZipsExtracted must leave it alone rather than overwrite it.
	extractedDir := filepath.Join(canonDir, "UFRII_v10.19.25_mac")
	if err := os.MkdirAll(extractedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cat, err := BuildMacCatalog(root)
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}
	if len(cat.Packages["Canon"]) != 0 {
		t.Fatalf("did not expect the zip to have been (re-)extracted into a pre-existing empty folder, got %v", cat.Packages["Canon"])
	}
}
