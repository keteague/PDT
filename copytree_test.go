package main

import (
	"os"
	"path/filepath"
	"testing"
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
