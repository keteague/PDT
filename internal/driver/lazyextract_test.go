package driver

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestExtractionCache_NestedArchiveInsideInfCache is the direct
// regression test for a real bug found live: a driver deploy failed with
// "SetupCopyOEMInf(...): The system cannot find the file specified" for a
// Lexmark driver whose real archive (ArchEntry.ArchivePath) was a nested
// .msi living inside .pdt-infcache - Lexmark's own real package is an outer
// self-extracting RAR whose selective .inf-only extraction also reveals
// several inner .msi files (needed to find their own .inf entries in turn -
// see ensureMsiInfsExtracted's own ordering comment in catalog.go).
//
// ExtractionCache's own "already extracted, skip" check used to treat
// the nested archive's own PRE-EXISTING .inf-only cache folder (created
// earlier by the catalog-scan side, containing just the cached .inf, none
// of the companion files a real deploy needs alongside it) as if it were
// already a full extraction - because both computed the exact same
// destination path for an archive already living inside .pdt-infcache. This
// reproduces that exact shape with a synthetic zip-in-zip fixture (Lexmark's
// own real self-extracting-RAR-in-an-.exe/.msi shape needs 7z/msiexec to
// even construct a fixture for - a zip-in-zip exercises the identical
// ExtractionCache code path without either).
func TestExtractionCache_NestedArchiveInsideInfCache(t *testing.T) {
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
	cache := NewExtractionCache()
	defer cache.Close()
	extractedDir, err := cache.Ensure(nestedInnerZip)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if filepath.Dir(extractedDir) == cacheDir {
		t.Fatalf("ExtractionCache returned the pre-existing .inf-only cache folder (%s) instead of a real, freshly-extracted one - the exact bug found live", extractedDir)
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

// The extraction goes to the temp folder - nothing is written next to the
// archive in the Drivers repo - and Close removes it.
func TestExtractionCache_ExtractsToTempNotNextToArchive(t *testing.T) {
	repo := t.TempDir()
	zipPath := filepath.Join(repo, "Driver.zip")
	writeZipEntries(t, zipPath, map[string]string{"driver.inf": testZipInf, "companion.cat": "x"})

	cache := NewExtractionCache()
	dir, err := cache.Ensure(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(dir, repo) {
		t.Fatalf("extracted into the Drivers repo (%s), want the temp folder", dir)
	}
	if !strings.HasPrefix(dir, os.TempDir()) {
		t.Errorf("extracted to %s, want somewhere under the temp dir %s", dir, os.TempDir())
	}
	for _, f := range []string{"driver.inf", "companion.cat"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s in the extraction: %v", f, err)
		}
	}
	entries, _ := os.ReadDir(repo)
	if len(entries) != 1 {
		t.Errorf("the repo folder gained entries: %v", entries)
	}

	cache.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Close should remove the extraction, err=%v", err)
	}
	cache.Close() // idempotent
}

// Rows in the same run that need the same package share one extraction, kept
// until Close.
func TestExtractionCache_ReusesExtractionWithinARun(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "Driver.zip")
	writeZipEntries(t, zipPath, map[string]string{"driver.inf": testZipInf})

	cache := NewExtractionCache()
	defer cache.Close()
	first, err := cache.Ensure(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(first, "reuse-marker")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := cache.Ensure(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Errorf("second Ensure returned %s, want the same extraction %s", second, first)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the extraction was redone instead of reused: %v", err)
	}
}

func TestExtractionCache_UnknownTypeFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "thing.rar")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := NewExtractionCache()
	defer cache.Close()
	if _, err := cache.Ensure(p); err == nil {
		t.Error("expected an error for an unsupported archive type")
	}
}

// Leftovers from a run that never closed are swept once they're old enough;
// fresh ones (another instance's live extraction) are left alone.
func TestSweepStaleExtractions(t *testing.T) {
	stale, err := os.MkdirTemp("", extractionRootPrefix+"test-stale-*")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := os.MkdirTemp("", extractionRootPrefix+"test-fresh-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stale)
	defer os.RemoveAll(fresh)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	SweepStaleExtractions(24 * time.Hour)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale extraction should be gone, err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh extraction must be left alone: %v", err)
	}
}
