package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"PDT/internal/driver"
)

func TestInstalledAppDataDir(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\Test\AppData\Local`)
	want := filepath.Join(`C:\Users\Test\AppData\Local`, "PDT")
	if got := installedAppDataDir(); got != want {
		t.Errorf("installedAppDataDir() = %q, want %q", got, want)
	}

	t.Setenv("LOCALAPPDATA", "")
	if got := installedAppDataDir(); got != "" {
		t.Errorf("installedAppDataDir() with no LOCALAPPDATA = %q, want empty", got)
	}
}

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

		archiveReadme := filepath.Join(folder, "Archive", "README.md")
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

func TestEnsureDriversScaffold_LeavesNonEmptyRootAlone(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "SomethingAlreadyHere.txt")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureDriversScaffold(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Windows")); !os.IsNotExist(err) {
		t.Error("expected no scaffolding to happen when root already has something in it")
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
