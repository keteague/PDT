package driver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFlattenRedundantWrapperDir_CollapsesSingleNestedFolder(t *testing.T) {
	dir := t.TempDir()
	destDir := filepath.Join(dir, "Foo")
	nested := filepath.Join(destDir, "Foo")
	if err := os.MkdirAll(filepath.Join(nested, "Driver"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "Driver", "real.inf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	flattenRedundantWrapperDir(destDir)

	if _, err := os.Stat(filepath.Join(destDir, "Driver", "real.inf")); err != nil {
		t.Errorf("expected the nested folder's content to be hoisted up to destDir directly: %v", err)
	}
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Error("expected the redundant nested folder itself to be gone after flattening")
	}
}

// TestFlattenRedundantWrapperDir_IgnoresStrayDSStore guards a real,
// confirmed-live bug (2026-09-13): a real Konica Minolta zip's own top
// level has both the real single wrapper folder *and* a macOS Finder-
// authored ".DS_Store" file sitting alongside it - the old strict
// len(entries) != 1 check saw 2 entries and silently refused to flatten at
// all, leaving a redundant Foo/Foo/... nesting in place.
func TestFlattenRedundantWrapperDir_IgnoresStrayDSStore(t *testing.T) {
	dir := t.TempDir()
	destDir := filepath.Join(dir, "Foo")
	nested := filepath.Join(destDir, "Foo")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "real.pkg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	flattenRedundantWrapperDir(destDir)

	if _, err := os.Stat(filepath.Join(destDir, "real.pkg")); err != nil {
		t.Errorf("expected the nested folder's content to be hoisted up to destDir directly despite the stray .DS_Store: %v", err)
	}
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Error("expected the redundant nested folder itself to be gone after flattening")
	}
}

func TestFlattenRedundantWrapperDir_LeavesDifferentlyNamedSingleFolderAlone(t *testing.T) {
	// The real regression this guards against: a package's own genuine
	// layout is very often a single top-level "Driver" folder - that must
	// NOT be hoisted up just because it's the only entry, or a normal
	// extraction like ZippedPackage/Driver/zipped.inf becomes the wrong
	// ZippedPackage/zipped.inf instead.
	dir := t.TempDir()
	destDir := filepath.Join(dir, "ZippedPackage")
	if err := os.MkdirAll(filepath.Join(destDir, "Driver"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "Driver", "zipped.inf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	flattenRedundantWrapperDir(destDir)

	if _, err := os.Stat(filepath.Join(destDir, "Driver", "zipped.inf")); err != nil {
		t.Errorf("expected a differently-named single subfolder to be left exactly as it was: %v", err)
	}
}

func TestFlattenRedundantWrapperDir_LeavesMultipleTopLevelEntriesAlone(t *testing.T) {
	dir := t.TempDir()
	destDir := filepath.Join(dir, "Foo")
	if err := os.MkdirAll(filepath.Join(destDir, "32BIT"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(destDir, "x64"), 0o755); err != nil {
		t.Fatal(err)
	}

	flattenRedundantWrapperDir(destDir)

	if _, err := os.Stat(filepath.Join(destDir, "32BIT")); err != nil {
		t.Error("expected the normal multi-folder structure to be left exactly as it was")
	}
	if _, err := os.Stat(filepath.Join(destDir, "x64")); err != nil {
		t.Error("expected the normal multi-folder structure to be left exactly as it was")
	}
}

func TestFlattenRedundantWrapperDir_LeavesSingleFileAlone(t *testing.T) {
	dir := t.TempDir()
	destDir := filepath.Join(dir, "Foo")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "Setup.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	flattenRedundantWrapperDir(destDir)

	if _, err := os.Stat(filepath.Join(destDir, "Setup.exe")); err != nil {
		t.Error("expected a single plain file (not a folder) to be left alone")
	}
}
