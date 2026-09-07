package main

import (
	"fmt"
	"os"
	"path/filepath"

	"PDT/internal/flashdrive"
)

// DriveInfo is one removable drive offered by the Write to Flash Drive
// dialog.
type DriveInfo struct {
	Letter     string `json:"letter"`
	Label      string `json:"label"`
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
}

// ListDrivesResult is ListRemovableDrives' outcome.
type ListDrivesResult struct {
	Drives []DriveInfo `json:"drives"`
	Error  string      `json:"error"`
}

// ListRemovableDrives lists every currently-mounted USB flash drive, for the
// Write to Flash Drive dialog's checklist.
func (a *App) ListRemovableDrives() ListDrivesResult {
	drives, err := flashdrive.EnumRemovableDrives()
	if err != nil {
		return ListDrivesResult{Error: err.Error()}
	}
	out := make([]DriveInfo, 0, len(drives))
	for _, d := range drives {
		out = append(out, DriveInfo{Letter: d.Letter, Label: d.Label, TotalBytes: d.TotalBytes, FreeBytes: d.FreeBytes})
	}
	return ListDrivesResult{Drives: out}
}

// BatchDriveResult is the per-drive outcome shared by FormatDrives and
// WritePortablePDT - each drive succeeds or fails independently, so one bad
// drive (write-protected, unplugged mid-operation) doesn't abort the rest.
type BatchDriveResult struct {
	Succeeded []string          `json:"succeeded"`
	Failed    map[string]string `json:"failed"`
}

// FormatDrives quick-formats every listed drive letter as exFAT. Destructive
// and irreversible - the frontend is expected to have already shown its own
// explicit "this erases everything" warning naming these exact drives before
// ever calling this.
func (a *App) FormatDrives(letters []string) BatchDriveResult {
	// Succeeded starts as []string{}, not nil, for the same reason
	// ManufacturersWithDrivers' own comment explains (a nil slice marshals
	// to JSON `null`) - the frontend already guards every read of this
	// specific field with `|| []`, but there's no reason to rely on that.
	result := BatchDriveResult{Succeeded: []string{}, Failed: map[string]string{}}
	for _, letter := range letters {
		if err := flashdrive.FormatExFAT(letter); err != nil {
			result.Failed[letter] = err.Error()
			continue
		}
		result.Succeeded = append(result.Succeeded, letter)
	}
	return result
}

// WritePortablePDT copies a portable PDT install - the running executable
// plus this laptop's own Drivers/ and Configs/ folders (driversRoot()/
// configsRoot(), the same "next to the executable" locations PDT already
// reads them from) - onto every listed drive letter. This is what actually
// turns "PDT installed on the technician's laptop" into "a portable copy on
// a flash drive": the copied exe finds its own Drivers/Configs right beside
// it exactly like it does today when run directly from a flash drive.
func (a *App) WritePortablePDT(letters []string) BatchDriveResult {
	<-a.ready
	result := BatchDriveResult{Succeeded: []string{}, Failed: map[string]string{}}

	exePath, err := os.Executable()
	if err != nil {
		result.Failed["*"] = fmt.Sprintf("could not determine this executable's own path: %v", err)
		return result
	}
	exeName := filepath.Base(exePath)
	exeData, err := os.ReadFile(exePath)
	if err != nil {
		result.Failed["*"] = fmt.Sprintf("could not read %q: %v", exePath, err)
		return result
	}

	for _, letter := range letters {
		if err := writePortablePDTTo(letter, exeName, exeData); err != nil {
			result.Failed[letter] = err.Error()
			continue
		}
		result.Succeeded = append(result.Succeeded, letter)
	}
	return result
}

func writePortablePDTTo(letter, exeName string, exeData []byte) error {
	if err := os.WriteFile(filepath.Join(letter, exeName), exeData, 0o755); err != nil {
		return fmt.Errorf("writing %s: %w", exeName, err)
	}
	if dirExists(driversRoot()) {
		if err := os.CopyFS(filepath.Join(letter, "Drivers"), os.DirFS(driversRoot())); err != nil {
			return fmt.Errorf("copying Drivers: %w", err)
		}
	}
	if dirExists(configsRoot()) {
		if err := os.CopyFS(filepath.Join(letter, "Configs"), os.DirFS(configsRoot())); err != nil {
			return fmt.Errorf("copying Configs: %w", err)
		}
	}
	return nil
}
