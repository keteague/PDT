package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExeRelative_AbsolutePathUnchanged(t *testing.T) {
	abs := `C:\Users\Ken\AppData\Local\PDT\Drivers`
	if got := resolveExeRelative(abs); got != abs {
		t.Errorf("resolveExeRelative(%q) = %q, want unchanged", abs, got)
	}
}

func TestResolveExeRelative_EmptyPathUnchanged(t *testing.T) {
	if got := resolveExeRelative(""); got != "" {
		t.Errorf("resolveExeRelative(\"\") = %q, want \"\"", got)
	}
}

func TestResolveExeRelative_RelativePathResolvedAgainstExeDir(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(exe), "Drivers")
	if got := resolveExeRelative(`.\Drivers`); got != want {
		t.Errorf(`resolveExeRelative(".\\Drivers") = %q, want %q`, got, want)
	}
}
