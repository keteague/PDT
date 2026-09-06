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
