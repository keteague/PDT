package main

import (
	"os"
	"path/filepath"
)

// installedAppDataDir is %LocalAppData%\PDT - where an installed (non-
// portable) copy of PDT keeps its own Drivers/Configs. Neither
// installer-chosen exe location (%ProgramFiles%\PDT for an elevated
// install, %LocalAppData%\Programs\PDT for an unelevated one) is guaranteed
// writable by whoever actually ends up running PDT day to day, but this is,
// regardless of which one PDT itself is installed under.
func installedAppDataDir() string {
	if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
		return filepath.Join(lad, "PDT")
	}
	return ""
}

// defaultDriversBasePath and defaultSaveFileBasePath both apply the same
// rule: prefer a real, already-populated Drivers folder sitting next to the
// running executable - the portable/flash-drive case, and the tell
// BuildCatalog itself already used to decide whether Configs was
// exe-relative too, before DriversBasePath/SaveFileBasePath existed as their
// own Settings fields - falling back to installedAppDataDir() for an
// installed copy, where the current user always has write access regardless
// of whether PDT itself sits under %ProgramFiles% or %LocalAppData%\Programs.
//
// The portable case returns a *relative* path ("Drivers", "Configs" - no
// leading ".\" - see settings.go's own doc comment for why: a bare relative
// name means the same thing to resolveExeRelative's own filepath.IsAbs check
// either way, and it's also exactly what settings_darwin.go's own portable
// case returns, so Settings' displayed base path never shows a platform-
// specific separator character on either OS - nothing to look wrong or get
// stuck in the wrong notation if the same technician's own workflow moves
// between a Windows and a macOS machine), not an absolute one built from
// this exe's current location - resolved back to absolute at the point of
// use (see resolveExeRelative) against wherever the exe actually is *at that
// moment*, not wherever it was when Settings was last saved. A flash drive
// doesn't keep the same drive letter across computers (or even across
// relaunches on the same one, if something else already claimed it), so
// baking in an absolute path here would save a value that silently stops
// meaning "this flash drive's own Drivers folder" the moment the letter
// changes - confirmed as a real gap: Settings' DriversBasePath/
// SaveFileBasePath fields showed a stale, computer-specific absolute path
// even for a portable copy before this.
func defaultDriversBasePath() string {
	if exe, err := os.Executable(); err == nil {
		if dirExists(filepath.Join(filepath.Dir(exe), "Drivers")) {
			return "Drivers"
		}
	}
	if dir := installedAppDataDir(); dir != "" {
		return filepath.Join(dir, "Drivers")
	}
	return "Drivers"
}

func defaultSaveFileBasePath() string {
	if exe, err := os.Executable(); err == nil {
		if dirExists(filepath.Join(filepath.Dir(exe), "Drivers")) {
			return "Configs"
		}
	}
	if dir := installedAppDataDir(); dir != "" {
		return filepath.Join(dir, "Configs")
	}
	return "Configs"
}
