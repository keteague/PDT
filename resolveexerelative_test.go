package main

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

// absPathForOS returns a real absolute path in this OS's own syntax -
// filepath.IsAbs (which resolveExeRelative's own "already absolute, leave it
// alone" check relies on) requires a drive letter on Windows
// (`C:\Users\...`) and a leading slash on everything else; a literal
// hardcoded for one OS isn't actually absolute on the other, which is
// exactly what this test needs to not assume.
func absPathForOS() string {
	if goruntime.GOOS == "windows" {
		return `C:\Users\Ken\AppData\Local\PDT\Drivers`
	}
	return "/Users/Ken/Library/Application Support/PDT/Drivers"
}

func TestResolveExeRelative_AbsolutePathUnchanged(t *testing.T) {
	abs := absPathForOS()
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
	// A plain relative path, not the Windows-flavored ".\Drivers" spelling -
	// filepath.IsAbs/filepath.Join both treat "Drivers" the same way on every
	// OS, so there's no platform-specific syntax needed here at all.
	if got := resolveExeRelative("Drivers"); got != want {
		t.Errorf(`resolveExeRelative("Drivers") = %q, want %q`, got, want)
	}
}
