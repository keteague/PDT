package main

import (
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
