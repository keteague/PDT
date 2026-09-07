package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"PDT/internal/driver"
)

// ensureDriversScaffold creates the standard Drivers\Windows\11\<Manufacturer>
// subfolder structure - one folder per entry in driver.Manufacturers, spaces
// stripped to match this project's own on-disk convention ("Konica Minolta"
// -> "KonicaMinolta") - under root, but only if root doesn't already have
// anything in it. A no-op for a portable copy's already-populated Drivers
// folder, or a previously-scaffolded/populated installed copy - this only
// ever fires for a genuinely empty or brand-new root. Windows 11 only, not
// the full Windows/macOS version matrix the README's "Drivers folder
// layout" documents for this project's own driver archive - PDT itself only
// ever scans the Windows side today, and a fresh install has no way to know
// which Windows versions its fleet actually needs ahead of time.
func ensureDriversScaffold(root string) error {
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		return nil
	}
	win11 := filepath.Join(root, "Windows", "11")
	for _, mfg := range driver.Manufacturers {
		folder := strings.ReplaceAll(mfg, " ", "")
		if err := os.MkdirAll(filepath.Join(win11, folder), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", folder, err)
		}
	}
	return nil
}

// OpenFolderResult is OpenDriversBasePathInExplorer's outcome - a DTO with
// its own Error field rather than a bare Go error, per this file's siblings
// (see app.go's own doc comment: a bare error value doesn't JSON-marshal its
// message to the frontend at all).
type OpenFolderResult struct {
	Error string `json:"error"`
}

// OpenDriversBasePathInExplorer scaffolds path (see ensureDriversScaffold)
// if it's empty or doesn't exist yet, then opens it in File Explorer - the
// Settings > General "Drivers Base Path" field's own right-arrow button.
// Uses whatever path is currently typed in that field, not necessarily the
// last-saved Settings.DriversBasePath, so pointing it at a not-yet-saved
// location and clicking the button still does something useful.
func (a *App) OpenDriversBasePathInExplorer(path string) OpenFolderResult {
	path = strings.TrimSpace(path)
	if path == "" {
		return OpenFolderResult{Error: "no folder path given"}
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return OpenFolderResult{Error: fmt.Sprintf("creating %q: %v", path, err)}
	}
	if err := ensureDriversScaffold(path); err != nil {
		return OpenFolderResult{Error: err.Error()}
	}
	if err := exec.Command("explorer.exe", path).Start(); err != nil {
		return OpenFolderResult{Error: err.Error()}
	}
	return OpenFolderResult{}
}
