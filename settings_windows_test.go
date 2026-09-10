package main

import (
	"os"
	"path/filepath"
	"testing"
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

// TestDefaultDriversBasePath_PortableLiteralHasNoBackslash: the portable
// case's relative-path literal is bare "Drivers"/"Configs" now, matching
// settings_darwin.go's own portable case exactly (see both files' doc
// comments for why - keeping the two identical means Settings' displayed
// base path never shows a platform-specific separator character at all).
func TestDefaultDriversBasePath_PortableLiteralHasNoBackslash(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	driversDir := filepath.Join(filepath.Dir(exe), "Drivers")
	if err := os.MkdirAll(driversDir, 0o755); err != nil {
		t.Fatalf("creating %q: %v", driversDir, err)
	}
	defer os.RemoveAll(driversDir)

	if got := defaultDriversBasePath(); got != "Drivers" {
		t.Errorf("defaultDriversBasePath() = %q, want %q (no backslash)", got, "Drivers")
	}
	if got := defaultSaveFileBasePath(); got != "Configs" {
		t.Errorf("defaultSaveFileBasePath() = %q, want %q (no backslash)", got, "Configs")
	}
}
