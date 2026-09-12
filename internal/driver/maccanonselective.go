package driver

import (
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CanonCoreDevicePackages locates, within a Canon UFR II-style Distribution
// package (already expanded via `pkgutil --expand`), the "Core" sub-package
// (the real driver framework/backend/PDE filter binaries - genuinely needed,
// installed for real via `installer`) and the "Device" sub-package (which
// ships one PPD + one per-model "Recipe" bundle for every model the driver
// family supports - 549 of each in a real UFR II download inspected live,
// only one of which any single Deploy row ever needs).
//
// Confirmed against a real UFR II package that Canon's own naming
// convention is "<anything>_Core.pkg"/"<anything>_Device.pkg" (seen as
// "Canon_Family_Printer_Core.pkg"/"Canon_Family_Printer_Device.pkg") -
// matched by suffix so a version-number-only-varying prefix doesn't matter.
// ok is false when either sub-package isn't found under expandedDir at all -
// an unexpected Distribution shape (a different Canon package, or a future
// driver version's layout changed) - ExtractCanonDeviceFiles's own caller
// falls back to installing the whole Distribution the old way rather than
// guessing further.
func CanonCoreDevicePackages(expandedDir string) (corePkgPath, devicePkgPath string, ok bool) {
	entries, err := os.ReadDir(expandedDir)
	if err != nil {
		return "", "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		switch {
		case strings.HasSuffix(e.Name(), "_Core.pkg"):
			corePkgPath = filepath.Join(expandedDir, e.Name())
		case strings.HasSuffix(e.Name(), "_Device.pkg"):
			devicePkgPath = filepath.Join(expandedDir, e.Name())
		}
	}
	return corePkgPath, devicePkgPath, corePkgPath != "" && devicePkgPath != ""
}

// CanonPPDBaseName strips a PPD filename's ".ppd.gz"/".ppd" extension -
// Canon's own Recipe bundle and its sibling .rcp symlink (see
// ExtractCanonDeviceFiles) both share this exact base name with the PPD
// itself (confirmed against two real models: "CNPZUIFC5150ZU.ppd.gz" pairs
// with "Recipe/CNPZUIFC5150ZU.bundle/" and "Recipe/CNPZUIFC5150ZU.rcp").
func CanonPPDBaseName(ppdFilename string) string {
	base := filepath.Base(ppdFilename)
	base = strings.TrimSuffix(base, ".gz")
	return strings.TrimSuffix(base, ".ppd")
}

// ExtractCanonDeviceFiles selectively cpio-extracts just one model's own
// files out of devicePkgPath's gzip-compressed Payload - not the other ~548
// models' worth Device.pkg also ships - into destDir, preserving each
// entry's real eventual install path relative to destDir (e.g.
// destDir/Library/Printers/PPDs/Contents/Resources/<ppd>), so the caller can
// place destDir's own "Library" subtree directly under the real "/Library"
// with a plain recursive copy.
//
// Three cpio patterns, confirmed complete against two real models
// (CNPZUIFC5150ZU, CNPZUIRAC5735ZU) by cross-checking Device.pkg's own real
// Bom - nothing else in the whole package references either model's base
// name anywhere:
//   - the PPD itself (ppdFilename, e.g. "CNPZUIFC5150ZU.ppd.gz")
//   - the model's own Recipe bundle, entirely (Recipe/<base>.bundle/**)
//   - a sibling *symlink* at Recipe/<base>.rcp pointing into that same
//     bundle's own Contents/Resources/<base>.rcp - easy to miss since it
//     sits next to the bundle, not inside it; a real BOM diff is what caught
//     it live, not a guess.
func ExtractCanonDeviceFiles(devicePkgPath, ppdFilename, destDir string) error {
	payloadPath := filepath.Join(devicePkgPath, "Payload")
	f, err := os.Open(payloadPath)
	if err != nil {
		return fmt.Errorf("no Payload under %s: %w", devicePkgPath, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s's Payload is not gzip-compressed as expected: %w", devicePkgPath, err)
	}
	defer gz.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	base := CanonPPDBaseName(ppdFilename)
	cmd := exec.Command("cpio", "-idm", "--quiet",
		"*/"+ppdFilename,
		"*/Recipe/"+base+".bundle/*",
		"*/Recipe/"+base+".rcp",
	)
	cmd.Dir = destDir
	cmd.Stdin = gz
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extracting %s from %s: %w: %s", ppdFilename, devicePkgPath, err, strings.TrimSpace(string(out)))
	}

	if !dirHasAnyFile(destDir) {
		return fmt.Errorf("cpio extracted nothing for %q from %s - Device.pkg's own layout may have changed", ppdFilename, devicePkgPath)
	}
	return nil
}
