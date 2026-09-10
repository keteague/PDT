package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledAppDataDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", "PDT")
	if got := installedAppDataDir(); got != want {
		t.Errorf("installedAppDataDir() = %q, want %q", got, want)
	}
}

// TestDefaultDriversBasePath_PortableLiteralHasNoBackslash guards against
// the real bug this split fixed: a Windows-style ".\Drivers" literal handed
// to filepath.Join/resolveExeRelative on macOS doesn't split on the
// backslash at all, so it created a folder literally *named* ".\Drivers"
// next to the running executable instead of a "Drivers" subfolder -
// confirmed live, and severely enough to break `wails build`'s own codesign
// step once that malformed folder existed inside the .app bundle.
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

	if got := resolveExeRelative(defaultDriversBasePath()); got != driversDir {
		t.Errorf("resolveExeRelative(defaultDriversBasePath()) = %q, want %q", got, driversDir)
	}
}
