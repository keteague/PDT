package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestSettings isolates the per-user folders (isolateUserConfigDir)
// and writes settings.json with a portable copy's relative base paths.
func writeTestSettings(t *testing.T) {
	t.Helper()
	dir := isolateUserConfigDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "PDT"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"driversBasePath": "Drivers", "saveFileBasePath": "Configs"}`
	if err := os.WriteFile(filepath.Join(dir, "PDT", "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withRunningFromInstallDir(t *testing.T, v bool) {
	t.Helper()
	prev := runningFromInstallDir
	runningFromInstallDir = func() bool { return v }
	t.Cleanup(func() { runningFromInstallDir = prev; basePathCorrections = nil })
	basePathCorrections = nil
}

// Regression (2026-11-03): a portable copy's relative "Drivers"/"Configs",
// saved into the shared settings.json, made the installed copy use
// %LocalAppData%\Programs\PDT\Drivers. An installed copy must ignore them.
func TestLoadSettings_InstalledCopyIgnoresRelativeBasePaths(t *testing.T) {
	writeTestSettings(t)
	withRunningFromInstallDir(t, true)

	s := loadSettings()

	if !filepath.IsAbs(s.DriversBasePath) || !filepath.IsAbs(s.SaveFileBasePath) {
		t.Errorf("expected absolute default paths, got Drivers=%q Configs=%q", s.DriversBasePath, s.SaveFileBasePath)
	}
	if len(basePathCorrections) != 2 {
		t.Errorf("expected 2 corrections, got %v", basePathCorrections)
	}
}

// A portable copy keeps its relative paths - that's what makes it survive
// drive-letter changes.
func TestLoadSettings_PortableCopyKeepsRelativeBasePaths(t *testing.T) {
	writeTestSettings(t)
	withRunningFromInstallDir(t, false)

	s := loadSettings()

	if s.DriversBasePath != "Drivers" || s.SaveFileBasePath != "Configs" {
		t.Errorf("expected relative paths kept, got Drivers=%q Configs=%q", s.DriversBasePath, s.SaveFileBasePath)
	}
	if len(basePathCorrections) != 0 {
		t.Errorf("expected no corrections, got %v", basePathCorrections)
	}
}
