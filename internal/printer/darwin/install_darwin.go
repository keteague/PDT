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
//
// This is the "install the whole package" fallback - for a Canon UFR II-
// shaped Distribution specifically, canoninstall_darwin.go's own selective
// path runs instead whenever it can (skips ~548 other models' worth of
// unused PPDs/Recipe bundles Device.pkg also ships, and Icons/Profiles/
// cnaccm entirely), falling back to this full install when the Distribution
// doesn't match that expected shape.
//
// A real, confirmed-live cost worth knowing about this path: a real Canon
// UFR II distribution package took 5m02s to install this way. An attempt
// this same session to get real phase-by-phase timing out of it (piping
// `installer -verboseR` through `do shell script`) was abandoned after
// three separate broken attempts - confirmed live (via a fast, harmless
// synthetic diagnostic, not a real 5-minute install) that `do shell
// script`'s own privileged-execution mechanism buffers a command's entire
// output until it fully exits, regardless of how many pipe stages run
// inside the script - there's no way to get genuine real-time progress or
// timing out of it, only a real architecture change (e.g. an elevated
// script writing to a file an unprivileged goroutine tails independently)
// would. Not attempted here; the selective-install path below sidesteps
// the whole question by not needing precise timing to justify itself.
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
