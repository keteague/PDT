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

// TestIsKnownInstallDir is the direct regression test for the real bug this
// guards against (see its own doc comment): an installed copy with a stray
// Drivers folder sitting next to its own exe used to get silently
// reclassified as portable.
func TestIsKnownInstallDir(t *testing.T) {
	t.Setenv("ProgramFiles", `C:\Program Files`)
	t.Setenv("LOCALAPPDATA", `C:\Users\Test\AppData\Local`)

	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{"elevated install dir", `C:\Program Files\PDT`, true},
		{"elevated install dir, different case", `C:\PROGRAM FILES\pdt`, true},
		{"unelevated install dir", `C:\Users\Test\AppData\Local\Programs\PDT`, true},
		{"some other folder under Documents", `C:\Users\Test\Documents\PDT`, false},
		{"a flash drive's own root", `D:\PDT`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isKnownInstallDir(c.dir); got != c.want {
				t.Errorf("isKnownInstallDir(%q) = %v, want %v", c.dir, got, c.want)
			}
		})
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
