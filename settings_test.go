package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"PDT/internal/driver"
)

// TestInstalledAppDataDir: see settings_windows_test.go/settings_darwin_test.go -
// installedAppDataDir itself is platform-specific now (settings_windows.go/
// settings_darwin.go), so there's no one shared behavior left to test here.

func TestEnsureDriversScaffold_CreatesManufacturerFolders(t *testing.T) {
	dir := t.TempDir()
	if err := ensureDriversScaffold(dir); err != nil {
		t.Fatal(err)
	}
	for _, mfg := range driver.Manufacturers {
		folder := filepath.Join(dir, "Windows", "11", strings.ReplaceAll(mfg, " ", ""))
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			t.Errorf("expected scaffolded folder %q to exist", folder)
		}

		archiveReadme := filepath.Join(folder, "Archive", "README.txt")
		data, err := os.ReadFile(archiveReadme)
		if err != nil {
			t.Errorf("expected %q to exist: %v", archiveReadme, err)
			continue
		}
		if string(data) != archiveReadmeContent {
			t.Errorf("%q content = %q, want %q", archiveReadme, data, archiveReadmeContent)
		}
	}
}

func TestEnsureDriversScaffold_LeavesFlatLegacyLayoutAlone(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "SomethingAlreadyHere.txt")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureDriversScaffold(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Windows")); !os.IsNotExist(err) {
		t.Error("expected no scaffolding to happen against a non-empty root with no Windows subfolder (the old flat layout)")
	}
}

func TestEnsureDriversScaffold_RetrofitsExistingManufacturerFolder(t *testing.T) {
	dir := t.TempDir()
	canonDir := filepath.Join(dir, "Windows", "11", "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonDir, "SomeRealDriver.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulate a manufacturer folder created by an older PDT version that
	// used README.md instead of README.txt.
	staleArchive := filepath.Join(canonDir, "Archive")
	if err := os.MkdirAll(staleArchive, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleArchive, "README.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureDriversScaffold(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(canonDir, "SomeRealDriver.zip")); err != nil {
		t.Error("expected the pre-existing real driver file to be left alone")
	}
	if _, err := os.Stat(filepath.Join(staleArchive, "README.md")); !os.IsNotExist(err) {
		t.Error("expected the stale README.md to be removed")
	}
	data, err := os.ReadFile(filepath.Join(staleArchive, "README.txt"))
	if err != nil {
		t.Fatalf("expected README.txt to be created: %v", err)
	}
	if string(data) != archiveReadmeContent {
		t.Errorf("README.txt content = %q, want %q", data, archiveReadmeContent)
	}

	// A manufacturer with no folder at all yet should also get scaffolded,
	// since Windows/11 already being present marks this as the nested (not
	// flat-legacy) layout.
	if _, err := os.Stat(filepath.Join(dir, "Windows", "11", "HP", "Archive", "README.txt")); err != nil {
		t.Errorf("expected HP to be scaffolded too: %v", err)
	}
}

func TestReconcileManufacturerOrder_Nil(t *testing.T) {
	got := reconcileManufacturerOrder(nil)
	if !reflect.DeepEqual(got, driver.Manufacturers) {
		t.Errorf("reconcileManufacturerOrder(nil) = %v, want %v", got, driver.Manufacturers)
	}
}

func TestReconcileManufacturerOrder_PreservesCustomOrder(t *testing.T) {
	saved := []string{"Sharp", "Canon"}
	got := reconcileManufacturerOrder(saved)

	if got[0] != "Sharp" || got[1] != "Canon" {
		t.Fatalf("expected Sharp then Canon first, got %v", got)
	}
	if len(got) != len(driver.Manufacturers) {
		t.Fatalf("expected every manufacturer present exactly once, got %d of %d: %v", len(got), len(driver.Manufacturers), got)
	}
	seen := map[string]int{}
	for _, m := range got {
		seen[m]++
	}
	for _, m := range driver.Manufacturers {
		if seen[m] != 1 {
			t.Errorf("%q appears %d times in reconciled order, want exactly 1", m, seen[m])
		}
	}
}

func TestReconcileManufacturerOrder_DropsUnknownAndDuplicates(t *testing.T) {
	saved := []string{"Canon", "Canon", "NotARealManufacturer", "HP"}
	got := reconcileManufacturerOrder(saved)

	if got[0] != "Canon" || got[1] != "HP" {
		t.Fatalf("expected Canon then HP first (duplicate/unknown entries dropped), got %v", got)
	}
	for _, m := range got {
		if m == "NotARealManufacturer" {
			t.Error("unknown manufacturer name should have been dropped")
		}
	}
}
