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

// installKyoceraSelective is installVariant's own fast path for a Kyocera
// "Web Build" package (installCanonSelective's own sibling - see that
// function's doc comment for the shared shape, this one's own doc comment
// only calls out what's different for Kyocera specifically): installs every
// *other* sub-package (the actual driver framework/CUPS filters/PDEs/Print
// Panel App/etc, everything except the 3 PPD-bearing choices) for real, then
// selectively cpio-extracts just this row's own one target PPD out of the
// baseline PPD-installer sub-package's own Payload - never running the
// "Duplex On"/"Net Manager On" choices' own installer at all (see
// driver.KyoceraSelectivePackages' own doc comment for why: both ship the
// exact same PPD set as the baseline, patched afterward through a slow
// per-file shell loop, for a default value PDT's own post-install lpadmin
// call immediately overwrites anyway regardless of which variant was
// "installed").
//
// Unlike Canon's Core.pkg (one sub-package), Kyocera splits its own
// functional pieces across 16 - all 16 get flattened and installed together
// in one combined call, deduplicated by packagePath the same way Canon's
// single Core.pkg is (coreInstalledThisRun). Also unlike Canon's Device.pkg
// (Recipe bundle + symlink per model), Kyocera's own PPDs are flat files
// with nothing else attached - the staged extraction directory's own
// contents copy directly into ppdResourcesDir, no nested destination path
// to preserve.
func installKyoceraSelective(ctx context.Context, packagePath, ppdFilename string, coreInstalledThisRun map[string]bool, log *printer.Logger) (handled bool, err error) {
	pkgPath, cleanup, err := driver.LocatePkg(packagePath)
	defer cleanup()
	if err != nil {
		return false, nil
	}

	expandDir, err := os.MkdirTemp("", "pdt-kyocera-expand-*")
	if err != nil {
		return false, nil
	}
	defer os.RemoveAll(expandDir)
	expanded := filepath.Join(expandDir, "x")
	if err := exec.CommandContext(ctx, "pkgutil", "--expand", pkgPath, expanded).Run(); err != nil {
		return false, nil
	}

	ppdInstallerPkgPath, otherPkgPaths, ok := driver.KyoceraSelectivePackages(expanded)
	if !ok {
		return false, nil
	}

	stageDir, err := os.MkdirTemp("", "pdt-kyocera-stage-*")
	if err != nil {
		return true, err
	}
	defer os.RemoveAll(stageDir)
	if err := driver.ExtractKyoceraPPD(ppdInstallerPkgPath, ppdFilename, stageDir); err != nil {
		return true, fmt.Errorf("selectively extracting %s from the PPD installer sub-package: %w", ppdFilename, err)
	}

	var script strings.Builder
	if !coreInstalledThisRun[packagePath] {
		var flatPaths []string
		for _, p := range otherPkgPaths {
			flat := p + "-flat.pkg"
			if err := exec.CommandContext(ctx, "pkgutil", "--flatten", p, flat).Run(); err != nil {
				return false, nil // couldn't flatten one - fall back to the old full-install path rather than guessing further
			}
			flatPaths = append(flatPaths, flat)
		}
		for _, flat := range flatPaths {
			fmt.Fprintf(&script, "installer -pkg %s -target / && ", singleQuoteShellArg(flat))
		}
	} else {
		log.Info("%q's non-PPD components were already installed earlier in this deploy run - skipping a redundant reinstall.", packagePath)
	}

	ppdDest := filepath.Join(ppdResourcesDir, ppdFilename)
	fmt.Fprintf(&script, "cp -RX %s/. %s/ && ", singleQuoteShellArg(stageDir), singleQuoteShellArg(ppdResourcesDir))
	fmt.Fprintf(&script, "chown root:admin %s && chmod 644 %s", singleQuoteShellArg(ppdDest), singleQuoteShellArg(ppdDest))

	if _, err := runPrivilegedShell(ctx, script.String()); err != nil {
		return true, err
	}
	coreInstalledThisRun[packagePath] = true
	return true, nil
}
