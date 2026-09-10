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
