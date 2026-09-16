package main

import (
	"os"
	"path/filepath"
	"strings"
)

// installedAppDataDir is ~/Library/Application Support/PDT - the macOS
// analog of Windows' %LocalAppData%\PDT (installedAppDataDir, settings_windows.go),
// where an installed (non-portable) copy of PDT keeps its own Drivers/Configs.
// Always writable by the current user regardless of where the .app bundle
// itself lives (/Applications typically needs admin rights to write into,
// same reasoning installedAppDataDir's own Windows doc comment gives for not
// using %ProgramFiles%\PDT directly).
func installedAppDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "PDT")
}

// isKnownInstallDir reports whether exeDir (the running .app bundle's own
// Contents/MacOS folder) sits directly under one of macOS's two real
// Applications folders (/Applications, or ~/Applications for a per-user
// install) - mirroring settings_windows.go's own isKnownInstallDir
// (%ProgramFiles%\PDT / %LocalAppData%\Programs\PDT) and the same real bug
// it guards against there: an installed copy with a stray Drivers folder
// sitting next to its own exe should never get silently reclassified as a
// portable/flash-drive copy just because that folder happens to exist.
// Structural (any *.app bundle placed directly under either Applications
// folder), not a hardcoded "PDT.app" literal, so this keeps working even if
// the bundle/product name ever changes.
func isKnownInstallDir(exeDir string) bool {
	bundleDir := filepath.Dir(filepath.Dir(exeDir)) // .../PDT.app/Contents/MacOS -> .../PDT.app
	if !strings.EqualFold(filepath.Ext(bundleDir), ".app") {
		return false
	}
	appsParent := filepath.Dir(bundleDir)
	if strings.EqualFold(appsParent, "/Applications") {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && strings.EqualFold(appsParent, filepath.Join(home, "Applications")) {
		return true
	}
	return false
}

// defaultDriversBasePath and defaultSaveFileBasePath both apply the same
// rule as their Windows counterparts (settings_windows.go's own doc comment
// has the full reasoning - portable-copy detection, why the portable case
// returns a relative path rather than baking in an absolute one): prefer a
// real, already-populated Drivers folder sitting next to the running
// executable, falling back to installedAppDataDir() for an installed copy.
//
// The portable case's relative-path literal is spelled "Drivers"/"Configs" -
// no leading "./" - matching settings_windows.go's own portable case exactly
// (also bare, no leading ".\") rather than echoing either OS's own separator
// convention: a bare relative name means the same thing to
// resolveExeRelative's own filepath.IsAbs check on any platform, and keeping
// both sides identical means Settings' displayed base path never shows a
// platform-specific character that could look wrong (or, worse, actually be
// wrong - confirmed live as a real, until-now-shipped bug: handing Windows'
// own ".\Drivers" spelling to filepath.Join on macOS doesn't split on the
// backslash at all, so it created a folder literally *named* ".\Drivers"
// instead of a "Drivers" subfolder - see settings.go's own doc comment) if a
// technician's own workflow ever moves the same flash drive between a
// Windows and a macOS machine.
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
