package driver

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func mkfile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// cleanupFixture lays out a Drivers folder with a current package, an
// archived one, cached data for both, an extracted folder, and things that
// aren't packages.
func cleanupFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mfg := filepath.Join(root, "Windows", "11", "Kyocera")
	mkfile(t, filepath.Join(mfg, "KXDriver_8.7.0917.zip"), "zip")
	mkfile(t, filepath.Join(mfg, "Archive", "KXDriver_8.6.1022.zip"), "old zip")
	mkfile(t, filepath.Join(mfg, "Archive", "README.txt"), "not a package")
	mkfile(t, filepath.Join(mfg, "Archive", "._KXDriver_8.6.1022.zip"), "AppleDouble")
	// .inf cache for both, and the current one's extracted folder.
	mkfile(t, filepath.Join(mfg, PdtInfCacheDirName, "KXDriver_8.7.0917", "64bit", "OEMSETUP.INF"), "inf")
	mkfile(t, filepath.Join(mfg, PdtInfCacheDirName, "Archive", "KXDriver_8.6.1022", "64bit", "OEMSETUP.INF"), "inf")
	mkfile(t, filepath.Join(mfg, "KXDriver_8.7.0917", "64bit", "OEMSETUP.INF"), "extracted")
	mkfile(t, filepath.Join(root, "Windows", "11", "Sharp", "MX-xxx1_Series_PCL6.zip"), "sharp zip")
	return root
}

func TestListCleanupPackages_IncludesArchiveSkipsDerivedData(t *testing.T) {
	root := cleanupFixture(t)
	got := ListCleanupPackages(root)
	var trees []string
	for _, p := range got {
		trees = append(trees, p.TreePath)
	}
	sort.Strings(trees)
	want := []string{
		"Kyocera/Archive/KXDriver_8.6.1022.zip",
		"Kyocera/KXDriver_8.7.0917.zip",
		"Sharp/MX-xxx1_Series_PCL6.zip",
	}
	if len(trees) != len(want) {
		t.Fatalf("got %v, want %v", trees, want)
	}
	for i := range want {
		if trees[i] != want[i] {
			t.Fatalf("got %v, want %v", trees, want)
		}
	}
	for _, p := range got {
		if p.Name == "KXDriver_8.6.1022.zip" && (!p.Archived || p.RelPath != "Windows/11/Kyocera/Archive/KXDriver_8.6.1022.zip") {
			t.Errorf("archived package = %+v", p)
		}
		if p.Name == "KXDriver_8.7.0917.zip" && (p.Archived || p.Size != 3) {
			t.Errorf("current package = %+v", p)
		}
	}
}

// A manufacturer under two Windows version folders shows which is which.
func TestListCleanupPackages_MultipleVersionFolders(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "Windows", "10", "Canon", "Generic.zip"), "a")
	mkfile(t, filepath.Join(root, "Windows", "11", "Canon", "Generic.zip"), "b")
	got := ListCleanupPackages(root)
	if len(got) != 2 || got[0].TreePath == got[1].TreePath {
		t.Fatalf("expected two distinct tree paths, got %+v", got)
	}
	for _, p := range got {
		if p.TreePath != "Canon/Windows 10/Generic.zip" && p.TreePath != "Canon/Windows 11/Generic.zip" {
			t.Errorf("unexpected tree path %q", p.TreePath)
		}
	}
}

// Deleting removes the archive and all of its cached data - and nothing
// belonging to any other package.
func TestDeleteDriverPackages_RemovesArchiveAndItsCache(t *testing.T) {
	root := cleanupFixture(t)
	mfg := filepath.Join(root, "Windows", "11", "Kyocera")

	deleted, failed := DeleteDriverPackages(root, []string{
		"Windows/11/Kyocera/KXDriver_8.7.0917.zip",
		"Windows/11/Kyocera/Archive/KXDriver_8.6.1022.zip",
	})
	if len(failed) != 0 || len(deleted) != 2 {
		t.Fatalf("deleted %v, failed %v", deleted, failed)
	}
	for _, gone := range []string{
		filepath.Join(mfg, "KXDriver_8.7.0917.zip"),
		filepath.Join(mfg, "KXDriver_8.7.0917"),
		filepath.Join(mfg, PdtInfCacheDirName, "KXDriver_8.7.0917"),
		filepath.Join(mfg, "Archive", "KXDriver_8.6.1022.zip"),
		filepath.Join(mfg, PdtInfCacheDirName, "Archive", "KXDriver_8.6.1022"),
	} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s should be gone", gone)
		}
	}
	for _, kept := range []string{
		filepath.Join(mfg, "Archive", "README.txt"),
		filepath.Join(root, "Windows", "11", "Sharp", "MX-xxx1_Series_PCL6.zip"),
	} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s should be kept: %v", kept, err)
		}
	}
}

// It can only ever delete driver archives inside a manufacturer folder.
func TestDeleteDriverPackages_RefusesAnythingElse(t *testing.T) {
	root := cleanupFixture(t)
	outside := filepath.Join(filepath.Dir(root), "outside.zip")
	mkfile(t, outside, "x")
	t.Cleanup(func() { os.Remove(outside) })

	_, failed := DeleteDriverPackages(root, []string{
		"Windows/11/Kyocera/Archive/README.txt",
		"../outside.zip",
		"Windows/11/Kyocera/" + PdtInfCacheDirName,
		"Windows/11/Kyocera/does-not-exist.zip",
	})
	if len(failed) != 4 {
		t.Errorf("expected all four refused, got %v", failed)
	}
	if _, err := os.Stat(filepath.Join(root, "Windows", "11", "Kyocera", "Archive", "README.txt")); err != nil {
		t.Error("README.txt was deleted")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("a file outside the Drivers folder was deleted")
	}
}
