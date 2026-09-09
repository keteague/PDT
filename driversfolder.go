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

// OpenFolderResult is OpenDriversBasePathInExplorer's outcome - a DTO with
// its own Error field rather than a bare Go error, per this file's siblings
// (see app.go's own doc comment: a bare error value doesn't JSON-marshal its
// message to the frontend at all). OpenDriversBasePathInExplorer itself is
// platform-specific - see openfolder_windows.go/openfolder_darwin.go.
type OpenFolderResult struct {
	Error string `json:"error"`
}
