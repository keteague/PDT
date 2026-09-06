package windows

import (
	"fmt"
	"os/exec"
	"strings"
)

// SetPrintConfigurationViaShell additionally updates a printer's PrintTicket-
// based configuration store via the Set-PrintConfiguration cmdlet.
//
// Confirmed necessary against a real Canon UFR II printer: SetDuplexAndColor
// (raw DEVMODE via DocumentProperties/SetPrinter) alone left this printer
// showing "duplex, color" in both Get-PrintConfiguration and - critically -
// the printer's own Shell UI (Printing Defaults/Preferences), even though the
// DEVMODE itself was already correctly set (confirmed independently via
// .NET's System.Drawing.Printing.PrinterSettings, the same mechanism
// GetActualDuplexColor uses). Running Set-PrintConfiguration updated both the
// PrintTicket store and left DEVMODE unchanged/correct - the two are
// evidently backed by different, independently-stale stores for at least
// this driver, and PrintTicket is what a user actually sees when they open
// Properties. Best-effort: this runs after SetDuplexAndColor already
// governs actual print behavior for most consumers, so a failure here is a
// [WARN] in the caller, not fatal.
func SetPrintConfigurationViaShell(name string, oneSided, mono bool) error {
	duplexArg := "TwoSidedLongEdge"
	if oneSided {
		duplexArg = "OneSided"
	}
	colorArg := "$true"
	if mono {
		colorArg = "$false"
	}

	script := fmt.Sprintf(
		"Set-PrintConfiguration -PrinterName %s -Color:%s -DuplexingMode %s",
		psSingleQuote(name), colorArg, duplexArg,
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Set-PrintConfiguration: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// psSingleQuote quotes s as a PowerShell single-quoted string literal - the
// name is user-typed (a row's printer Name), so this must not simply be
// string-concatenated into the script unescaped.
func psSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
