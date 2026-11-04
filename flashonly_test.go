package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// flashOnlyFixture: a laptop Drivers folder and a "flash drive" whose
// Drivers has the laptop's packages plus two the laptop doesn't - one moved
// to Archive on the laptop, one removed altogether - and a cached .inf for
// the removed one.
func flashOnlyFixture(t *testing.T) (drive string) {
	t.Helper()
	laptop := filepath.Join(t.TempDir(), "Drivers")
	prev := currentDriversBasePath
	currentDriversBasePath = laptop
	t.Cleanup(func() { currentDriversBasePath = prev })
	mkTestFile(t, filepath.Join(laptop, "Windows", "11", "Kyocera", "KXDriver_8.7.0917.zip"), "new")
	mkTestFile(t, filepath.Join(laptop, "Windows", "11", "Kyocera", "Archive", "KXDriver_8.6.1022.zip"), "old")

	drive = t.TempDir()
	dd := filepath.Join(drive, "Drivers", "Windows", "11")
	mkTestFile(t, filepath.Join(dd, "Kyocera", "KXDriver_8.7.0917.zip"), "new")
	mkTestFile(t, filepath.Join(dd, "Kyocera", "kxdriver_8.6.1022.ZIP"), "old, at its pre-Archive path")
	mkTestFile(t, filepath.Join(dd, "Sharp", "Old_Sharp.zip"), "gone from the laptop")
	mkTestFile(t, filepath.Join(dd, "Sharp", ".pdt-infcache", "Old_Sharp", "x.inf"), "cached")
	return drive
}

func TestFlashOnlyPackages(t *testing.T) {
	drive := flashOnlyFixture(t)
	got := flashOnlyPackages(drive, laptopPackageSet())
	var paths []string
	for _, p := range got {
		paths = append(paths, p.RelPath)
	}
	if len(paths) != 2 {
		t.Fatalf("got %v, want the old Kyocera path and the Sharp package", paths)
	}
	for _, p := range paths {
		if !strings.HasSuffix(strings.ToLower(p), "kyocera/kxdriver_8.6.1022.zip") && p != "Windows/11/Sharp/Old_Sharp.zip" {
			t.Errorf("unexpected drive-only package %q", p)
		}
	}
}

// Same path, different case, is the same file - not a drive-only package.
func TestFlashOnlyPackages_IgnoresCase(t *testing.T) {
	drive := flashOnlyFixture(t)
	mkTestFile(t, filepath.Join(drive, "Drivers", "Windows", "11", "Kyocera", "Archive", "KXDRIVER_8.6.1022.ZIP"), "old")
	for _, p := range flashOnlyPackages(drive, laptopPackageSet()) {
		if strings.Contains(p.RelPath, "Archive") {
			t.Errorf("%s matches the laptop's Archive copy ignoring case", p.RelPath)
		}
	}
}

// Deleting removes the chosen package and its cache from the drive, and
// keeps one that has since appeared on the laptop.
func TestDeleteFlashOnlyDrivers(t *testing.T) {
	drive := flashOnlyFixture(t)
	sharp := filepath.Join(drive, "Drivers", "Windows", "11", "Sharp")
	result := &BatchDriveResult{Failed: map[string]string{}}

	// Meanwhile the laptop gained this one.
	mkTestFile(t, filepath.Join(driversRoot(), "Windows", "11", "Kyocera", "kxdriver_8.6.1022.ZIP"), "restored")
	deleteFlashOnlyDrivers(drive, []string{
		"Windows/11/Sharp/Old_Sharp.zip",
		"Windows/11/Kyocera/kxdriver_8.6.1022.ZIP",
	}, result)

	if fileExists(filepath.Join(sharp, "Old_Sharp.zip")) || dirExists(filepath.Join(sharp, ".pdt-infcache", "Old_Sharp")) {
		t.Error("the Sharp package or its cache is still on the drive")
	}
	if !fileExists(filepath.Join(drive, "Drivers", "Windows", "11", "Kyocera", "kxdriver_8.6.1022.ZIP")) {
		t.Error("a package now on the laptop was deleted from the drive")
	}
	var kept, deleted bool
	for _, n := range result.Notes {
		kept = kept || strings.Contains(n.Text, "Kept")
		deleted = deleted || strings.Contains(n.Text, "Deleted Windows/11/Sharp/Old_Sharp.zip")
	}
	if !kept || !deleted {
		t.Errorf("notes = %+v", result.Notes)
	}
}
