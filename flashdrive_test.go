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
	if err := writePortablePDTTo(dest, "PDT.exe", []byte("fake exe bytes"), nil); err != nil {
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

// TestWritePortablePDTTo_RepeatWriteDoesNotDropRealDrivers guards against the
// exact real bug that motivated switching from os.CopyFS to copyTreeMerge:
// os.CopyFS documents that it "will not overwrite existing files" and "stops
// at and returns the first error encountered" - so writing to a flash drive
// that already had anything under Drivers\ (a prior Write to Flash Drive, or
// even just its own scaffolded Archive\README.txt files) made the copy fail
// immediately, silently copying none of the real driver files after
// whichever path it choked on first. Confirmed live.
func TestWritePortablePDTTo_RepeatWriteDoesNotDropRealDrivers(t *testing.T) {
	oldDrivers, oldConfigs := currentDriversBasePath, currentConfigsBasePath
	defer func() {
		currentDriversBasePath, currentConfigsBasePath = oldDrivers, oldConfigs
	}()

	sourceDrivers := t.TempDir()
	canonDir := filepath.Join(sourceDrivers, "Windows", "11", "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonDir, "real-driver.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentDriversBasePath = sourceDrivers
	currentConfigsBasePath = filepath.Join(t.TempDir(), "Configs-does-not-exist")

	dest := t.TempDir()
	if err := writePortablePDTTo(dest, "PDT.exe", []byte("fake exe bytes"), nil); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	// Second write to the SAME already-populated destination - this is what
	// os.CopyFS choked on.
	if err := writePortablePDTTo(dest, "PDT.exe", []byte("fake exe bytes, updated"), nil); err != nil {
		t.Fatalf("second write to an already-populated destination failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "Drivers", "Windows", "11", "Canon", "real-driver.zip")); err != nil {
		t.Errorf("expected Canon's real content to still be present after a second write: %v", err)
	}
}

// TestWritePortablePDTTo_CopiesSevenZipTools guards the "USB drive should
// include tools (7-zip) in case it's needed" request - a portable copy
// should be fully self-contained, not rely on the destination computer
// already having 7z.exe cached from some other PDT install.
func TestWritePortablePDTTo_CopiesSevenZipTools(t *testing.T) {
	oldDrivers, oldConfigs := currentDriversBasePath, currentConfigsBasePath
	defer func() {
		currentDriversBasePath, currentConfigsBasePath = oldDrivers, oldConfigs
	}()
	currentDriversBasePath = t.TempDir()
	currentConfigsBasePath = filepath.Join(t.TempDir(), "Configs-does-not-exist")

	// sevenZipToolsDir() itself isn't overridable (it always resolves to the
	// real per-machine cache dir), so this only meaningfully exercises the
	// copy step if that real directory happens to exist on the machine
	// running the test - matching how ensureSevenZipExtracted would have
	// already populated it during a real App.startup(). Skips cleanly
	// otherwise rather than asserting something environment-dependent.
	toolsDir := sevenZipToolsDir()
	if !dirExists(toolsDir) {
		t.Skip("7-Zip tools cache not present on this machine - nothing to verify")
	}

	dest := t.TempDir()
	if err := writePortablePDTTo(dest, "PDT.exe", []byte("fake exe bytes"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "tools", "7zip", "7z.exe")); err != nil {
		t.Errorf("expected 7z.exe to be copied onto the flash drive: %v", err)
	}
}

// TestSyncDriversTo_SkipsWhenTargetIsTheSameAsSource guards against a real
// hazard: Sync stays available even when PDT itself is running from a flash
// drive (unlike Write to Flash Drive), so a technician could pick that exact
// same drive as the sync target. copyTreeMerge would then be asked to copy a
// directory tree onto itself - truncating a source file while still reading
// it (its own os.OpenFile uses O_TRUNC). This must be a silent no-op
// instead.
func TestSyncDriversTo_SkipsWhenTargetIsTheSameAsSource(t *testing.T) {
	oldDrivers := currentDriversBasePath
	defer func() { currentDriversBasePath = oldDrivers }()

	root := t.TempDir()
	driversDir := filepath.Join(root, "Drivers")
	if err := os.MkdirAll(filepath.Join(driversDir, "Windows", "11", "Canon"), 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(driversDir, "Windows", "11", "Canon", "real-driver.zip")
	if err := os.WriteFile(realFile, []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentDriversBasePath = driversDir

	// root's own Drivers subfolder IS driversRoot() - syncing "to root" would
	// make the destination and source identical.
	if err := syncDriversTo(root, nil); err != nil {
		t.Fatalf("expected a same-path sync to be a silent no-op, got error: %v", err)
	}

	data, err := os.ReadFile(realFile)
	if err != nil || string(data) != "original content" {
		t.Errorf("expected the source file to be completely untouched: data=%q err=%v", data, err)
	}
}
