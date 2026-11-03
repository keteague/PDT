package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"PDT/internal/driver"
)

// flashApp is one copy of PDT that Sync keeps current on a flash drive: this
// laptop's own running app, or the other platform's from includeDir().
type flashApp struct {
	label   string // progress step / note label, e.g. "PDT.exe (Windows)"
	name    string // what it's called at the drive root: winExeName or macAppBundleName
	src     string // the file (PDT.exe) or bundle folder (PDT.app) to copy
	version string // src's version; "" if it couldn't be read
	isDir   bool   // a .app bundle rather than a single file
	// readVersion reads an existing copy's version (peInstalledVersion /
	// macAppInstalledVersion); ok false means missing or unreadable.
	readVersion func(path string) (version string, ok bool)
}

// syncAppToDrive copies app onto driveRoot only when the drive's copy is
// missing, unreadable, or older than app.version (Ken, 2026-11-03: "always
// check if the copy of the PDT application on the flash drive is out of
// date and sync that over if necessary"). Before this, Sync overwrote the
// drive's PDT.exe and merged PDT.app over the top on every run, whatever
// their versions - rewriting tens of MB each time, and DOWNGRADING a drive
// that already had a newer PDT than the laptop doing the sync. A newer copy
// on the drive is now left alone with a warning: the laptop is the one out
// of date. The new copy is staged beside the old one and only swapped in
// once complete, so a failed or canceled copy never leaves a broken app on
// the drive. Returns the note to log; a non-nil error means the drive's
// copy is still missing or out of date.
func syncAppToDrive(ctx context.Context, driveRoot string, app flashApp, progress stepProgressFunc) (FlashNote, error) {
	dest := filepath.Join(driveRoot, app.name)
	if samePath(dest, app.src) {
		// A portable copy syncing onto its own drive - it IS the drive's copy.
		return FlashNote{Level: "OK", Text: fmt.Sprintf("%s on %s is the copy running now (v%s).", app.name, driveRoot, app.version)}, nil
	}
	installed, has := app.readVersion(dest)
	_, statErr := os.Stat(dest)
	existedUnreadable := !has && statErr == nil
	if app.version == "" {
		if has {
			return FlashNote{Level: "WARN", Text: fmt.Sprintf("Couldn't read the version of this computer's %s, so the one on %s (v%s) was left as is.", app.name, driveRoot, installed)}, nil
		}
	} else if has {
		switch c := driver.CompareVersions(installed, app.version); {
		case c == 0:
			return FlashNote{Level: "OK", Text: fmt.Sprintf("%s on %s is current (v%s).", app.name, driveRoot, installed)}, nil
		case c > 0:
			return FlashNote{Level: "WARN", Text: fmt.Sprintf("%s on %s (v%s) is newer than this computer's (v%s) and was left as is - update PDT on this computer.", app.name, driveRoot, installed, app.version)}, nil
		}
	}

	var err error
	if app.isDir {
		err = replaceDirOnDrive(ctx, dest, app.src, func(p CopyProgress) {
			if progress != nil {
				progress(app.label, p)
			}
		})
	} else {
		err = replaceFileOnDrive(ctx, dest, app.src, app.label, progress)
	}
	if err != nil {
		return FlashNote{}, fmt.Errorf("copying %s onto %s: %w", app.name, driveRoot, err)
	}
	switch {
	case has && app.version != "":
		return FlashNote{Level: "OK", Text: fmt.Sprintf("Updated %s on %s from v%s to v%s.", app.name, driveRoot, installed, app.version)}, nil
	case has:
		return FlashNote{Level: "OK", Text: fmt.Sprintf("Replaced %s on %s (v%s).", app.name, driveRoot, installed)}, nil
	case existedUnreadable:
		return FlashNote{Level: "OK", Text: fmt.Sprintf("Replaced %s on %s - its version couldn't be read - with v%s.", app.name, driveRoot, app.version)}, nil
	case app.version != "":
		return FlashNote{Level: "OK", Text: fmt.Sprintf("Added %s v%s to %s.", app.name, app.version, driveRoot)}, nil
	}
	return FlashNote{Level: "OK", Text: fmt.Sprintf("Added %s to %s.", app.name, driveRoot)}, nil
}

// replaceFileOnDrive copies src to a staging file beside dest, then renames
// it over dest.
func replaceFileOnDrive(ctx context.Context, dest, src, label string, progress stepProgressFunc) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	staging := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".new")
	os.Remove(staging)
	var done int64
	report := func(files int) {
		if progress != nil {
			progress(label, CopyProgress{DoneFiles: files, TotalFiles: 1, DoneBytes: done, TotalBytes: info.Size()})
		}
	}
	report(0)
	buf := make([]byte, copyBufferSize)
	if err := copyFileProgress(ctx, staging, src, info.ModTime(), buf, func(n int64) {
		done += n
		report(0)
	}); err != nil {
		os.Remove(staging)
		return err
	}
	if err := os.Rename(staging, dest); err != nil {
		os.Remove(staging)
		return err
	}
	report(1)
	return nil
}

// replaceDirOnDrive copies the src folder to a staging folder beside dest,
// then swaps it in - a full replacement, unlike copyTreeMerge straight onto
// dest, which would leave behind files an older version had that the new
// one doesn't.
func replaceDirOnDrive(ctx context.Context, dest, src string, onProgress func(CopyProgress)) error {
	parent, base := filepath.Dir(dest), filepath.Base(dest)
	staging := filepath.Join(parent, "."+base+".new")
	old := filepath.Join(parent, "."+base+".old")
	os.RemoveAll(staging)
	if err := copyTreeMerge(ctx, staging, src, onProgress); err != nil {
		os.RemoveAll(staging)
		return err
	}
	os.RemoveAll(old)
	if dirExists(dest) {
		if err := os.Rename(dest, old); err != nil {
			os.RemoveAll(staging)
			return err
		}
	}
	if err := os.Rename(staging, dest); err != nil {
		if dirExists(old) {
			_ = os.Rename(old, dest)
		}
		os.RemoveAll(staging)
		return err
	}
	os.RemoveAll(old)
	return nil
}
