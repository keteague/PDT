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
	writeSourceMarker(root, cacheDir, archivePath)

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
	writeSourceMarker(root, cacheDir, archivePath)

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
// TestIsIgnoredDotEntry is the direct regression test for a real bug found
// live (2026-10-02, GitHub issue: stale .pdt-source markers never replaced
// by Sync): the PdtInfCacheDirName exemption only ever matched the folder
// name itself, never pdtSourceMarkerName - a dot-prefixed FILE sitting
// inside that folder's own subfolders. Sync (collectCopyJobs, copytree.go)
// calls this once per entry at every recursion depth, so a marker file was
// silently excluded from every copy job, on every sync, including the very
// first one onto a brand-new drive - not "copied but skipped as unchanged"
// (a red herring: copyTreeMerge's own size comparison never even got the
// chance to run), but never copied at all.
func TestIsIgnoredDotEntry(t *testing.T) {
	ignored := []string{".DS_Store", ".git", ".foo.pdt-partial"}
	for _, name := range ignored {
		if !IsIgnoredDotEntry(name) {
			t.Errorf("IsIgnoredDotEntry(%q) = false, want true (real clutter, not PDT's own)", name)
		}
	}
	kept := []string{PdtInfCacheDirName, pdtSourceMarkerName, "driver.inf", "Foo.zip"}
	for _, name := range kept {
		if IsIgnoredDotEntry(name) {
			t.Errorf("IsIgnoredDotEntry(%q) = true, want false (must travel with the rest of the folder)", name)
		}
	}
}

// TestWriteSourceMarker_StoresRootRelativeNotAbsolute is writeSourceMarker's
// own direct regression test for Ken's own explicit design (2026-10-02,
// GitHub issue: stale .pdt-source markers): the marker's stored bytes must
// never carry a drive letter or absolute machine-specific prefix at all, so
// it's portable by construction rather than merely recoverable after the
// fact when it turns out stale.
func TestWriteSourceMarker_StoresRootRelativeNotAbsolute(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Canon") // any absolute mfgPath will do
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(root, PdtInfCacheDirName, "Foo")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "Foo.zip")

	writeSourceMarker(root, destDir, archivePath)

	data, err := os.ReadFile(filepath.Join(destDir, pdtSourceMarkerName))
	if err != nil {
		t.Fatal(err)
	}
	stored := string(data)
	if stored != "Foo.zip" {
		t.Errorf("writeSourceMarker stored %q, want the plain root-relative %q", stored, "Foo.zip")
	}
	if filepath.VolumeName(stored) != "" {
		t.Errorf("writeSourceMarker stored %q, which still carries a volume/drive prefix - not portable", stored)
	}
}

// TestFindSourceArchive_NewFormatMarkerPortableAcrossRoots is the real,
// end-to-end point of storing markers root-relative: the identical marker
// content resolves correctly under two structurally-identical trees sitting
// at entirely different absolute locations (standing in for "the same
// Drivers folder, mounted at a different drive letter") - not just
// recoverable after the fact (that's recoverArchivePathFromMarkerLocation's
// own job, for a marker an older PDT version already wrote absolute), but
// correct by construction from the moment it's written.
func TestFindSourceArchive_NewFormatMarkerPortableAcrossRoots(t *testing.T) {
	build := func(t *testing.T) (mfgPath string) {
		t.Helper()
		root := t.TempDir()
		mfgPath = filepath.Join(root, "Canon")
		if err := os.MkdirAll(mfgPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mfgPath, "Foo.zip"), []byte("not a real zip"), 0o644); err != nil {
			t.Fatal(err)
		}
		destDir := filepath.Join(mfgPath, PdtInfCacheDirName, "Foo")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeSourceMarker(mfgPath, destDir, filepath.Join(mfgPath, "Foo.zip"))
		return mfgPath
	}

	for _, mfgPath := range []string{build(t), build(t)} {
		destDir := filepath.Join(mfgPath, PdtInfCacheDirName, "Foo")
		want := filepath.Join(mfgPath, "Foo.zip")
		got, markerDir := findSourceArchive(destDir, mfgPath)
		if got != want {
			t.Errorf("findSourceArchive under %q = %q, want %q", mfgPath, got, want)
		}
		if markerDir != destDir {
			t.Errorf("findSourceArchive markerDir = %q, want %q", markerDir, destDir)
		}
	}
}

// TestRecoverArchivePathFromMarkerLocation_StaleDriveLetterRecovered is the
// real bug's own regression test (2026-10-02, confirmed live): a
// .pdt-source marker whose stored contents point at a drive that doesn't
// exist on this machine at all - here "E:Drivers\Windows\11\Sharp\..."
// (even reproducing the missing separator a historical resolveAgainstExe-
// class bug baked into the real marker this was confirmed against) - gets
// recovered from the marker's own live location once the real archive is
// found sitting where that location implies, on root.
func TestRecoverArchivePathFromMarkerLocation_StaleDriveLetterRecovered(t *testing.T) {
	root := t.TempDir()
	realArchive := filepath.Join(root, "UD3_07_PCL6_2510a.zip")
	if err := os.WriteFile(realArchive, []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	markerDir := filepath.Join(root, PdtInfCacheDirName, "UD3_07_PCL6_2510a")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := `E:Drivers\Windows\11\Sharp\UD3_07_PCL6_2510a.zip`

	got := recoverArchivePathFromMarkerLocation(root, markerDir, stale)
	if got != realArchive {
		t.Errorf("recoverArchivePathFromMarkerLocation(%q, %q, %q) = %q, want %q", root, markerDir, stale, got, realArchive)
	}
}

// TestRecoverArchivePathFromMarkerLocation_NestedCacheEntryLeftAlone: a
// marker whose markerDir isn't under root/PdtInfCacheDirName at all (the
// filepath.Rel guard's own ".." case) returns storedArchivePath unchanged
// rather than computing something nonsensical.
func TestRecoverArchivePathFromMarkerLocation_NestedCacheEntryLeftAlone(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	stale := `E:Drivers\Windows\11\Sharp\Foo.zip`
	if got := recoverArchivePathFromMarkerLocation(root, outsideDir, stale); got != stale {
		t.Errorf("recoverArchivePathFromMarkerLocation with markerDir outside root's cache = %q, want unchanged %q", got, stale)
	}
}

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
	writeSourceMarker(root, done, filepath.Join(root, "x.zip"))
	if !prepareInfCacheDest(done) {
		t.Error("a folder carrying its source marker is a finished extraction and must be left alone")
	}
}
