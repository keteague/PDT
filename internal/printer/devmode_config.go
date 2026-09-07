package printer

import (
	"os"
	"path/filepath"
	"strings"
)

// filenameIllegalChars are the characters Windows won't allow in a filename -
// a printer's Name is free text and isn't restricted the way SalesChain ID
// already is on the frontend, so it needs its own sanitizing before use in a
// DEVMODE filename.
const filenameIllegalChars = `\/:*?"<>|`

// SanitizeFilenamePart strips characters that aren't legal in a Windows
// filename from s, for building a DEVMODE .bin filename out of free-text
// values (a printer's Name, primarily).
func SanitizeFilenamePart(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(filenameIllegalChars, r) {
			return -1
		}
		return r
	}, s)
}

// DevModeFileName is the canonical, deterministic filename a captured DEVMODE
// for (salesChainId, printerName) is saved under - e.g.
// ("18455-1", "Copy Room") -> "18455-1-Copy Room.bin". Used both when saving
// a freshly-captured/browsed DEVMODE and when ResolveDevModePath falls back
// to deriving the name instead of trusting an explicit pointer.
func DevModeFileName(salesChainID, printerName string) string {
	return salesChainID + "-" + SanitizeFilenamePart(printerName) + ".bin"
}

// ResolveDevModePath finds the actual DEVMODE .bin file for row, if any,
// under configsRoot. Tries row.DevModeFile first (the explicit pointer saved
// alongside the row - stays correct even if row.Name is edited after
// capture), then falls back to the conventional DevModeFileName derived from
// salesChainID+row.Name - the "forgot to save the JSON after capturing"
// recovery path, since a capture always writes the conventional filename to
// disk regardless of what ends up in the row's own pointer.
func ResolveDevModePath(configsRoot, salesChainID string, row PrinterRow) (path string, ok bool) {
	if row.DevModeFile != "" {
		candidate := filepath.Join(configsRoot, row.DevModeFile)
		if fileExists(candidate) {
			return candidate, true
		}
	}
	candidate := filepath.Join(configsRoot, DevModeFileName(salesChainID, row.Name))
	if fileExists(candidate) {
		return candidate, true
	}
	return "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// DriverDataFileName mirrors DevModeFileName's convention, for the captured
// PrinterDriverData registry sidecar (see windows.CaptureDriverData) most
// drivers use for Device Settings tab data (installable options, form-to-
// tray assignment) - kept entirely separate from DEVMODE by Windows itself,
// so it needs its own file alongside the .bin rather than living inside it.
func DriverDataFileName(salesChainID, printerName string) string {
	return salesChainID + "-" + SanitizeFilenamePart(printerName) + ".driverdata.json"
}

// ResolveDriverDataPath finds row's captured driver-data sidecar file, if
// any, under configsRoot - the same two-tier lookup ResolveDevModePath uses:
// derived from row.DevModeFile's own basename first (so a captured pair
// stays associated even after a row rename), falling back to the
// SalesChainID+Name convention. Not finding one is routine, not an error -
// unlike DEVMODE, a driver-data sidecar only exists when CaptureDevModeForPrinter's
// best-effort registry capture actually found values to save.
func ResolveDriverDataPath(configsRoot, salesChainID string, row PrinterRow) (path string, ok bool) {
	if row.DevModeFile != "" {
		derived := strings.TrimSuffix(row.DevModeFile, ".bin") + ".driverdata.json"
		candidate := filepath.Join(configsRoot, derived)
		if fileExists(candidate) {
			return candidate, true
		}
	}
	candidate := filepath.Join(configsRoot, DriverDataFileName(salesChainID, row.Name))
	if fileExists(candidate) {
		return candidate, true
	}
	return "", false
}
