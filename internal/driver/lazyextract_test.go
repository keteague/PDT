package driver

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// writeZipEntries creates a .zip at zipPath with one entry per name->content
// pair - lazyextract_test.go's own version of zip_test.go's writeTestZip,
// extended to support more than one entry (needed here to prove a real,
// full extraction pulls in a companion file, not just an .inf).
func writeZipEntries(t *testing.T, zipPath string, entries map[string]string) {
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

// TestEnsureArchiveExtracted_NestedArchiveInsideInfCache is the direct
// regression test for a real bug found live: a driver deploy failed with
// "SetupCopyOEMInf(...): The system cannot find the file specified" for a
// Lexmark driver whose real archive (ArchEntry.ArchivePath) was a nested
// .msi living inside .pdt-infcache - Lexmark's own real package is an outer
// self-extracting RAR whose selective .inf-only extraction also reveals
// several inner .msi files (needed to find their own .inf entries in turn -
// see ensureMsiInfsExtracted's own ordering comment in catalog.go).
//
// EnsureArchiveExtracted's own "already extracted, skip" check used to treat
// the nested archive's own PRE-EXISTING .inf-only cache folder (created
// earlier by the catalog-scan side, containing just the cached .inf, none
// of the companion files a real deploy needs alongside it) as if it were
// already a full extraction - because both computed the exact same
// destination path for an archive already living inside .pdt-infcache. This
// reproduces that exact shape with a synthetic zip-in-zip fixture (Lexmark's
// own real self-extracting-RAR-in-an-.exe/.msi shape needs 7z/msiexec to
// even construct a fixture for - a zip-in-zip exercises the identical
// EnsureArchiveExtracted code path without either).
func TestEnsureArchiveExtracted_NestedArchiveInsideInfCache(t *testing.T) {
	root := t.TempDir()

	outerZip := filepath.Join(root, "Outer.zip")
	// Inner.zip needs to be a real, valid zip itself (containing an .inf
	// plus a companion file) - build it separately, then embed its raw bytes
	// as one entry of Outer.zip so Outer.zip really does contain a nested
	// archive, the same shape 7z's own selective extraction
	// (extractInfsFromSfxArchive) reveals from Lexmark's real package.
	innerZip := filepath.Join(root, "inner-scratch.zip")
	writeZipEntries(t, innerZip, map[string]string{
		"driver.inf":    testZipInf,
		"companion.cat": "not a real catalog file, just needs to exist",
	})
	innerBytes, err := os.ReadFile(innerZip)
	if err != nil {
		t.Fatal(err)
	}
	writeZipEntries(t, outerZip, map[string]string{"Inner.zip": string(innerBytes)})

	// Simulate exactly what the catalog-scan side (ensureZipInfsExtracted)
	// would have already produced for this outer/nested pair before Deploy
	// ever runs: a top-level .inf-only cache entry for Outer.zip (with its
	// own marker pointing back at the real Outer.zip), and - because that
	// pass doesn't skip .pdt-infcache while searching for source archives -
	// a second, nested .inf-only cache entry for Inner.zip once it was
	// revealed sitting inside the first one, plus the real (uninspected
	// beyond its own name) copy of Inner.zip itself sitting alongside it.
	cacheDir := filepath.Join(root, PdtInfCacheDirName, "Outer")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSourceMarker(cacheDir, outerZip)

	nestedInnerZip := filepath.Join(cacheDir, "Inner.zip")
	if err := os.WriteFile(nestedInnerZip, innerBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	nestedCacheDir := filepath.Join(cacheDir, "Inner")
	if err := os.MkdirAll(nestedCacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedCacheDir, "driver.inf"), []byte(testZipInf), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSourceMarker(nestedCacheDir, nestedInnerZip)

	// This is exactly ArchEntry.ArchivePath's own real-world shape for a
	// Lexmark driver found this way - a nested archive path already living
	// inside .pdt-infcache.
	extractedDir, err := EnsureArchiveExtracted(nestedInnerZip)
	if err != nil {
		t.Fatalf("EnsureArchiveExtracted: %v", err)
	}

	if filepath.Dir(extractedDir) == cacheDir {
		t.Fatalf("EnsureArchiveExtracted returned the pre-existing .inf-only cache folder (%s) instead of a real, freshly-extracted one - the exact bug found live", extractedDir)
	}

	infPath := filepath.Join(extractedDir, "driver.inf")
	if _, err := os.Stat(infPath); err != nil {
		t.Errorf("expected the real .inf at %s, got %v", infPath, err)
	}
	companionPath := filepath.Join(extractedDir, "companion.cat")
	if _, err := os.Stat(companionPath); err != nil {
		t.Errorf("expected the real companion file at %s (missing means this is still just the .inf-only cache) - got %v", companionPath, err)
	}
}
