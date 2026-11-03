package main

import (
	"os"
	"path/filepath"
	"strings"
)

// portableSettingsDirName is the folder, next to a portable copy's own
// PDT.exe (the flash drive's root, beside Drivers and Configs), that holds
// that copy's settings.json and state files.
const portableSettingsDirName = "Settings"

// stateFileNames are every per-user file appStateDir holds.
var stateFileNames = []string{"settings.json", "cloudsync-state.json", "update-check-state.json"}

// portableExeDir returns the running exe's folder when this is a portable
// copy - not in one of PDT's installed locations, and with a Drivers folder
// beside it (the same test defaultDriversBasePath uses) - or "" otherwise.
// A var so tests can stand in for os.Executable's real location.
var portableExeDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if isKnownInstallDir(dir) || !dirExists(filepath.Join(dir, "Drivers")) {
		return ""
	}
	return dir
}

// appStateDir is where settings.json and the Cloud Sync / update-check
// state files live (Ken, 2026-11-03):
//   - a portable copy keeps its own, on the flash drive
//     (<exe folder>\Settings), including when it's run on the tech's own
//     laptop - it never reads or writes the PC's per-user files;
//   - an installed copy keeps them with its data, in installedAppDataDir()
//     (%LocalAppData%\PDT on Windows, ~/Library/Application Support/PDT on
//     macOS), beside Drivers and Configs.
//
// Before this, every copy shared os.UserConfigDir()/PDT (%AppData%\PDT on
// Windows). That's how a portable copy's relative "Drivers"/"Configs" got
// into the installed copy's settings (see runningFromInstallDir), and the
// reverse - a portable copy on the tech's laptop reading the installed
// copy's absolute paths and using the laptop's library instead of the
// drive's.
//
// Anything else that isn't portable (a dev build with no Drivers beside it)
// shares the installed copy's folder, as it always shared %AppData%\PDT.
func appStateDir() (string, error) {
	if dir := portableExeDir(); dir != "" {
		return filepath.Join(dir, portableSettingsDirName), nil
	}
	if dir := installedAppDataDir(); dir != "" {
		return dir, nil
	}
	return legacyStateDir()
}

// legacyStateDir is where every copy kept its state before appStateDir.
func legacyStateDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "PDT"), nil
}

// appStateFilePath is name inside appStateDir.
func appStateFilePath(name string) (string, error) {
	dir, err := appStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// migrateLegacyStateFiles copies each state file from legacyStateDir into
// appStateDir if it isn't there yet - an installed copy's first launch
// after appStateDir existed. Copied, not moved: an older PDT version still
// installed elsewhere, or a not-yet-updated copy, keeps reading the old
// location and must not lose its settings. Never for a portable copy -
// picking up the PC's files is exactly what portable copies must not do.
// Best-effort and idempotent; safe to call on every load. Reports whether
// settings.json existed in either place, i.e. whether this is an existing
// installation (see seedGuestCloudSync).
func migrateLegacyStateFiles() (hadSettings bool) {
	newDir, err := appStateDir()
	if err != nil {
		return false
	}
	if portableExeDir() != "" {
		return fileExists(filepath.Join(newDir, "settings.json"))
	}
	legacyDir, err := legacyStateDir()
	if err != nil || strings.EqualFold(filepath.Clean(newDir), filepath.Clean(legacyDir)) {
		return fileExists(filepath.Join(newDir, "settings.json"))
	}
	for _, name := range stateFileNames {
		dst := filepath.Join(newDir, name)
		if fileExists(dst) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(legacyDir, name))
		if err != nil {
			continue
		}
		if os.MkdirAll(newDir, 0o755) == nil {
			_ = os.WriteFile(dst, data, 0o644)
		}
	}
	return fileExists(filepath.Join(newDir, "settings.json")) || fileExists(filepath.Join(legacyDir, "settings.json"))
}
