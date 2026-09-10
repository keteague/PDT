package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

	if err := copyTreeMerge(dest, src, nil); err != nil {
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

	err := copyTreeMerge(dest, src, nil)
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
	err := copyTreeMerge(dest, src, func(p CopyProgress) {
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
	if err := copyTreeMerge(dest, src, nil); err != nil {
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
	if err := copyTreeMerge(dest, src, nil); err != nil {
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
	go func() { done <- copyTreeMerge(dest, src, nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("expected an error reporting the symlink cycle")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("copyTreeMerge did not return - likely stuck in a symlink cycle")
	}
}
