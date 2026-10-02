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

// TestResolveAgainstExe_DegenerateExePathUnchanged is resolveAgainstExe's
// own regression test for the real bug (2026-10-02, see its own doc
// comment): a degenerate os.Executable() result - bare "E:" rather than
// "E:\PDT.exe" - must never silently produce a joined path missing its
// separator ("E:Drivers"). "E:" isn't an absolute path on Windows (no
// rooted path after the volume) or on Unix (no leading "/") - the same
// value exercises resolveAgainstExe's guard on every OS without needing
// goruntime.GOOS branching, unlike absPathForOS's own real absolute paths.
func TestResolveAgainstExe_DegenerateExePathUnchanged(t *testing.T) {
	for _, exe := range []string{"E:", "E:."} {
		if got := resolveAgainstExe(exe, "Drivers"); got != "Drivers" {
			t.Errorf(`resolveAgainstExe(%q, "Drivers") = %q, want unchanged "Drivers"`, exe, got)
		}
	}
}

// TestResolveAgainstExe_SaneExePathJoined confirms the fix above didn't
// just make resolveAgainstExe always return path unchanged - a genuine,
// fully-qualified exe path still resolves exactly as before. Windows-only:
// IsAbs(`E:\PDT.exe`) is false on Unix (no leading "/"), so this exact
// input isn't a meaningful "sane path" case there.
func TestResolveAgainstExe_SaneExePathJoined(t *testing.T) {
	if goruntime.GOOS != "windows" {
		t.Skip("drive-letter path syntax is Windows-specific")
	}
	got := resolveAgainstExe(`E:\PDT.exe`, "Drivers")
	want := `E:\Drivers`
	if got != want {
		t.Errorf(`resolveAgainstExe(E:\PDT.exe, "Drivers") = %q, want %q`, got, want)
	}
}
