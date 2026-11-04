package main

import (
	"errors"

	"PDT/internal/driver"
)

// IsTechLaptopCopy reports whether this is the technician's own installed
// PDT - not a copy running from a flash drive, and not a portable copy
// anywhere. Driver Cleanup is offered only there (Ken, 2026-11-04): a flash
// drive's Drivers folder is a copy made for a client site, and deleting
// packages from it there isn't something to do in the field.
func (a *App) IsTechLaptopCopy() bool {
	return !a.IsRunningFromRemovableDrive() && portableExeDir() == ""
}

// errNotTechLaptop is what every tech-laptop-only action returns in a
// portable copy - Write to Flash Drive, Sync to/from Flash Drive, Cloud
// Sync, Rescan Drivers and Driver Cleanup, whose toolbar buttons are hidden
// there (Ken, 2026-11-04). Enforced here too, so hiding the button is never
// the only thing stopping it.
var errNotTechLaptop = errors.New("this is only available in the PDT installed on your laptop, not in a portable copy")

// DriverCleanupResult is DeleteDriverPackages' outcome, plus the rebuilt
// catalog's status for the toolbar's usual refresh handling.
type DriverCleanupResult struct {
	Deleted []string          `json:"deleted"`
	Failed  map[string]string `json:"failed"`
	Catalog CatalogStatus     `json:"catalog"`
}

// ListDriverCleanupPackages is the Driver Cleanup dialog's list (toolbar
// trash can, Ken 2026-11-04): every driver archive in the Drivers folder,
// Archive folders included - see driver.ListCleanupPackages.
func (a *App) ListDriverCleanupPackages() []driver.CleanupPackage {
	<-a.ready
	if !a.IsTechLaptopCopy() {
		return []driver.CleanupPackage{}
	}
	return driver.ListCleanupPackages(driversRoot())
}

// DeleteDriverPackages deletes the selected archives and every piece of
// cached data PDT keeps for them (driver.DeleteDriverPackages), then
// rebuilds the driver catalog so they disappear from it.
//
// Each deleted path is also unchecked in Cloud Sync: a package deleted here
// that's still in the cloud repository would otherwise show up in the next
// Cloud Sync as a download, checked by default, and quietly come back. The
// technician can still check it there to bring it back on purpose.
func (a *App) DeleteDriverPackages(relPaths []string) DriverCleanupResult {
	<-a.ready
	if !a.IsTechLaptopCopy() {
		return DriverCleanupResult{Deleted: []string{}, Failed: map[string]string{"*": errNotTechLaptop.Error()}, Catalog: a.GetCatalogStatus()}
	}
	deleted, failed := driver.DeleteDriverPackages(driversRoot(), relPaths)
	result := DriverCleanupResult{Deleted: deleted, Failed: failed}
	if result.Deleted == nil {
		result.Deleted = []string{}
	}
	if len(deleted) > 0 {
		state := loadCloudSyncState()
		for _, rel := range deleted {
			state.Deselected[rel] = true
		}
		_ = saveCloudSyncState(state)
		result.Catalog = a.RefreshDriverCatalog()
	} else {
		result.Catalog = a.GetCatalogStatus()
	}
	return result
}
