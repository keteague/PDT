package main

import (
	"os"
	"path/filepath"
	"strings"
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

// isKnownInstallDir reports whether dir is one of the two directories the
// installer itself ever places PDT.exe under (installer/pdt.iss's own
// DefaultDirName={autopf}\PDT - %ProgramFiles%\PDT for an elevated install,
// %LocalAppData%\Programs\PDT for the default unelevated one).
//
// defaultDriversBasePath/defaultSaveFileBasePath use this to tell "PDT is
// genuinely installed, and something unrelated just happens to have put a
// Drivers folder next to the exe" apart from "this really is a portable/
// flash-drive copy" - confirmed as a real, live bug: without this check, an
// installed copy with a stray Drivers folder sitting next to its own exe
// (e.g. left over from early testing, or copied there by hand) silently got
// reclassified as portable and started using that exe-relative folder
// instead of installedAppDataDir(), splitting one technician's driver
// library across two locations with no visible indication of which one PDT
// was actually reading from at any given moment.
func isKnownInstallDir(dir string) bool {
	dir = filepath.Clean(dir)
	if pf := os.Getenv("ProgramFiles"); pf != "" && strings.EqualFold(dir, filepath.Clean(filepath.Join(pf, "PDT"))) {
		return true
	}
	if lad := os.Getenv("LOCALAPPDATA"); lad != "" && strings.EqualFold(dir, filepath.Clean(filepath.Join(lad, "Programs", "PDT"))) {
		return true
	}
	return false
}

// defaultDriversBasePath and defaultSaveFileBasePath both apply the same
// rule: prefer a real, already-populated Drivers folder sitting next to the
// running executable - the portable/flash-drive case, and the tell
// BuildCatalog itself already used to decide whether Configs was
// exe-relative too, before DriversBasePath/SaveFileBasePath existed as their
// own Settings fields - unless the exe is running from one of PDT's own
// known installed locations (isKnownInstallDir), in which case a Drivers
// folder happening to sit there doesn't mean this is a portable copy at
// all. Falls back to installedAppDataDir() for an installed copy - not
// because %ProgramFiles%\PDT/%LocalAppData%\Programs\PDT aren't writable
// (PDT.exe's own manifest requires elevation for every launch regardless of
// install location, so both actually are by the time PDT is running), but
// so an uninstall/reinstall of the program itself - which Inno Setup's own
// default behavior can remove {app}'s entire contents for - never risks
// touching a multi-GB, technician-curated driver library that has nothing
// to do with the program binary itself.
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
		exeDir := filepath.Dir(exe)
		if !isKnownInstallDir(exeDir) && dirExists(filepath.Join(exeDir, "Drivers")) {
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
		exeDir := filepath.Dir(exe)
		if !isKnownInstallDir(exeDir) && dirExists(filepath.Join(exeDir, "Drivers")) {
			return "Configs"
		}
	}
	if dir := installedAppDataDir(); dir != "" {
		return filepath.Join(dir, "Configs")
	}
	return "Configs"
}
