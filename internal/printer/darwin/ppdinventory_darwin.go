package darwin

import "path/filepath"

// ppdResourcesDir is where CUPS keeps every installed PPD on macOS -
// confirmed against this machine's own `lpinfo -m` output, which lists every
// entry as a path relative to "/" starting with exactly this directory.
const ppdResourcesDir = "/Library/Printers/PPDs/Contents/Resources"

// snapshotPPDs lists every *.ppd/*.ppd.gz currently under ppdResourcesDir -
// the before/after diffing primitive EnsureDriverInstalled uses to discover
// what a driver package actually registered, since there is no single
// "installed driver version" registry-equivalent on macOS the way
// driverinfo_windows.go reads one straight from the registry. This is
// unprivileged - reading the PPD directory needs no elevation, only writing
// to it (via installer, see install_darwin.go) does.
func snapshotPPDs() (map[string]bool, error) {
	matches, err := filepath.Glob(filepath.Join(ppdResourcesDir, "*.ppd*"))
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(matches))
	for _, m := range matches {
		out[m] = true
	}
	return out, nil
}

// newPPDs returns every path in after that wasn't in before - what one
// install actually added.
func newPPDs(before, after map[string]bool) []string {
	var out []string
	for p := range after {
		if !before[p] {
			out = append(out, p)
		}
	}
	return out
}
