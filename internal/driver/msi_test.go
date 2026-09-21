package driver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompressedSiblingRe(t *testing.T) {
	matches := []string{"LMUD1o40.gd_", "LMUD1o40.dl_64", "lmud1o40.in_", "foo.EX_"}
	for _, name := range matches {
		if !compressedSiblingRe.MatchString(name) {
			t.Errorf("expected %q to match compressedSiblingRe", name)
		}
	}

	nonMatches := []string{"LMUD1o40.inf", "LMUD1o40.dll", "LMUD1o40.gpd", "Setup.exe", "readme.txt"}
	for _, name := range nonMatches {
		if compressedSiblingRe.MatchString(name) {
			t.Errorf("did not expect %q to match compressedSiblingRe", name)
		}
	}
}

func TestEnsureMsiExtracted_SkipsAlreadyExtracted(t *testing.T) {
	dir := t.TempDir()
	msiPath := filepath.Join(dir, "Foo.msi")
	if err := os.WriteFile(msiPath, []byte("not a real msi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The destination folder already exists (however it got there) -
	// extraction should be skipped entirely, never attempting to run
	// msiexec against this deliberately-invalid fake .msi.
	if err := os.Mkdir(filepath.Join(dir, "Foo"), 0o755); err != nil {
		t.Fatal(err)
	}

	ensureMsiExtracted(dir)

	if _, err := os.Stat(filepath.Join(dir, "Foo")); err != nil {
		t.Error("expected the already-extracted Foo/ folder to be left alone, not removed")
	}
}

// TestEnsureMsiExtracted_DoesNotRecurseIntoOwnExtractionOutput guards the
// real, confirmed-live bug: a package's own administrative-install output
// can legitimately contain a verbatim copy of itself one level in (Canon's
// DiasSetup.msi does this, apparently for its own uninstaller's use). Every
// fresh run used to treat that leftover nested .msi as new, unextracted
// work and extract it again - nesting one level deeper on every single run,
// forever, with no bound (confirmed live: 12+ levels, 100+MB, zero .inf
// files gained). This fixture simulates exactly one such already-completed
// extraction (Foo.msi -> Foo/, containing a real nested Foo.msi) and
// confirms a second run doesn't extract that nested copy into a further
// Foo/Foo/.
func TestEnsureMsiExtracted_DoesNotRecurseIntoOwnExtractionOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.msi"), []byte("not a real msi"), 0o644); err != nil {
		t.Fatal(err)
	}
	fooDir := filepath.Join(dir, "Foo")
	if err := os.Mkdir(fooDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The genuine nested copy a real administrative install can produce.
	if err := os.WriteFile(filepath.Join(fooDir, "Foo.msi"), []byte("not a real msi"), 0o644); err != nil {
		t.Fatal(err)
	}

	ensureMsiExtracted(dir)

	if _, err := os.Stat(filepath.Join(fooDir, "Foo")); !os.IsNotExist(err) {
		t.Errorf("expected Foo/Foo/ to NOT be created - ensureMsiExtracted must not re-extract a .msi found inside its own prior extraction output, got err=%v", err)
	}
}

func TestCompressedInfRe(t *testing.T) {
	matches := []string{"LMUD1o40.in_", "LMUD1o40.IN_64", "driver.in_"}
	for _, name := range matches {
		if !compressedInfRe.MatchString(name) {
			t.Errorf("expected %q to match compressedInfRe", name)
		}
	}
	nonMatches := []string{"LMUD1o40.gd_", "LMUD1o40.dl_64", "driver.inf", "driver.ini"}
	for _, name := range nonMatches {
		if compressedInfRe.MatchString(name) {
			t.Errorf("did not expect %q to match compressedInfRe", name)
		}
	}
}

// TestCopyInfsFromExtractedTree_OnlyInfsLandInCache is
// copyInfsFromExtractedTree's own regression test for GitHub issue #10's
// whole premise - see TestEnsureZipInfsExtracted_OnlyInfsLandInCache's own
// doc comment for the full reasoning. Tested directly against a synthetic
// "already extracted" tree (no real .msi/msiexec needed at all - see
// copyInfsFromExtractedTree's own doc comment for why it's split out this
// way).
func TestCopyInfsFromExtractedTree_OnlyInfsLandInCache(t *testing.T) {
	src := t.TempDir()
	driverDir := filepath.Join(src, "Driver")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(driverDir, "driver.inf"), []byte(testZipInf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(driverDir, "driver.cat"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(driverDir, "driver.dll"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := copyInfsFromExtractedTree(src, dest); err != nil {
		t.Fatalf("copyInfsFromExtractedTree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "Driver", "driver.inf")); err != nil {
		t.Errorf("expected the .inf to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Driver", "driver.cat")); !os.IsNotExist(err) {
		t.Errorf("expected the .cat payload file to NOT be copied, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Driver", "driver.dll")); !os.IsNotExist(err) {
		t.Errorf("expected the .dll payload file to NOT be copied, got err=%v", err)
	}
}

func TestCopyInfsFromExtractedTree_ErrorsWhenNoInfFound(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "readme.txt"), []byte("nothing useful here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyInfsFromExtractedTree(src, t.TempDir()); err == nil {
		t.Error("expected an error when no .inf is found anywhere in the tree")
	}
}

// A nested .msi that was pulled into .pdt-infcache only to reach its own
// .inf files is deleted once those are cached - including one an older PDT
// version left behind. An .msi in the real Drivers folder is the source
// archive and is never touched.
func TestEnsureMsiInfsExtracted_RemovesProcessedMsiFromCacheOnly(t *testing.T) {
	dir := t.TempDir()
	cacheRoot := filepath.Join(dir, PdtInfCacheDirName)

	nested := filepath.Join(cacheRoot, "Pkg", "Drivers", "x64", "print64PCL.msi")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("not a real msi"), 0o644); err != nil {
		t.Fatal(err)
	}
	nestedDest := filepath.Join(cacheRoot, "Pkg", "Drivers", "x64", "print64PCL")
	if err := os.MkdirAll(nestedDest, 0o755); err != nil {
		t.Fatal(err)
	}
	inf := filepath.Join(nestedDest, "LMUD1o40.inf")
	if err := os.WriteFile(inf, []byte("; inf"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSourceMarker(nestedDest, nested)

	real := filepath.Join(dir, "Real.msi")
	if err := os.WriteFile(real, []byte("not a real msi"), 0o644); err != nil {
		t.Fatal(err)
	}
	realDest := filepath.Join(cacheRoot, "Real")
	if err := os.MkdirAll(realDest, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSourceMarker(realDest, real)

	ensureMsiInfsExtracted(dir)

	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Error("expected the processed nested .msi inside the cache to be removed")
	}
	if _, err := os.Stat(inf); err != nil {
		t.Error("expected the cached .inf to be kept")
	}
	if _, err := os.Stat(real); err != nil {
		t.Error("expected an .msi outside the cache (the real source archive) to be left alone")
	}
}

func TestEnsureMsiInfsExtracted_SkipsAlreadyCached(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.msi"), []byte("not a real msi"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(dir, PdtInfCacheDirName, "Foo")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A finished extraction always ends with its source marker.
	writeSourceMarker(cacheDir, filepath.Join(dir, "Foo.msi"))

	ensureMsiInfsExtracted(dir)
	// No assertion beyond "didn't try to run msiexec against this
	// deliberately-invalid fake .msi" - if the skip check didn't fire,
	// extractInfsFromMsi would have failed and os.RemoveAll'd cacheDir.
	if _, err := os.Stat(cacheDir); err != nil {
		t.Error("expected the already-cached Foo/ folder to be left alone, not removed")
	}
}
