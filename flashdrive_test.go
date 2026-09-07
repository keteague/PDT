package main

import (
	"os"
	"path/filepath"
	"testing"

	"PDT/internal/driver"
)

// TestWritePortablePDTTo_AlwaysCreatesConfigsAndFullDriversScaffold guards
// against two real gaps found live: a technician's local Configs folder
// often doesn't exist yet (nothing saved/captured there so far), which used
// to leave the flash drive with no Configs folder at all; and their local
// Drivers folder is very rarely fully populated for every manufacturer PDT
// knows about, which used to leave the flash drive missing folders for
// whichever manufacturers weren't already downloaded locally.
func TestWritePortablePDTTo_AlwaysCreatesConfigsAndFullDriversScaffold(t *testing.T) {
	oldDrivers, oldConfigs := currentDriversBasePath, currentConfigsBasePath
	defer func() {
		currentDriversBasePath, currentConfigsBasePath = oldDrivers, oldConfigs
	}()

	sourceDrivers := t.TempDir()
	// Only Canon has anything real locally - every other manufacturer is
	// deliberately absent, the common case on a technician's own laptop.
	canonDir := filepath.Join(sourceDrivers, "Windows", "11", "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonDir, "real-driver.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentDriversBasePath = sourceDrivers

	// No Configs folder at all yet - the common "nothing saved yet" case.
	currentConfigsBasePath = filepath.Join(t.TempDir(), "Configs-does-not-exist")

	dest := t.TempDir()
	if err := writePortablePDTTo(dest, "PDT.exe", []byte("fake exe bytes")); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dest, "PDT.exe")); err != nil {
		t.Errorf("expected PDT.exe to be written: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dest, "Configs")); err != nil || !info.IsDir() {
		t.Errorf("expected a Configs folder to exist even though the source had none: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Drivers", "Windows", "11", "Canon", "real-driver.zip")); err != nil {
		t.Errorf("expected Canon's real local content to be copied: %v", err)
	}
	for _, mfg := range driver.Manufacturers {
		if mfg == "Canon" {
			continue
		}
		folder := filepath.Join(dest, "Drivers", "Windows", "11", mfg)
		if mfg == "Konica Minolta" {
			folder = filepath.Join(dest, "Drivers", "Windows", "11", "KonicaMinolta")
		}
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			t.Errorf("expected %s to be scaffolded on the flash drive even though it had nothing locally: %v", mfg, err)
		}
	}
}
