package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"PDT/internal/driver"
)

// FlashOnlyDrive is one flash drive's driver packages that this laptop
// doesn't have - offered for review before a Sync to Flash Drive (Ken,
// 2026-11-04: with Drivers selected, the drive's Drivers repo should match
// the tech laptop's, but nothing on the drive is removed without asking).
type FlashOnlyDrive struct {
	Letter   string                  `json:"letter"`
	Packages []driver.CleanupPackage `json:"packages"`
	Error    string                  `json:"error"`
}

// ListFlashOnlyDrivers returns, for each drive in letters, the driver
// packages in its Drivers folder that aren't in this laptop's - compared by
// path within the Drivers folder, ignoring case (NTFS and exFAT both do), the
// same packages Driver Cleanup lists (driver.ListCleanupPackages). A drive
// with none is still listed, with an empty Packages. Sync to Flash Drive
// copies from the laptop only - Drivers from the cloud aren't compared here.
func (a *App) ListFlashOnlyDrivers(letters []string) []FlashOnlyDrive {
	<-a.ready
	out := make([]FlashOnlyDrive, 0, len(letters))
	if !a.IsTechLaptopCopy() {
		for _, l := range letters {
			out = append(out, FlashOnlyDrive{Letter: l, Packages: []driver.CleanupPackage{}, Error: errNotTechLaptop.Error()})
		}
		return out
	}
	laptop := laptopPackageSet()
	for _, letter := range letters {
		out = append(out, FlashOnlyDrive{Letter: letter, Packages: flashOnlyPackages(letter, laptop)})
	}
	return out
}

func laptopPackageSet() map[string]bool {
	set := map[string]bool{}
	for _, p := range driver.ListCleanupPackages(driversRoot()) {
		set[strings.ToLower(p.RelPath)] = true
	}
	return set
}

// flashOnlyPackages lists letter's Drivers packages not in laptop (a set of
// lowercased RelPaths). Nothing when the drive's Drivers folder is this
// laptop's own (samePath).
func flashOnlyPackages(letter string, laptop map[string]bool) []driver.CleanupPackage {
	out := []driver.CleanupPackage{}
	driveDrivers := filepath.Join(letter, "Drivers")
	if samePath(driveDrivers, driversRoot()) || !dirExists(driveDrivers) {
		return out
	}
	for _, p := range driver.ListCleanupPackages(driveDrivers) {
		if !laptop[strings.ToLower(p.RelPath)] {
			out = append(out, p)
		}
	}
	return out
}

// deleteFlashOnlyDrivers deletes the packages chosen for deletion on letter
// (with their cached data - driver.DeleteDriverPackages, so it can only ever
// delete a driver archive inside a manufacturer folder), re-checking each
// against the laptop first: a package that has since appeared on the laptop
// is kept, since the sync is about to copy it there anyway.
func deleteFlashOnlyDrivers(letter string, relPaths []string, result *BatchDriveResult) {
	if len(relPaths) == 0 {
		return
	}
	laptop := laptopPackageSet()
	var toDelete []string
	for _, rel := range relPaths {
		if laptop[strings.ToLower(rel)] {
			result.Notes = append(result.Notes, FlashNote{Level: "INFO", Text: fmt.Sprintf("Kept %s on %s - it's on this computer now.", rel, letter)})
			continue
		}
		toDelete = append(toDelete, rel)
	}
	deleted, failed := driver.DeleteDriverPackages(filepath.Join(letter, "Drivers"), toDelete)
	for _, rel := range deleted {
		result.Notes = append(result.Notes, FlashNote{Level: "OK", Text: fmt.Sprintf("Deleted %s from %s.", rel, letter)})
	}
	for rel, msg := range failed {
		result.Notes = append(result.Notes, FlashNote{Level: "WARN", Text: fmt.Sprintf("Could not delete %s from %s: %s", rel, letter, msg)})
	}
}
