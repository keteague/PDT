package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"PDT/internal/driver"
)

func TestSamePath(t *testing.T) {
	dir := t.TempDir()
	if !samePath(dir, dir) {
		t.Error("expected identical paths to match")
	}
	if !samePath(filepath.Join(dir, "Drivers"), filepath.Join(dir, "drivers")) {
		t.Error("expected a case-insensitive match (Windows paths aren't case-sensitive)")
	}
	if samePath(dir, filepath.Join(dir, "Sub")) {
		t.Error("expected a parent and its own subdirectory to be reported as different")
	}
}

func TestListFileSizes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	sizes := listFileSizes(dir)
	if len(sizes) != 2 {
		t.Fatalf("got %d entries, want 2: %v", len(sizes), sizes)
	}
	if sizes["a.txt"] != 5 {
		t.Errorf("a.txt size = %d, want 5", sizes["a.txt"])
	}
	if sizes["sub/b.txt"] != 2 {
		t.Errorf("sub/b.txt size = %d, want 2 (want forward slashes even on Windows)", sizes["sub/b.txt"])
	}
}

func TestListFileSizes_EmptyForMissingRoot(t *testing.T) {
	sizes := listFileSizes(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(sizes) != 0 {
		t.Errorf("expected an empty map for a nonexistent root, got %v", sizes)
	}
}

func TestCopyTreeMerge_MergesIntoAlreadyPopulatedDestination(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "unchanged.txt"), []byte("same size"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	// Pre-existing files at the destination - the exact scenario os.CopyFS
	// refuses outright (see its own documentation: "will not overwrite
	// existing files").
	if err := os.WriteFile(filepath.Join(dest, "unchanged.txt"), []byte("same size"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "only-at-dest.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyTreeMerge(context.Background(), dest, src, nil); err != nil {
		t.Fatalf("copyTreeMerge failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "new.txt"))
	if err != nil || string(data) != "new" {
		t.Errorf("expected new.txt to be copied over: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "only-at-dest.txt")); err != nil {
		t.Errorf("expected a file only present at the destination to be left alone: %v", err)
	}
}

// TestCopyTreeMerge_OneFailingFileDoesNotStopTheRest guards against a real
// bug: filepath.WalkDir aborts entirely the moment its callback returns a
// non-nil error, so the very first version of copyTreeMerge - despite fixing
// os.CopyFS's own refuse-to-overwrite problem - silently dropped every file
// (and, worse, every remaining top-level manufacturer folder, alphabetically
// after whichever one failed) once it hit any single file it couldn't copy.
// Confirmed live against a real flash drive: Lexmark's own driver package
// (thousands of deeply nested files) hit an error partway through, and
// Ricoh/Sharp/Toshiba/Xerox - all sorting after Lexmark - never got copied
// at all as a result.
func TestCopyTreeMerge_OneFailingFileDoesNotStopTheRest(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a-before.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "m-poisoned.txt"), []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "z-after.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	// Force a real, guaranteed copy failure for the middle file specifically:
	// a directory already sitting where copyFileIfChanged expects to open a
	// file for writing.
	if err := os.MkdirAll(filepath.Join(dest, "m-poisoned.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := copyTreeMerge(context.Background(), dest, src, nil)
	if err == nil {
		t.Error("expected a non-nil error reporting the poisoned file's own failure")
	}

	if _, err := os.Stat(filepath.Join(dest, "a-before.txt")); err != nil {
		t.Errorf("expected the file before the poisoned one to still be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "z-after.txt")); err != nil {
		t.Errorf("expected the file after the poisoned one to still be copied - this is the exact real bug (Ricoh/Sharp/Toshiba/Xerox never copied after Lexmark failed): %v", err)
	}
}

// TestCopyTreeMerge_ConcurrentCopyIsCorrectAndComplete stresses the
// copyTreeWorkers-wide worker pool with enough files (well more than
// copyTreeWorkers) that every worker goroutine necessarily handles more than
// one job, guarding against the class of bug concurrency introduces that a
// single-threaded implementation can't have: a file silently dropped or
// double-processed, a corrupted/truncated copy from a shared buffer, or a
// progress total that doesn't land exactly on 100% at the end.
func TestCopyTreeMerge_ConcurrentCopyIsCorrectAndComplete(t *testing.T) {
	src := t.TempDir()
	const fileCount = 200
	var wantTotalBytes int64
	for i := 0; i < fileCount; i++ {
		content := fmt.Sprintf("file number %d\n", i)
		if err := os.WriteFile(filepath.Join(src, fmt.Sprintf("file-%03d.txt", i)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		wantTotalBytes += int64(len(content))
	}

	dest := t.TempDir()
	var (
		mu       sync.Mutex
		progress []CopyProgress
	)
	err := copyTreeMerge(context.Background(), dest, src, func(p CopyProgress) {
		mu.Lock()
		progress = append(progress, p)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("copyTreeMerge failed: %v", err)
	}

	for i := 0; i < fileCount; i++ {
		want := fmt.Sprintf("file number %d\n", i)
		got, err := os.ReadFile(filepath.Join(dest, fmt.Sprintf("file-%03d.txt", i)))
		if err != nil {
			t.Errorf("file-%03d.txt missing at destination: %v", i, err)
			continue
		}
		if string(got) != want {
			t.Errorf("file-%03d.txt content = %q, want %q (corrupted by concurrent copy?)", i, got, want)
		}
	}

	if len(progress) != fileCount {
		t.Fatalf("got %d progress callbacks, want exactly %d (one per file, no drops/duplicates)", len(progress), fileCount)
	}
	last := progress[len(progress)-1]
	if last.DoneFiles != fileCount || last.TotalFiles != fileCount {
		t.Errorf("final progress DoneFiles/TotalFiles = %d/%d, want %d/%d", last.DoneFiles, last.TotalFiles, fileCount, fileCount)
	}
	if last.DoneBytes != wantTotalBytes || last.TotalBytes != wantTotalBytes {
		t.Errorf("final progress DoneBytes/TotalBytes = %d/%d, want %d/%d", last.DoneBytes, last.TotalBytes, wantTotalBytes, wantTotalBytes)
	}
}

// TestCopyTreeMerge_PreservesSourceModTime guards a real, if minor, gap:
// copyFile's own os.OpenFile(O_TRUNC) write stamps destPath's mtime as
// whatever moment the copy happened, not the source file's own original
// date - so every driver package looked freshly downloaded today after any
// Sync/Write to Flash Drive, no matter how old it actually was. Ken asked
// for original timestamps to survive a sync; this is the local half of that
// (see internal/cloudsync's own mtimeMetaKey for the R2 half).
func TestCopyTreeMerge_PreservesSourceModTime(t *testing.T) {
	src := t.TempDir()
	srcFile := filepath.Join(src, "old-driver.zip")
	if err := os.WriteFile(srcFile, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantModTime := time.Date(2019, 3, 14, 9, 0, 0, 0, time.UTC)
	if err := os.Chtimes(srcFile, wantModTime, wantModTime); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := copyTreeMerge(context.Background(), dest, src, nil); err != nil {
		t.Fatalf("copyTreeMerge failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dest, "old-driver.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(wantModTime) {
		t.Errorf("copied file's mtime = %v, want the source's own %v", info.ModTime(), wantModTime)
	}
}

// symlinkOrSkip creates a symlink and skips the test if this machine can't
// (Windows requires either Developer Mode or an elevated process to create
// one at all - "A required privilege is not held by the client" otherwise),
// rather than failing a test suite run on a machine that simply doesn't have
// that configured.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks on this machine (%v) - skipping", err)
	}
}

// TestCopyTreeMerge_DereferencesDirectorySymlink guards the exact real bug
// reported live: a real Drivers folder can alias several macOS version
// folders to one real shared driver folder via a directory symlink/junction
// (e.g. "15-Sequoia" -> "26-Tahoe", to avoid keeping duplicate copies
// locally) - filepath.WalkDir doesn't follow it, so it was treated as a
// small non-directory file, and copying "it" created a truncated, silently
// empty 0-byte file at the destination instead of any of the real content.
// exFAT (what every flash drive is formatted as) can't represent a
// symlink/junction at all, so the fix is to follow the link and copy its
// real resolved content in its place - confirmed here by checking the
// aliased folder's file actually landed at the destination with real
// content, not as an empty stand-in.
func TestCopyTreeMerge_DereferencesDirectorySymlink(t *testing.T) {
	src := t.TempDir()
	realDir := filepath.Join(src, "26-Tahoe")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "driver.ppd"), []byte("real ppd content"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, realDir, filepath.Join(src, "15-Sequoia"))

	dest := t.TempDir()
	if err := copyTreeMerge(context.Background(), dest, src, nil); err != nil {
		t.Fatalf("copyTreeMerge failed: %v", err)
	}

	for _, name := range []string{"26-Tahoe", "15-Sequoia"} {
		data, err := os.ReadFile(filepath.Join(dest, name, "driver.ppd"))
		if err != nil {
			t.Errorf("%s/driver.ppd missing at destination: %v", name, err)
			continue
		}
		if string(data) != "real ppd content" {
			t.Errorf("%s/driver.ppd content = %q, want the real content, not an empty stand-in", name, data)
		}
	}
}

// TestCopyTreeMerge_DereferencesFileSymlink guards the file-level version of
// the same problem - a symlink to a single file, not a whole directory.
func TestCopyTreeMerge_DereferencesFileSymlink(t *testing.T) {
	src := t.TempDir()
	realFile := filepath.Join(src, "real.inf")
	if err := os.WriteFile(realFile, []byte("real inf content"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, realFile, filepath.Join(src, "alias.inf"))

	dest := t.TempDir()
	if err := copyTreeMerge(context.Background(), dest, src, nil); err != nil {
		t.Fatalf("copyTreeMerge failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "alias.inf"))
	if err != nil {
		t.Fatalf("alias.inf missing at destination: %v", err)
	}
	if string(data) != "real inf content" {
		t.Errorf("alias.inf content = %q, want the real content, not an empty stand-in", data)
	}
}

// TestCopyTreeMerge_SymlinkCycleDoesNotHang guards against a directory
// symlink that resolves to one of its own ancestors - without ancestor
// tracking, collectCopyJobs would recurse into that same real directory
// forever. This must report an error and return, not hang or crash.
func TestCopyTreeMerge_SymlinkCycleDoesNotHang(t *testing.T) {
	src := t.TempDir()
	sub := filepath.Join(src, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// sub/loop points back at src itself - a direct cycle.
	symlinkOrSkip(t, src, filepath.Join(sub, "loop"))

	dest := t.TempDir()
	done := make(chan error, 1)
	go func() { done <- copyTreeMerge(context.Background(), dest, src, nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("expected an error reporting the symlink cycle")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("copyTreeMerge did not return - likely stuck in a symlink cycle")
	}
}

// TestCopyFile_CanceledContextDeletesPartialDestinationFile is the direct
// regression test for the Cancel button's own requirement: "immediately stop
// the current transfer and delete the file that was in transit, so that we
// don't end up with a partially copied file." An already-canceled context
// makes ctxReader's first Read call fail before any real bytes are copied -
// deterministic and instant, rather than racing real disk I/O to catch a
// copy truly mid-flight - but exercises the exact same cleanup path
// (copyFile's own deferred os.Remove) that a cancellation landing partway
// through a large real file would hit.
func TestCopyFile_CanceledContextDeletesPartialDestinationFile(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(srcPath, []byte("real content that must never land at the destination"), 0o644); err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(dir, "dest.bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	buf := make([]byte, copyBufferSize)
	err := copyFile(ctx, destPath, srcPath, time.Now(), buf)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copyFile with an already-canceled context returned %v, want an error wrapping context.Canceled", err)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Errorf("expected the partially-copied destination file to be deleted after cancellation, got stat err=%v", statErr)
	}
}

// TestCopyTreeMerge_CancellationStopsEarlyAndReportsCanceled guards the
// batch-level half of the same requirement: an already-canceled context must
// stop copyTreeMerge from starting fresh files (not just abort one already
// in flight), and the returned error must let a caller (SyncDriversToFlashDrives)
// tell "the user hit Cancel" apart from a genuine per-file failure via
// errors.Is(err, context.Canceled).
func TestCopyTreeMerge_CancellationStopsEarlyAndReportsCanceled(t *testing.T) {
	src := t.TempDir()
	const fileCount = 50
	for i := 0; i < fileCount; i++ {
		if err := os.WriteFile(filepath.Join(src, fmt.Sprintf("file-%03d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dest := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := copyTreeMerge(ctx, dest, src, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the returned error to report cancellation via errors.Is, got %v", err)
	}

	entries, readErr := os.ReadDir(dest)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) == fileCount {
		t.Errorf("expected an already-canceled context to stop copying before every file was processed, but all %d landed at the destination", fileCount)
	}
}

// TestCopyTreeMerge_SkipsExtractedSiblingFolder is copyTreeMerge's own
// integration test for GitHub issue #10's Sync-side fix: an archive's own
// extracted sibling folder (see driver.ExtractedSiblingDirs) must never be
// copied - it's a derived, re-creatable artifact of the archive sitting
// right next to it, and syncing it too is exactly what made a real Drivers
// folder 5.4GB/22,570 files instead of ~1.5GB/~25.
func TestCopyTreeMerge_SkipsExtractedSiblingFolder(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "Driver.zip"), []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	extractedDir := filepath.Join(src, "Driver")
	if err := os.MkdirAll(extractedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extractedDir, "driver.inf"), []byte("extracted content"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := copyTreeMerge(context.Background(), dest, src, nil); err != nil {
		t.Fatalf("copyTreeMerge failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "Driver.zip")); err != nil {
		t.Errorf("expected the archive itself to still be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Driver")); !os.IsNotExist(err) {
		t.Errorf("expected Driver/ (the archive's extracted sibling) to be skipped entirely, got err=%v", err)
	}
}

// TestCopyTreeMerge_CopiesPdtInfCacheFolder guards PdtInfCacheDirName's own
// exception to the general dotfile/dotfolder skip rule (see
// TestCopyTreeMerge_SkipsDotEntries) - unlike an archive's extracted
// sibling folder, the .inf-only metadata cache the catalog rework writes
// into is real, useful content, and both Write to Flash Drive and Sync
// (both built on copyTreeMerge) must carry it and everything under it along
// with the rest of the folder rather than dropping it.
func TestCopyTreeMerge_CopiesPdtInfCacheFolder(t *testing.T) {
	src := t.TempDir()
	cacheDir := filepath.Join(src, driver.PdtInfCacheDirName, "Driver")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "driver.inf"), []byte("cached inf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("real file"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := copyTreeMerge(context.Background(), dest, src, nil); err != nil {
		t.Fatalf("copyTreeMerge failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "real.txt")); err != nil {
		t.Errorf("expected the unrelated real file to still be copied: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, driver.PdtInfCacheDirName, "Driver", "driver.inf")); err != nil {
		t.Errorf("expected %s and its contents to be copied, got err=%v", driver.PdtInfCacheDirName, err)
	} else if string(got) != "cached inf" {
		t.Errorf("got %q, want %q", got, "cached inf")
	}
}

// TestCopyTreeMerge_SkipsDotEntries guards the general dotfile/dotfolder
// skip rule (driver.IsIgnoredDotEntry) - anything dot-prefixed other than
// PdtInfCacheDirName itself (see TestCopyTreeMerge_CopiesPdtInfCacheFolder)
// is clutter, not real driver content, and must never be transferred.
func TestCopyTreeMerge_SkipsDotEntries(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, ".gitignore"), []byte("*.log"), 0o644); err != nil {
		t.Fatal(err)
	}
	dotDir := filepath.Join(src, ".git", "objects")
	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotDir, "pack"), []byte("git internals"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("real file"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := copyTreeMerge(context.Background(), dest, src, nil); err != nil {
		t.Fatalf("copyTreeMerge failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "real.txt")); err != nil {
		t.Errorf("expected the unrelated real file to still be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("expected .gitignore to be skipped, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Errorf("expected .git/ to be skipped entirely, got err=%v", err)
	}
}

// TestCopyTreeMerge_SkipsDSStore guards Finder's own per-folder metadata
// clutter (macOS creates a .DS_Store in nearly every folder it browses,
// including a Drivers folder synced to/from a Windows machine) - never real
// driver content, so both Sync and Write to Flash Drive (both built on
// copyTreeMerge) must never transfer it.
func TestCopyTreeMerge_SkipsDSStore(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, driver.DSStoreFileName), []byte("finder metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("real file"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := copyTreeMerge(context.Background(), dest, src, nil); err != nil {
		t.Fatalf("copyTreeMerge failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "real.txt")); err != nil {
		t.Errorf("expected the unrelated real file to still be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, driver.DSStoreFileName)); !os.IsNotExist(err) {
		t.Errorf("expected %s to be skipped entirely, got err=%v", driver.DSStoreFileName, err)
	}
}
