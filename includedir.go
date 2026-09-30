package main

import "path/filepath"

// includeDir is where PDT keeps a ready copy of the OPPOSING platform's app:
// %LocalAppData%\PDT\Include (holding PDT.app) on Windows,
// ~/Library/Application Support/PDT/Include (holding PDT.exe) on macOS - a
// sibling of Drivers/Configs under the same installedAppDataDir() each
// platform's settings_*.go already defines.
//
// Kept current by CheckForUpdate itself (update_windows.go's
// refreshIncludeMacApp / update_darwin.go's refreshIncludeWinExe) every time
// this laptop checks for its own update, rather than fetched fresh at
// flash-drive time - so WritePortablePDT/SyncToFlashDrives
// (ensureReleaseAppsOnDrives, flashdrive.go) can put both platforms' apps on
// a drive with no Internet needed at that exact moment, only whenever the
// last update check happened to succeed (Ken, 2026-09-30: a client site with
// no Internet shouldn't block a Sync from carrying both apps, as long as a
// check ran earlier somewhere that did have it).
//
// Empty only if installedAppDataDir() itself can't be determined (a HOME/
// LOCALAPPDATA lookup failure) - every caller treats that the same as
// installedAppDataDir()'s own existing "" convention: Include unavailable,
// skip.
func includeDir() string {
	dir := installedAppDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "Include")
}
