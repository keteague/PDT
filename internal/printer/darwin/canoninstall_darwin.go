package darwin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"PDT/internal/driver"
	"PDT/internal/printer"
)

// canonRecipeDir is where a Canon UFR II Device sub-package installs each
// model's own per-model "Recipe" bundle - confirmed against a real BOM
// (/Library/Printers/Canon/CUPS_Printer/Recipe/<model>.bundle/, plus a
// sibling <model>.rcp symlink pointing into it - see
// driver.ExtractCanonDeviceFiles' own doc comment).
const canonRecipeDir = "/Library/Printers/Canon/CUPS_Printer/Recipe"

// installCanonSelective is installVariant's own fast path for a Canon UFR
// II-shaped Distribution package: installs the Core sub-package for real
// (the actual driver framework/backend/PDE filter binaries - genuinely
// needed), and selectively places just this row's own one target PPD +
// matching Recipe bundle out of the Device sub-package - never running
// Device.pkg's own installer at all (it ships one PPD+Recipe pair *per
// model* the whole driver family supports - confirmed live: 549 of each in
// a real UFR II download, only one of which any single row ever needs), and
// never touching Icons/Profiles/cnaccm at all (cosmetic Print-dialog icons,
// ICC color profiles, and the Canon Accounting Manager Client utility
// respectively - none required for functional duplex/color/network/
// finishing-feature printing, which all live in the PPD's own *OpenUI
// options: confirmed live that a real Canon PPD declares *CNFinisher/
// *CNPuncher/*CNFolder/*CNSaddleStitch/*CNVfolding/*CNCopyTray directly).
//
// handled reports whether this path actually ran: false means the located
// package didn't match the expected Canon UFR II Distribution shape (a
// different manufacturer, or an unexpected/future Canon layout) -
// installVariant's own caller falls back to the plain full-package install
// in that case, never a hard failure just because this optimization doesn't
// apply. A real error only comes back once handled is true - i.e. once this
// path has actually committed to installing something.
//
// coreInstalledThisRun caches by packagePath (the same Deployer-lifetime,
// per-run cache shape ensureInstalledOnce already uses for the fallback
// path) so 2 rows sharing one package - confirmed live as the exact real
// case this matters for: two different Canon models deployed in the same
// run - only pay for Core's own real install once; each row's own PPD+
// Recipe placement is cheap enough (a small selective cpio extraction, not
// a multi-minute installer run) that it's never worth caching/skipping - it
// always runs, once per row, unconditionally.
func installCanonSelective(ctx context.Context, packagePath, ppdFilename string, coreInstalledThisRun map[string]bool, log *printer.Logger) (handled bool, err error) {
	pkgPath, cleanup, err := driver.LocatePkg(packagePath)
	defer cleanup()
	if err != nil {
		return false, nil
	}

	expandDir, err := os.MkdirTemp("", "pdt-canon-expand-*")
	if err != nil {
		return false, nil
	}
	defer os.RemoveAll(expandDir)
	expanded := filepath.Join(expandDir, "x")
	if err := exec.CommandContext(ctx, "pkgutil", "--expand", pkgPath, expanded).Run(); err != nil {
		return false, nil
	}

	corePkgPath, devicePkgPath, ok := driver.CanonCoreDevicePackages(expanded)
	if !ok {
		return false, nil
	}

	// `installer -pkg` rejects a pkgutil --expand-produced sub-package
	// directory outright - confirmed live (and reproduced here without any
	// privilege at all, so it's a format/recognition issue, not a
	// permissions one): "the package path specified was invalid" even for
	// the current user's own freshly-expanded directory. `pkgutil
	// --flatten` re-packs that same directory into a proper flat (xar-
	// format, single-file) .pkg - confirmed live that installer accepts the
	// flattened result (correctly reports "Must be run as root" instead of
	// rejecting the path, run the same non-privileged way). Only needed
	// when Core actually needs installing this call - skipped entirely once
	// coreInstalledThisRun already covers packagePath.
	var flatCorePkgPath string
	if !coreInstalledThisRun[packagePath] {
		flatCorePkgPath = filepath.Join(expandDir, "core-flat.pkg")
		if err := exec.CommandContext(ctx, "pkgutil", "--flatten", corePkgPath, flatCorePkgPath).Run(); err != nil {
			return false, nil
		}
	}

	stageDir, err := os.MkdirTemp("", "pdt-canon-stage-*")
	if err != nil {
		return true, err
	}
	defer os.RemoveAll(stageDir)
	if err := driver.ExtractCanonDeviceFiles(devicePkgPath, ppdFilename, stageDir); err != nil {
		return true, fmt.Errorf("selectively extracting %s from the Device sub-package: %w", ppdFilename, err)
	}

	base := driver.CanonPPDBaseName(ppdFilename)
	recipeBundleDir := filepath.Join(canonRecipeDir, base+".bundle")
	recipeSymlink := filepath.Join(canonRecipeDir, base+".rcp")
	ppdDest := filepath.Join(ppdResourcesDir, ppdFilename)

	var script strings.Builder
	if flatCorePkgPath != "" {
		fmt.Fprintf(&script, "installer -pkg %s -target / && ", singleQuoteShellArg(flatCorePkgPath))
	} else {
		log.Info("%q's Core package was already installed earlier in this deploy run - skipping a redundant reinstall.", packagePath)
	}
	// -X: don't copy extended attributes - confirmed live (reproduced against
	// the real /Library, then fixed, with a harmless throwaway test file
	// before wiring this in) that a plain `cp -R src/. /Library/` fails
	// outright even as root ("unable to copy extended attributes to
	// /Library/.: Operation not permitted") - `cp -R` with a trailing "/."
	// source also tries to copy the *source directory's own* xattrs onto
	// the destination directory entry itself, and /Library's own inode
	// metadata is apparently protected against that even for root. None of
	// these freshly-cpio-extracted driver files carry xattrs worth
	// preserving anyway (chown/chmod right below already re-establish the
	// ownership/permissions that actually matter).
	fmt.Fprintf(&script, "cp -RX %s/. /Library/ && ", singleQuoteShellArg(filepath.Join(stageDir, "Library")))
	fmt.Fprintf(&script, "chown -Rh root:admin %s %s && ", singleQuoteShellArg(recipeBundleDir), singleQuoteShellArg(recipeSymlink))
	fmt.Fprintf(&script, "chmod 644 %s && ", singleQuoteShellArg(ppdDest))
	fmt.Fprintf(&script, "find %s -type d -exec chmod 755 {} + && find %s -type f -exec chmod 644 {} +",
		singleQuoteShellArg(recipeBundleDir), singleQuoteShellArg(recipeBundleDir))

	if _, err := runPrivilegedShell(ctx, script.String()); err != nil {
		return true, err
	}
	coreInstalledThisRun[packagePath] = true
	return true, nil
}
