package main

import (
	"path/filepath"
	"testing"
)

func TestIncludeDir(t *testing.T) {
	base := installedAppDataDir()
	if base == "" {
		t.Skip("installedAppDataDir() unavailable in this environment")
	}
	if got, want := includeDir(), filepath.Join(base, "Include"); got != want {
		t.Errorf("includeDir() = %q, want %q", got, want)
	}
}
