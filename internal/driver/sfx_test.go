package driver

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// realSevenZipPath returns a real, working 7z.exe cached on this machine
// (PDT's own per-machine cache - see the repo root's sevenzip_windows.go),
// or "" with the test skipped if nothing's cached yet - matches
// TestWritePortablePDTTo_CopiesSevenZipTools' own "skip cleanly rather than
// assert something environment-dependent" reasoning.
func realSevenZipPath(t *testing.T) string {
	t.Helper()
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Skip("no user cache dir on this machine")
	}
	path := filepath.Join(cacheDir, "PDT", "tools", "7zip", "7z.exe")
	if _, err := os.Stat(path); err != nil {
		t.Skip("no real 7z.exe cached on this machine - run PDT once to populate it")
	}
	return path
}

func TestIsSelfExtractingArchive(t *testing.T) {
	dir := t.TempDir()

	rar5 := filepath.Join(dir, "rar5.exe")
	// A native SFX stub of arbitrary bytes, then the RAR5 signature well past
	// the start - mirrors a real self-extracting package, where the signature
	// sits a few hundred KB into the file, not at offset zero.
	data := append([]byte("MZ"), make([]byte, 4096)...)
	data = append(data, []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}...)
	if err := os.WriteFile(rar5, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSelfExtractingArchive(rar5) {
		t.Error("expected a file containing the RAR5 signature to be detected as a self-extracting archive")
	}

	rar4 := filepath.Join(dir, "rar4.exe")
	data4 := append([]byte("MZ"), []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x00}...)
	if err := os.WriteFile(rar4, data4, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSelfExtractingArchive(rar4) {
		t.Error("expected a file containing the older RAR 1.5-4.x signature to be detected too")
	}

	sevenZip := filepath.Join(dir, "7z.exe")
	data7z := append([]byte("MZ"), make([]byte, 4096)...)
	data7z = append(data7z, []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C}...)
	if err := os.WriteFile(sevenZip, data7z, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSelfExtractingArchive(sevenZip) {
		t.Error("expected a file containing the 7z signature to be detected too - confirmed against a real Konica Minolta package packaged this way")
	}

	zip := filepath.Join(dir, "zip.exe")
	dataZip := append([]byte("MZ"), make([]byte, 4096)...)
	dataZip = append(dataZip, []byte{'P', 'K', 0x03, 0x04}...)
	if err := os.WriteFile(zip, dataZip, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSelfExtractingArchive(zip) {
		t.Error("expected a file containing the Zip local file header signature to be detected too")
	}

	plain := filepath.Join(dir, "plain.exe")
	if err := os.WriteFile(plain, []byte("MZ this is just an ordinary executable, not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isSelfExtractingArchive(plain) {
		t.Error("an ordinary .exe with no known archive signature should not be detected as self-extracting")
	}
}

func TestEnsureSfxArchivesExtracted_NoOpWithoutSevenZipConfigured(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = ""
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	rarPath := filepath.Join(dir, "Foo.exe")
	data := []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}
	if err := os.WriteFile(rarPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ensureSfxArchivesExtracted(dir)

	if _, err := os.Stat(filepath.Join(dir, "Foo")); !os.IsNotExist(err) {
		t.Error("expected no extraction to happen with SevenZipPath unset")
	}
}

func TestEnsureSfxArchivesExtracted_SkipsAlreadyExtracted(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = "some-path-that-would-fail-if-actually-invoked.exe"
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	rarPath := filepath.Join(dir, "Foo.exe")
	data := []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}
	if err := os.WriteFile(rarPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The destination folder already exists (however it got there - a prior
	// run of this, or a manual extraction) - extraction should be skipped
	// entirely, never even attempting to invoke SevenZipPath.
	if err := os.Mkdir(filepath.Join(dir, "Foo"), 0o755); err != nil {
		t.Fatal(err)
	}

	ensureSfxArchivesExtracted(dir)
	// No assertion needed beyond "this didn't panic/hang trying to exec a
	// bogus SevenZipPath" - if the skip-when-already-extracted check didn't
	// fire, extractSfxArchive would have tried (and failed) to run the bogus
	// path, and os.RemoveAll would have removed the Foo/ folder afterward.
	if _, err := os.Stat(filepath.Join(dir, "Foo")); err != nil {
		t.Error("expected the already-extracted Foo/ folder to be left alone, not removed")
	}
}

// TestEnsureSfxArchivesExtracted_SkipsKyoceraNamedExe guards against a real
// regression: a Kyocera driver package's raw bytes do contain a real 7z/RAR
// signature within the scan window (its .text PE section IS the embedded
// archive), so without this exclusion this function would "successfully"
// extract it into a same-named sibling folder full of nothing but raw PE
// sections - and that wrong folder's name would then satisfy
// kyoceraVersionAlreadyExtracted's own substring check, permanently blocking
// kyoceraexe.go's correct two-stage extraction from ever running for that
// version at all.
func TestEnsureSfxArchivesExtracted_SkipsKyoceraNamedExe(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = "some-path-that-would-fail-if-actually-invoked.exe"
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	kyoceraPath := filepath.Join(dir, "KXDRIVER 8.6A.1412.exe")
	// A real archive signature, same as any other test file here - the point
	// is this file WOULD be treated as a self-extracting archive if not for
	// the name-based exclusion.
	data := []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}
	if err := os.WriteFile(kyoceraPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ensureSfxArchivesExtracted(dir)

	if _, err := os.Stat(filepath.Join(dir, "KXDRIVER 8.6A.1412")); !os.IsNotExist(err) {
		t.Error("expected a Kyocera-named exe to be left entirely alone by the generic SFX extractor")
	}
}

// buildFakeSfxExe writes a real Zip archive (7z reads Zip natively, same as
// any other supported format) at exePath, prefixed with an "MZ" PE-stub-like
// byte run so isSelfExtractingArchive's own signature scan finds it exactly
// like it would a real self-extracting package - matches
// TestIsSelfExtractingArchive's own fixture-building approach.
func buildFakeSfxExe(t *testing.T, exePath string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	data := append([]byte("MZ"), make([]byte, 256)...)
	data = append(data, buf.Bytes()...)
	if err := os.WriteFile(exePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestExtractInfsFromSfxArchive_OnlyInfsLandInCache is the SFX-path
// regression test for GitHub issue #10's whole premise - see
// TestEnsureZipInfsExtracted_OnlyInfsLandInCache's own doc comment for the
// full reasoning. Uses a real cached 7z.exe (skips cleanly if none is
// cached on this machine yet).
func TestExtractInfsFromSfxArchive_OnlyInfsLandInCache(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = realSevenZipPath(t)
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "Setup.exe")
	buildFakeSfxExe(t, exePath, map[string]string{
		"Driver/setup.inf": testZipInf,
		"Driver/setup.cat": "not a real catalog file, just payload",
		"Driver/help.chm":  "not a real help file, just payload",
	})

	destDir := filepath.Join(dir, PdtInfCacheDirName, "Setup")
	if err := extractInfsFromSfxArchive(exePath, destDir); err != nil {
		t.Fatalf("extractInfsFromSfxArchive: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "Driver", "setup.inf")); err != nil {
		t.Errorf("expected the .inf to be cached: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Driver", "setup.cat")); !os.IsNotExist(err) {
		t.Errorf("expected the .cat payload file to NOT be cached, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Driver", "help.chm")); !os.IsNotExist(err) {
		t.Errorf("expected the .chm payload file to NOT be cached, got err=%v", err)
	}
}

func TestEnsureSfxArchiveInfsExtracted_NoOpWithoutSevenZipConfigured(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = ""
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	rarPath := filepath.Join(dir, "Foo.exe")
	data := []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}
	if err := os.WriteFile(rarPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ensureSfxArchiveInfsExtracted(dir)

	if _, err := os.Stat(filepath.Join(dir, PdtInfCacheDirName)); !os.IsNotExist(err) {
		t.Error("expected no extraction to happen with SevenZipPath unset")
	}
}

func TestEnsureSfxArchiveInfsExtracted_SkipsAlreadyCached(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = "some-path-that-would-fail-if-actually-invoked.exe"
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	rarPath := filepath.Join(dir, "Foo.exe")
	data := []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}
	if err := os.WriteFile(rarPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(dir, PdtInfCacheDirName, "Foo")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ensureSfxArchiveInfsExtracted(dir)
	// No assertion needed beyond "this didn't panic/hang trying to exec a
	// bogus SevenZipPath" - if the skip-when-already-cached check didn't
	// fire, extractInfsFromSfxArchive would have tried (and failed) to run
	// the bogus path, and os.RemoveAll would have removed cacheDir afterward.
	if _, err := os.Stat(cacheDir); err != nil {
		t.Error("expected the already-cached Foo/ folder to be left alone, not removed")
	}
}
