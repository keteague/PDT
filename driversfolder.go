package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"PDT/internal/driver"
)

// archiveReadmeContent is the boilerplate dropped into every manufacturer's
// Archive\README.txt - not user data, so ensureDriversScaffold always
// (re)writes it verbatim rather than treating it as something to preserve.
const archiveReadmeContent = `Move retired or superseded driver packages in here (whole package folders, not individual files).

The program never scans this folder, so anything moved here disappears from the Driver dropdown and
the version-upgrade check - keep it around for as long as you want, safely out of the way, without
needing to delete it.
`

// ensureDriversScaffold makes sure every entry in driver.Manufacturers has a
// Drivers\Windows\11\<Manufacturer>\Archive\README.txt (spaces stripped from
// the folder name to match this project's own on-disk convention - "Konica
// Minolta" -> "KonicaMinolta"), creating the manufacturer folder itself too
// if it doesn't exist yet. Unconditional and idempotent - safe to call on
// every startup - which is what retroactively adds Archive\README.txt to a
// manufacturer folder that predates this scaffold existing at all (the
// original motivating case: real driver folders like Canon or HP that were
// already populated by hand long before PDT started creating this itself).
// Windows 11 only, not the full Windows/macOS version matrix the README's
// "Drivers folder layout" documents for this project's own driver archive -
// PDT itself only ever scans the Windows side today, and there's no way to
// know ahead of time which Windows versions a given install's fleet needs.
//
// The one case this leaves alone entirely: an older flat-layout Drivers
// folder (Drivers\<Manufacturer>\... directly, no Windows\<version> nesting
// - see README's "Drivers folder layout" back-compat note). Bolting a
// Windows\11 tree onto one of those would plant a "Windows" folder that
// itself flips BuildCatalog's own flat-vs-nested detection, breaking the
// exact back-compat this is supposed to preserve - so a non-empty root with
// no "Windows" subfolder is left untouched.
func ensureDriversScaffold(root string) error {
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		if _, err := os.Stat(filepath.Join(root, "Windows")); os.IsNotExist(err) {
			return nil
		}
	}
	win11 := filepath.Join(root, "Windows", "11")
	for _, mfg := range driver.Manufacturers {
		folder := strings.ReplaceAll(mfg, " ", "")
		archiveDir := filepath.Join(win11, folder, "Archive")
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", folder, err)
		}
		// Best-effort: drop a stale README.md from an earlier PDT version
		// that used that filename, so a manufacturer folder doesn't end up
		// with both README.md and README.txt side by side.
		_ = os.Remove(filepath.Join(archiveDir, "README.md"))
		if err := os.WriteFile(filepath.Join(archiveDir, "README.txt"), []byte(archiveReadmeContent), 0o644); err != nil {
			return fmt.Errorf("writing %s\\Archive\\README.txt: %w", folder, err)
		}
	}
	return nil
}

// ensureMacDriversScaffold is the darwin analog of ensureDriversScaffold
// above, called from app_darwin.go's platformStartup - but a genuinely
// different shape, not a straight port, because macOS driver packages
// really do vary by OS release the way Windows' generally don't (see
// README's "Drivers folder layout": Drivers/macOS/<Manufacturer>/
// <macOS version>/... nests version *under* manufacturer, the opposite of
// Windows' Drivers/Windows/<version>/<Manufacturer>/...). Two things, both
// unconditional/idempotent like the Windows version:
//
//  1. Makes sure every entry in driver.Manufacturers has at least a bare
//     Drivers/macOS/<Manufacturer> folder - same "every manufacturer is
//     offered regardless of whether it's populated locally" philosophy as
//     the Windows side, so a brand-new macOS install can still pick any
//     manufacturer before downloading anything.
//  2. Retroactively drops an Archive/README.txt into every macOS-version
//     subfolder it finds already there under each manufacturer - confirmed
//     necessary against a real Drivers folder (Ken's own): only 2 of
//     Canon's 10 real version folders (10.15-Catalina, 26-Tahoe) had an
//     Archive folder at all, the rest didn't, because nothing had been
//     creating this automatically until now.
//
// Deliberately does NOT create any version subfolder itself, unlike
// ensureDriversScaffold's own hardcoded Windows/11 - there is no one macOS
// version PDT could hardcode here that wouldn't need updating by hand the
// moment Apple ships the next one (confirmed live: going from macOS 26
// "Tahoe" to 27 "Golden Gate" needed exactly that, by hand, the same day
// this function was written). Scaffolding only ever adds Archive to a
// version folder that's already there, never invents one.
func ensureMacDriversScaffold(macDriversRoot string) error {
	for _, mfg := range driver.Manufacturers {
		folder := strings.ReplaceAll(mfg, " ", "")
		mfgDir := filepath.Join(macDriversRoot, folder)
		if err := os.MkdirAll(mfgDir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", folder, err)
		}
		entries, err := os.ReadDir(mfgDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == "Archive" {
				continue
			}
			archiveDir := filepath.Join(mfgDir, entry.Name(), "Archive")
			if err := os.MkdirAll(archiveDir, 0o755); err != nil {
				return fmt.Errorf("creating %s/%s/Archive: %w", folder, entry.Name(), err)
			}
			if err := os.WriteFile(filepath.Join(archiveDir, "README.txt"), []byte(archiveReadmeContent), 0o644); err != nil {
				return fmt.Errorf("writing %s/%s/Archive/README.txt: %w", folder, entry.Name(), err)
			}
		}
	}
	return nil
}

// OpenFolderResult is OpenDriversBasePathInExplorer's outcome - a DTO with
// its own Error field rather than a bare Go error, per this file's siblings
// (see app.go's own doc comment: a bare error value doesn't JSON-marshal its
// message to the frontend at all). OpenDriversBasePathInExplorer itself is
// platform-specific - see openfolder_windows.go/openfolder_darwin.go.
type OpenFolderResult struct {
	Error string `json:"error"`
}
