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

// TestIsKnownInstallDir is the direct regression test for the same real bug
// settings_windows.go's own isKnownInstallDir guards against there: an
// installed copy with a stray Drivers folder sitting next to its own exe
// (inside the .app bundle) should never get silently reclassified as
// portable.
func TestIsKnownInstallDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}

	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{"system Applications install", "/Applications/PDT.app/Contents/MacOS", true},
		{"different case", "/APPLICATIONS/PDT.app/Contents/MacOS", true},
		{"per-user Applications install", filepath.Join(home, "Applications", "PDT.app", "Contents", "MacOS"), true},
		{"a renamed bundle still under Applications", "/Applications/Printer Deployment Tool.app/Contents/MacOS", true},
		{"a portable copy on a mounted volume", "/Volumes/FLASHDRIVE/PDT.app/Contents/MacOS", false},
		{"a dev build in an arbitrary folder", filepath.Join(home, "Documents", "PDT.app", "Contents", "MacOS"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isKnownInstallDir(c.dir); got != c.want {
				t.Errorf("isKnownInstallDir(%q) = %v, want %v", c.dir, got, c.want)
			}
		})
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
