package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"PDT/internal/driver"
)

// writeRunbookFile writes text to "<salesChainID>-runbook.txt" under dir
// (configsRoot() at GenerateRunbook's own real call site; parameterized here
// so this is testable against a t.TempDir() instead), overwriting any
// previous runbook for the same Save ID - a fresh Runbook click is always
// meant to reflect the grid's current state, not append to or preserve
// whatever an earlier click already produced.
func writeRunbookFile(dir, salesChainID, text string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, salesChainID+"-runbook.txt")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// RunbookPrinter is one grid row's own subset of fields the Runbook report
// (GitHub issue #15) actually needs - sent fresh from the frontend at
// generate-time, the same "backend doesn't keep its own live copy of the
// grid" shape Deploy's own request type already uses. Deliberately its own
// type rather than reusing printer.PrinterRow: ID has no place on that
// Deploy-boundary type (see config.SavedRow's own doc comment), and nothing
// here needs SNMP/Mono/OneSided/AdvancedPrintingFeatures/DevModeFile at all.
type RunbookPrinter struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	IP              string `json:"ip"`
	LPDQueueName    string `json:"lpdQueueName"`
	UseExistingPort bool   `json:"useExistingPort"`
	Manufacturer    string `json:"manufacturer"`
	Model           string `json:"model"`
	// Driver/MacDriver/WindowsEnabled/MacEnabled: see printer.PrinterRow's
	// own doc comments (GitHub issue #16 - the same Windows/macOS driver
	// split, and same checkbox meaning, as a real deploy). MacDriver here is
	// the tech's own explicit pin (if any) from the new macOS Driver field -
	// preferred outright over a fresh catalog guess when present, so the
	// report reflects the exact commitment that will actually get deployed,
	// not a possibly-different auto-pick (see runbookPrinterBlock).
	// WindowsEnabled is the positive form of PrinterRow's own
	// WindowsDisabled - the frontend already tracks it that way (row.id-
	// style ergonomic JS naming), so this DTO matches rather than making the
	// frontend re-invert it just to cross the Wails bridge.
	Driver         string `json:"driver"`
	MacDriver      string `json:"macDriver"`
	WindowsEnabled bool   `json:"windowsEnabled"`
	MacEnabled     bool   `json:"macEnabled"`
}

// RunbookResult is GenerateRunbook's own outcome.
type RunbookResult struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// runbookHeader is the fixed, always-present opening section - every
// question here is something only a human on-site can answer (was driver
// install forbidden, is elevation required, whose credentials), so it's
// always emitted as a blank template for the technician to fill in by hand,
// never something PDT tries to guess.
const runbookHeader = `*** PRINT DRIVER INSTALLATION
	* Were we forbidden access to install drivers?  Yes|No
		* If Yes, who prevented us?  Name of person that prevented us
	* Elevation required for driver installation?  Yes|No
		* Do we have credentials (if Yes, supply them below)?  Yes|No
			* Username:
			* Password:
			* If No, who has credentials and how do we proceed? (e.g. in-house IT will install the driver and has been provided with instructions on which driver to use and which options to configure)
`

// runbookFooter is the fixed, always-present closing section - same
// human-only reasoning as runbookHeader, appended once after every printer
// entry rather than once per printer.
const runbookFooter = `	* Number of endpoints:
	* List of endpoints unavailable or inaccessible for print driver/object installation during preinstall, preferably by the name of the user so that the service tech can install it for them on day of delivery:
		* User1
		* User2
		* User3
`

// resolveOtherPlatformDriverFunc looks up manufacturer/model against the
// *other* platform's own driver catalog - only ever non-nil on Windows (see
// GenerateRunbook), since that's the only platform with both its own native
// catalog and a macOS-shaped one (GitHub issue #3); a macOS build has no
// Windows catalog at all, so the Windows lines simply stay blank there. ok
// is false whenever nothing resolves (not model-driven, no match, or the
// background macOS catalog build - app_windows.go's own loadCatalog - just
// hasn't finished yet on a very fresh launch).
type resolveOtherPlatformDriverFunc func(manufacturer, model string) (driverName string, ok bool)

// runbookPortType renders one row's own port configuration the way a
// technician would actually describe it - mirrors the exact same three-way
// precedence Deploy itself applies (UseExistingPort wins outright; an LPD
// queue name means LPR; otherwise plain Standard TCP/IP), just as prose
// instead of a live port creation.
func runbookPortType(p RunbookPrinter) string {
	switch {
	case p.UseExistingPort:
		return "Use existing port"
	case p.LPDQueueName != "":
		return fmt.Sprintf("LPR on %s (queue: %s)", p.IP, p.LPDQueueName)
	default:
		return fmt.Sprintf("Standard TCP/IP on %s", p.IP)
	}
}

// runbookDownloadURL resolves manufacturer/platform/driverName against
// Settings > Direct Downloads (GitHub issue #19) - "" whenever driverName
// itself is blank (nothing to match against) or nothing was ever configured
// for the resolved family, exactly the same "leave it blank rather than
// guess" contract MatchDirectDownloadFamily's own callers already rely on.
func runbookDownloadURL(urls map[string]map[string]map[string]string, manufacturer, platform, driverName string) string {
	if driverName == "" {
		return ""
	}
	family, ok := driver.MatchDirectDownloadFamily(manufacturer, platform, driverName)
	if !ok {
		return ""
	}
	return urls[manufacturer][platform][family]
}

// runbookPrinterBlock renders one printer's own section - showID controls
// whether the "* ID:" line appears at all (GitHub issue #15's own explicit
// shape: present, even blank, once there's more than one printer to tell
// apart; absent entirely for a single-MFD survey, matching the simpler
// template that never needed one).
//
// The platform this build doesn't natively know shows a plain "<Manufacturer>
// <Model>" (e.g. "Canon imageFORCE C331F") - not resolveOther's own raw
// return value, which for a real mac-model-driven manufacturer is
// macVariantLabel's own internal "<model> (UFR II)"-shaped family label
// (confirmed against directdownload.go's own doc comment) - a technician
// reading this report needs "which printer/model to search for," not PDT's
// own internal family disambiguation text; that raw label is still used
// as-is for the Direct Downloads lookup right below, since that's exactly
// the string format MatchDirectDownloadFamily's own token matching expects.
func runbookPrinterBlock(p RunbookPrinter, resolveMacDriver resolveOtherPlatformDriverFunc, urls map[string]map[string]map[string]string, showID bool) string {
	windowsDriver := ""
	if p.WindowsEnabled {
		windowsDriver = p.Driver
	}
	macDriver, macMatchLabel := "", ""
	if p.MacEnabled {
		// The tech's own explicit pin (the new macOS Driver field, GitHub
		// issue #16 - auto-filled the moment Model resolves, but always
		// overridable) wins outright when present - the report should
		// reflect exactly what will actually get deployed, not a possibly-
		// different fresh guess. Only fall back to resolving one here when
		// somehow still blank (macOS checked but nothing ever resolved/was
		// picked) - a defensive catch-all, not the primary path anymore.
		macDriver, macMatchLabel = p.MacDriver, p.MacDriver
		if macDriver == "" && resolveMacDriver != nil {
			if label, ok := resolveMacDriver(p.Manufacturer, p.Model); ok {
				macDriver = strings.TrimSpace(p.Manufacturer + " " + p.Model)
				macMatchLabel = label
			}
		}
	}
	windowsURL := runbookDownloadURL(urls, p.Manufacturer, driver.DirectDownloadPlatformWindows, windowsDriver)
	macURL := runbookDownloadURL(urls, p.Manufacturer, driver.DirectDownloadPlatformMac, macMatchLabel)

	var b strings.Builder
	if showID {
		fmt.Fprintf(&b, "\t* ID: %s\n", p.ID)
	}
	fmt.Fprintf(&b, "\t* Print Object Name: %s\n", p.Name)
	fmt.Fprintf(&b, "\t\t* Port Type: %s\n", runbookPortType(p))
	b.WriteString("\t\t* Print Driver: \n")
	fmt.Fprintf(&b, "\t\t\tWindows: %s\n", windowsDriver)
	fmt.Fprintf(&b, "\t\t\t\t* Windows driver download link: %s\n", windowsURL)
	fmt.Fprintf(&b, "\t\t\tmacOS: %s\n", macDriver)
	fmt.Fprintf(&b, "\t\t\t\t* macOS driver download link: %s\n", macURL)
	b.WriteString("\t* Local Print Object or Server Shared: \n")
	b.WriteString("\t\t* If server:\n")
	b.WriteString("\t\t\t* Hostname: \n")
	b.WriteString("\t\t\t* Who configured the print object: \n")
	b.WriteString("\t\t\t* Deployment method:\n")
	b.WriteString("\t\t\t\t* GPO\n")
	b.WriteString("\t\t\t\t* \\\\printServer01 > right-click > connect\n")
	return b.String()
}

// buildRunbookText assembles the complete report - a pure function (no App/
// catalog access) so it's fully testable with a fake resolveMacDriver,
// mirroring this codebase's own established "extract the pure logic, test
// without a live App" discipline (e.g. matchingConfigFiles in
// exportconfigs.go). Driven entirely by each printer's own explicit
// Driver/MacDriver/WindowsEnabled/MacEnabled fields now (GitHub issue #16) -
// unlike this function's own first version, there's no need to know which
// platform actually generated the report any more, since both drivers are
// independent, already-resolved fields on the row itself rather than one
// shared, platform-ambiguous Driver field.
func buildRunbookText(printers []RunbookPrinter, resolveMacDriver resolveOtherPlatformDriverFunc, urls map[string]map[string]map[string]string) string {
	var b strings.Builder
	b.WriteString(runbookHeader)
	showID := len(printers) > 1
	for _, p := range printers {
		b.WriteString("\n")
		b.WriteString(runbookPrinterBlock(p, resolveMacDriver, urls, showID))
	}
	b.WriteString("\n")
	b.WriteString(runbookFooter)
	return b.String()
}

// GenerateRunbook builds the site-survey print-driver-installation report
// (GitHub issue #15) for the given printers, writes it to
// "<salesChainID>-runbook.txt" under the Configs folder - deliberately the
// exact same folder (and naming convention: starts with salesChainID)
// ExportConfigs' own matchingConfigFiles already scans, so the runbook rides
// along with the rest of this Save ID's Configs files the next time the
// technician runs Export Configs, no changes needed there at all - and opens
// it in Notepad (Windows) or TextEdit (macOS) via openInTextEditor
// (runbook_windows.go/runbook_darwin.go).
func (a *App) GenerateRunbook(salesChainID string, printers []RunbookPrinter) RunbookResult {
	<-a.ready
	if len(printers) == 0 {
		return RunbookResult{Error: "no printers to include in the runbook"}
	}

	text := buildRunbookText(printers, a.resolveOtherPlatformDriver, a.settings.DirectDownloadURLs)

	path, err := writeRunbookFile(configsRoot(), salesChainID, text)
	if err != nil {
		return RunbookResult{Error: err.Error()}
	}
	if err := openInTextEditor(path); err != nil {
		return RunbookResult{Path: path, Error: err.Error()}
	}
	return RunbookResult{Path: path}
}
