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

// TestBuildCatalog_ExtractsAndScansZippedDriverPackage confirms BuildCatalog
// finds a zip's driver name via the .inf-only cache (GitHub issue #10) -
// PdtInfCacheDirName/ZippedPackage, not a full ZippedPackage/ sibling the
// way it worked before that rework (BuildCatalog only ever reads .inf text
// content; the rest of a real package is never extracted at catalog-build
// time anymore - see EnsureArchiveExtracted for the lazy, Deploy-time-only
// full extraction that replaced it).
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

	cacheDir := filepath.Join(canonDir, PdtInfCacheDirName, "ZippedPackage")
	if info, err := os.Stat(cacheDir); err != nil || !info.IsDir() {
		t.Fatalf("expected %s to have been created by .inf-only extraction", cacheDir)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "Driver", "zipped.inf")); err != nil {
		t.Fatalf("expected the zip's Driver/zipped.inf to have been cached: %v", err)
	}
	// The full package is never extracted at catalog-build time anymore -
	// only the .inf itself. No ZippedPackage/ sibling of the zip at all.
	if _, err := os.Stat(filepath.Join(canonDir, "ZippedPackage")); !os.IsNotExist(err) {
		t.Errorf("did not expect a full-extraction ZippedPackage/ sibling to exist, got err=%v", err)
	}
}

func TestBuildCatalog_DoesNotReExtractExistingCache(t *testing.T) {
	root := t.TempDir()
	canonDir := filepath.Join(root, "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, filepath.Join(canonDir, "ZippedPackage.zip"), "Driver/zipped.inf", testZipInf)

	// Pre-create the .inf cache destination folder empty, simulating either
	// a prior extraction or a user-made folder of the same name - either
	// way, ensureZipInfsExtracted must leave it alone rather than overwrite
	// it.
	cacheDir := filepath.Join(canonDir, PdtInfCacheDirName, "ZippedPackage")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A finished extraction always ends with its source marker.
	writeSourceMarker(cacheDir, filepath.Join(canonDir, "ZippedPackage.zip"))

	cat, err := BuildCatalog(root)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	if _, ok := cat["Canon"]["Canon Zipped Test Driver"]; ok {
		t.Fatalf("did not expect the zip to have been (re-)extracted into a pre-existing cache folder, got %v", cat["Canon"])
	}
}

// TestEnsureZipInfsExtracted_OnlyInfsLandInCache is the direct regression
// test for GitHub issue #10's whole premise: BuildCatalog only ever reads a
// package's .inf text content, so the cache it builds should hold only
// that - not the rest of a real package's payload (.dll/.cat/help files),
// which is exactly what made a real Drivers folder 5.4GB/22,570 files
// instead of ~1.5GB/~25.
func TestEnsureZipInfsExtracted_OnlyInfsLandInCache(t *testing.T) {
	root := t.TempDir()
	canonDir := filepath.Join(root, "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(canonDir, "ZippedPackage.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	mustWrite := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("Driver/zipped.inf", testZipInf)
	mustWrite("Driver/zipped.cat", "not a real catalog file, just payload")
	mustWrite("Driver/help.chm", "not a real help file, just payload")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	ensureZipInfsExtracted(canonDir)

	cacheDir := filepath.Join(canonDir, PdtInfCacheDirName, "ZippedPackage")
	if _, err := os.Stat(filepath.Join(cacheDir, "Driver", "zipped.inf")); err != nil {
		t.Errorf("expected the .inf to be cached: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "Driver", "zipped.cat")); !os.IsNotExist(err) {
		t.Errorf("expected the .cat payload file to NOT be cached, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "Driver", "help.chm")); !os.IsNotExist(err) {
		t.Errorf("expected the .chm payload file to NOT be cached, got err=%v", err)
	}
}

func TestEnsureZipInfsExtracted_SkipsAlreadyCached(t *testing.T) {
	root := t.TempDir()
	canonDir := filepath.Join(root, "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, filepath.Join(canonDir, "ZippedPackage.zip"), "Driver/zipped.inf", testZipInf)

	cacheDir := filepath.Join(canonDir, PdtInfCacheDirName, "ZippedPackage")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSourceMarker(cacheDir, filepath.Join(canonDir, "ZippedPackage.zip"))

	ensureZipInfsExtracted(canonDir)

	if _, err := os.Stat(filepath.Join(cacheDir, "Driver", "zipped.inf")); !os.IsNotExist(err) {
		t.Errorf("expected the completed cache folder to be left alone, not (re-)extracted into, got err=%v", err)
	}
}

// TestEnsureZipInfsExtracted_RebuildsIncompleteCache is the regression test
// for Toshiba's universal driver (Ken, 2026-09-20): its cache folder existed
// (left by an older PDT version) but held none of the package's own .inf files
// and had no top-level source marker, so the "folder exists = done" check
// skipped it forever and Toshiba never produced a single Windows driver. A
// folder without the marker - written last by a successful pass - is rebuilt.
func TestEnsureZipInfsExtracted_RebuildsIncompleteCache(t *testing.T) {
	root := t.TempDir()
	canonDir := filepath.Join(root, "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, filepath.Join(canonDir, "ZippedPackage.zip"), "Driver/zipped.inf", testZipInf)

	cacheDir := filepath.Join(canonDir, PdtInfCacheDirName, "ZippedPackage")
	if err := os.MkdirAll(filepath.Join(cacheDir, "Driver", "leftover"), 0o755); err != nil {
		t.Fatal(err)
	}

	ensureZipInfsExtracted(canonDir)

	if _, err := os.Stat(filepath.Join(cacheDir, "Driver", "zipped.inf")); err != nil {
		t.Errorf("expected the incomplete cache folder to be rebuilt with the package's .inf, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, pdtSourceMarkerName)); err != nil {
		t.Errorf("expected the rebuilt cache to carry its source marker, got err=%v", err)
	}
}

// TestEnsureZipInfsExtracted_ProcessesArchivesInsideInfCache is the direct
// regression test for a real bug found live: a multi-layer package (real
// example: Lexmark's own driver, an outer self-extracting RAR wrapping an
// inner .msi that itself contains the real .inf) needs one ensure*InfsExtracted
// pass to reveal a nested archive *into* PdtInfCacheDirName, then a later
// pass to find and process it there. Skipping PdtInfCacheDirName outright
// while searching for source archives (as an earlier version of this code
// did, confusing it with Sync's own unrelated "never transfer this"
// exclusion) silently broke that cascade - a .zip landing inside
// PdtInfCacheDirName (simulating what an earlier extraction pass revealed)
// must still be found and processed here, not skipped as if it were already
// this function's own finished output.
func TestEnsureZipInfsExtracted_ProcessesArchivesInsideInfCache(t *testing.T) {
	root := t.TempDir()
	nestedDir := filepath.Join(root, PdtInfCacheDirName, "OuterPackage")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, filepath.Join(nestedDir, "Inner.zip"), "Driver/inner.inf", testZipInf)

	ensureZipInfsExtracted(root)

	if _, err := os.Stat(filepath.Join(nestedDir, "Inner", "Driver", "inner.inf")); err != nil {
		t.Errorf("expected the .zip inside %s to be found and processed, not skipped: %v", PdtInfCacheDirName, err)
	}
}
