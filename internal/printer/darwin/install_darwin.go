package darwin

import (
	"context"
	"fmt"

	"PDT/internal/driver"
)

// EnsureDriverInstalled installs resolved's package (a .pkg run directly, or
// a .dmg mounted first via driver.LocatePkg - the exact same resolution used
// to inspect it for PackageLabel) via macOS's own `installer` tool, and
// reports which PPD(s) the install actually registered under
// /Library/Printers/PPDs/Contents/Resources - a before/after diff (see
// ppdinventory_darwin.go), the macOS equivalent of driverinfo_windows.go's
// registry read.
//
// A legitimately empty diff is not itself an error and is returned as such
// (nil, nil) - some modern drivers register via an IPP-Everywhere/AirPrint-
// style descriptor rather than installing a classic PPD at all; the caller
// (deploy_darwin.go) is the one that decides what to do about that (fall
// back to `-m everywhere`).
func EnsureDriverInstalled(ctx context.Context, resolved *driver.ResolvedMacPackage) (newPPDPaths []string, err error) {
	pkgPath, cleanup, err := driver.LocatePkg(resolved.Path)
	defer cleanup()
	if err != nil {
		return nil, fmt.Errorf("locating installer package inside %s: %w", resolved.Path, err)
	}

	before, err := snapshotPPDs()
	if err != nil {
		return nil, fmt.Errorf("snapshotting installed PPDs before install: %w", err)
	}

	if _, err := runPrivileged(ctx, []string{"installer", "-pkg", pkgPath, "-target", "/"}); err != nil {
		return nil, fmt.Errorf("installing %s: %w", pkgPath, err)
	}

	after, err := snapshotPPDs()
	if err != nil {
		return nil, fmt.Errorf("snapshotting installed PPDs after install: %w", err)
	}

	return newPPDs(before, after), nil
}
