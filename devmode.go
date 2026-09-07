package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PDT/internal/driver"
	"PDT/internal/printer"
	pdtwin "PDT/internal/printer/windows"
)

// saveDevModeToConfigsFolder writes data to Configs/<SalesChainID>-<sanitized
// printerName>.bin (creating the Configs folder if needed) and returns the
// bare filename - the pointer stored on a row (printer.PrinterRow.DevModeFile),
// never the bytes themselves. Every capture/browse path writes through this
// one function, so a .bin always exists under the conventional name once a
// row has a DEVMODE assigned, regardless of how it got there - what makes
// the deploy-time "forgot to save the JSON" fallback work at all (see
// printer.ResolveDevModePath).
func saveDevModeToConfigsFolder(salesChainID, printerName string, data []byte) (fileName string, err error) {
	dir := configsRoot()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fileName = printer.DevModeFileName(salesChainID, printerName)
	if err := os.WriteFile(filepath.Join(dir, fileName), data, 0o644); err != nil {
		return "", err
	}
	return fileName, nil
}

// saveDriverDataToConfigsFolder writes a captured PrinterDriverData registry
// snapshot to Configs/<SalesChainID>-<sanitized printerName>.driverdata.json -
// the Device Settings tab sidecar alongside the DEVMODE .bin (see
// printer.ResolveDriverDataPath). JSON, not a raw binary blob like the
// DEVMODE file, since a PrinterDataValue list needs its own name/type/data
// framing - encoding/json's default []byte handling (base64) is enough, no
// custom format needed.
func saveDriverDataToConfigsFolder(salesChainID, printerName string, values []pdtwin.PrinterDataValue) error {
	dir := configsRoot()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	fileName := printer.DriverDataFileName(salesChainID, printerName)
	return os.WriteFile(filepath.Join(dir, fileName), data, 0o644)
}

// DevModeResult is CaptureDevModeForPrinter/BrowseDevModeFile's outcome.
type DevModeResult struct {
	FileName string `json:"fileName"`
	Canceled bool   `json:"canceled"`
	Error    string `json:"error"`
}

// CaptureDevModeForPrinter reads printerName's *current* DEVMODE straight
// from its driver (pdtwin.GetDevMode - the same call SetDuplexAndColor's own
// verification step already uses) and saves it to the Configs folder. Only
// works for a printer that already exists locally under that exact name -
// this is meant to be run on a reference machine where it was manually
// configured, per the workflow this whole feature exists for (see README).
//
// Also captures the printer's PrinterDriverData registry key (Device
// Settings tab - installable options, form-to-tray assignment - which most
// drivers keep entirely separate from DEVMODE) to a sidecar file, best-
// effort: a driver with nothing there, or one that rejects the read, still
// leaves the DEVMODE capture above fully successful, so failure here is
// silently swallowed rather than surfaced as this call's own error.
func (a *App) CaptureDevModeForPrinter(salesChainID, printerName string) DevModeResult {
	if strings.TrimSpace(salesChainID) == "" {
		return DevModeResult{Error: "Set a SalesChain ID before capturing a DEVMODE - the saved filename depends on it."}
	}
	p, err := pdtwin.OpenPrinter(printerName, pdtwin.PrinterAccessUse)
	if err != nil {
		return DevModeResult{Error: fmt.Sprintf("no local printer named %q was found to capture a DEVMODE from: %v", printerName, err)}
	}
	defer p.Close()

	data, err := pdtwin.GetDevMode(p.Handle, printerName)
	if err != nil {
		return DevModeResult{Error: err.Error()}
	}

	fileName, err := saveDevModeToConfigsFolder(salesChainID, printerName, data)
	if err != nil {
		return DevModeResult{Error: fmt.Sprintf("captured the DEVMODE but could not save it: %v", err)}
	}

	if values, derr := pdtwin.EnumPrinterDataEx(p.Handle, pdtwin.PrinterDriverDataKeyName); derr == nil && len(values) > 0 {
		_ = saveDriverDataToConfigsFolder(salesChainID, printerName, values)
	}

	return DevModeResult{FileName: fileName}
}

// BrowseDevModeFile lets the user manually pick an existing .bin file (e.g.
// one captured elsewhere) and associates it with printerName - re-saved
// through saveDevModeToConfigsFolder under the conventional name regardless
// of what the source file was called, so it's discoverable by the
// convention-based fallback the same as a live capture would be.
func (a *App) BrowseDevModeFile(salesChainID, printerName string) DevModeResult {
	if strings.TrimSpace(salesChainID) == "" {
		return DevModeResult{Error: "Set a SalesChain ID before assigning a DEVMODE - the saved filename depends on it."}
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Select a captured DEVMODE file",
		Filters: []runtime.FileFilter{{DisplayName: "DEVMODE Files (*.bin)", Pattern: "*.bin"}},
	})
	if err != nil || path == "" {
		return DevModeResult{Canceled: path == ""}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return DevModeResult{Error: err.Error()}
	}
	fileName, err := saveDevModeToConfigsFolder(salesChainID, printerName, data)
	if err != nil {
		return DevModeResult{Error: fmt.Sprintf("could not save the selected file: %v", err)}
	}
	return DevModeResult{FileName: fileName}
}

// LocalPrinterCandidate is one locally-installed printer offered by the
// Import Printers dialog.
type LocalPrinterCandidate struct {
	Name         string `json:"name"`
	IP           string `json:"ip"`
	Manufacturer string `json:"manufacturer"`
	Driver       string `json:"driver"`
	Physical     bool   `json:"physical"`
}

// virtualPrinterKeywords: substrings (case-insensitive) in a printer's own
// driver name that mark it as a virtual/software printer rather than a real
// network device - "Microsoft Print to PDF", "Microsoft XPS Document
// Writer", "Microsoft Shared Fax Driver", "Send to Microsoft OneNote ...",
// and similar. Best-effort, not exhaustive - the Import Printers dialog's
// checkboxes stay fully user-overridable either way, this only decides the
// default checked state.
var virtualPrinterDriverKeywords = []string{"pdf", "xps document writer", "fax", "onenote", "journal note"}

// isPhysicalPrinterGuess reports whether driverName looks like a real network
// printer's driver rather than a virtual/software one.
func isPhysicalPrinterGuess(driverName string) bool {
	lower := strings.ToLower(driverName)
	for _, kw := range virtualPrinterDriverKeywords {
		if strings.Contains(lower, kw) {
			return false
		}
	}
	return true
}

// findManufacturerForDriver looks through catalog for the manufacturer whose
// driver list contains driverName exactly - best-effort enrichment for
// Import Printers, so an already-configured reference printer's row doesn't
// need its Manufacturer/Driver retyped by hand. "" if no match (e.g. a
// manufacturer PDT doesn't know about, or none of its drivers are locally
// present to match against).
func findManufacturerForDriver(catalog driver.Catalog, driverName string) string {
	for mfg, drivers := range catalog {
		if _, ok := drivers[driverName]; ok {
			return mfg
		}
	}
	return ""
}

// EnumerateLocalPrinters lists every locally-installed printer as a
// candidate for the Import Printers dialog, with a best-effort physical/
// virtual guess and IP/Manufacturer enrichment (IP via
// FindHostByTcpIpPortName, for a Standard TCP/IP port; Manufacturer via
// findManufacturerForDriver) - a printer already deployed and manually
// configured on this reference machine already has its real target IP on
// its own port, worth recovering rather than asking the user to retype it.
func (a *App) EnumerateLocalPrinters() []LocalPrinterCandidate {
	<-a.ready
	locals, err := pdtwin.EnumLocalPrinters()
	if err != nil {
		// []LocalPrinterCandidate{}, not nil - see
		// driver.ManufacturersWithDrivers' own comment for why a nil slice
		// here is a latent frontend crash waiting to happen (the frontend
		// already guards this specific call with `|| []`, but no reason to
		// rely on that being the only caller that ever remembers to).
		return []LocalPrinterCandidate{}
	}
	out := make([]LocalPrinterCandidate, 0, len(locals))
	for _, lp := range locals {
		cand := LocalPrinterCandidate{
			Name:         lp.Name,
			Driver:       lp.DriverName,
			Manufacturer: findManufacturerForDriver(a.catalog, lp.DriverName),
			Physical:     isPhysicalPrinterGuess(lp.DriverName),
		}
		if host, found, _ := pdtwin.FindHostByTcpIpPortName(lp.PortName); found {
			cand.IP = host
		}
		out = append(out, cand)
	}
	return out
}
