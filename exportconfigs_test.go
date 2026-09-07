package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMatchingPreinstallFolders(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "18455-1 - ABC Company - 322 E 21st St S"))
	mustMkdir(t, filepath.Join(dir, "18455-1 - ABC Company - Second Visit"))
	mustMkdir(t, filepath.Join(dir, "18455-10 - Not A Match"))
	mustMkdir(t, filepath.Join(dir, "Unrelated Folder"))
	mustWriteFile(t, filepath.Join(dir, "18455-1 - stray file, not a dir"))

	got, err := matchingPreinstallFolders(dir, "18455-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "18455-1 - ABC Company - 322 E 21st St S"),
		filepath.Join(dir, "18455-1 - ABC Company - Second Visit"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("matchingPreinstallFolders = %v, want %v", got, want)
	}
}

func TestMatchingPreinstallFolders_NoMatches(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "99999-1 - Someone Else"))

	got, err := matchingPreinstallFolders(dir, "18455-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected no matches, got %v", got)
	}
}

func TestMatchingConfigFiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "18455-1.json"))
	mustWriteFile(t, filepath.Join(dir, "18455-1-Copy Room.bin"))
	mustWriteFile(t, filepath.Join(dir, "18455-1-Copy Room.driverdata.json"))
	mustWriteFile(t, filepath.Join(dir, "99999-1.json"))
	mustMkdir(t, filepath.Join(dir, "18455-1-a-directory-should-be-skipped"))

	got, err := matchingConfigFiles(dir, "18455-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"18455-1-Copy Room.bin", "18455-1-Copy Room.driverdata.json", "18455-1.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("matchingConfigFiles = %v, want %v", got, want)
	}
}

func TestMatchingConfigFiles_MissingDir(t *testing.T) {
	got, err := matchingConfigFiles(filepath.Join(t.TempDir(), "does-not-exist"), "18455-1")
	if err != nil {
		t.Fatalf("expected a missing Configs dir to be treated as no files, got error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no matches, got %v", got)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "present.txt")
	mustWriteFile(t, file)

	if !fileExists(file) {
		t.Error("expected fileExists to report true for an existing file")
	}
	if fileExists(filepath.Join(dir, "missing.txt")) {
		t.Error("expected fileExists to report false for a missing file")
	}
	if fileExists(dir) {
		t.Error("expected fileExists to report false for a directory")
	}
}
