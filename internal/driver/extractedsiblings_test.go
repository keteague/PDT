package driver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractedSiblingDirs_ZipMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.zip"), []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "Foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractedSiblingDirs(dir, entries)
	if !got["Foo"] {
		t.Errorf("expected Foo/ to be flagged as Foo.zip's extracted sibling, got %v", got)
	}
}

func TestExtractedSiblingDirs_MsiMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Driver.msi"), []byte("not a real msi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "Driver"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractedSiblingDirs(dir, entries)
	if !got["Driver"] {
		t.Errorf("expected Driver/ to be flagged as Driver.msi's extracted sibling, got %v", got)
	}
}

func TestExtractedSiblingDirs_SelfExtractingExeMatch(t *testing.T) {
	dir := t.TempDir()
	// A real Zip local file header signature - enough for isSelfExtractingArchive
	// to recognize this as a self-extracting archive without needing a real one.
	content := append([]byte{'P', 'K', 0x03, 0x04}, make([]byte, 64)...)
	if err := os.WriteFile(filepath.Join(dir, "Setup.exe"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "Setup"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractedSiblingDirs(dir, entries)
	if !got["Setup"] {
		t.Errorf("expected Setup/ to be flagged as the self-extracting Setup.exe's sibling, got %v", got)
	}
}

// TestExtractedSiblingDirs_OrdinaryExeNotMatched guards against flagging a
// same-named folder next to an ordinary (non-archive) .exe - most .exe files
// found in a real Drivers folder are legitimate installer tools, not
// archives, and must not be mistaken for one just because a folder of the
// same name happens to sit beside them.
func TestExtractedSiblingDirs_OrdinaryExeNotMatched(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Readme.exe"), []byte("just an ordinary tool, not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "Readme"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractedSiblingDirs(dir, entries)
	if got["Readme"] {
		t.Error("did not expect Readme/ to be flagged - Readme.exe has no archive signature")
	}
}

// TestExtractedSiblingDirs_KyoceraVersionSubstringMatch guards Kyocera's own
// non-basename naming convention (kyoceraexe.go's own
// kyoceraVersionAlreadyExtracted) - the destination folder name only needs
// to CONTAIN the exe's version token, not match it exactly.
func TestExtractedSiblingDirs_KyoceraVersionSubstringMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "KXDriver_8.6.1022.exe"), []byte("kyocera package"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "KXDriver_8.6.1022"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractedSiblingDirs(dir, entries)
	if !got["KXDriver_8.6.1022"] {
		t.Errorf("expected KXDriver_8.6.1022/ to be flagged via version-substring match, got %v", got)
	}
}

func TestExtractedSiblingDirs_UnrelatedFolderNotMatched(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.zip"), []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "Archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractedSiblingDirs(dir, entries)
	if len(got) != 0 {
		t.Errorf("expected no folders flagged - Archive/ doesn't correspond to any archive here, got %v", got)
	}
}

func TestExtractedSiblingDirs_ArchiveWithNoExtractedFolderYet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.zip"), []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractedSiblingDirs(dir, entries)
	if len(got) != 0 {
		t.Errorf("expected no folders flagged - Foo/ doesn't exist yet, got %v", got)
	}
}

// TestPruneOrphanedInfCache_RemovesEntryWhoseArchiveIsGone is the direct
// regression test for the real correctness gap found live: without this,
// scanManufacturerFolders' own .inf-discovery walk would keep finding and
// parsing a removed/archived driver's stale cached .inf indefinitely, since
// it only checks whether a .inf file is actually sitting on disk - never
// whether the archive that produced it still exists.
func TestPruneOrphanedInfCache_RemovesEntryWhoseArchiveIsGone(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "Foo.zip")
	if err := os.WriteFile(archivePath, []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(root, PdtInfCacheDirName, "Foo")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "driver.inf"), []byte(testZipInf), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSourceMarker(cacheDir, archivePath)

	// The archive is now removed/archived - simulating a technician
	// deleting or moving Foo.zip out of the Drivers folder.
	if err := os.Remove(archivePath); err != nil {
		t.Fatal(err)
	}

	pruneOrphanedInfCache(root)

	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Errorf("expected the orphaned cache entry to be removed once its source archive was gone, got err=%v", err)
	}
}

func TestPruneOrphanedInfCache_KeepsEntryWhoseArchiveStillExists(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "Foo.zip")
	if err := os.WriteFile(archivePath, []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(root, PdtInfCacheDirName, "Foo")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSourceMarker(cacheDir, archivePath)

	pruneOrphanedInfCache(root)

	if _, err := os.Stat(cacheDir); err != nil {
		t.Errorf("expected the still-valid cache entry to be left alone, got err=%v", err)
	}
}

func TestPruneOrphanedInfCache_LeavesUnmarkedEntryAlone(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, PdtInfCacheDirName, "NoMarker")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pruneOrphanedInfCache(root)

	if _, err := os.Stat(cacheDir); err != nil {
		t.Errorf("expected an entry with no marker at all to be left alone rather than guessed at, got err=%v", err)
	}
}

// TestPrepareInfCacheDest covers the three states an .inf-only cache
// destination can be in (Ken, 2026-09-20 - Toshiba's zip and Lexmark's
// self-extracting package both sat in the second one, left by an older PDT
// version, and were trusted forever because the folder merely existed).
func TestPrepareInfCacheDest(t *testing.T) {
	root := t.TempDir()

	missing := filepath.Join(root, "missing")
	if prepareInfCacheDest(missing) {
		t.Error("a destination that doesn't exist is not 'already extracted'")
	}

	incomplete := filepath.Join(root, "incomplete", "Driver")
	if err := os.MkdirAll(incomplete, 0o755); err != nil {
		t.Fatal(err)
	}
	top := filepath.Join(root, "incomplete")
	if prepareInfCacheDest(top) {
		t.Error("a folder with no source marker is a leftover, not a finished extraction")
	}
	if _, err := os.Stat(top); !os.IsNotExist(err) {
		t.Errorf("the incomplete folder should have been removed for a clean re-extract, got err=%v", err)
	}

	done := filepath.Join(root, "done")
	if err := os.MkdirAll(done, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSourceMarker(done, filepath.Join(root, "x.zip"))
	if !prepareInfCacheDest(done) {
		t.Error("a folder carrying its source marker is a finished extraction and must be left alone")
	}
}
