package printer

import "strings"

// RequiresNulPortWorkaround reports whether resolvedDriverName (the actual
// installed/to-be-installed driver name, from driver.ResolvedDriver.Name) is
// HP's Universal Print Driver family, which has been observed to take ~4
// minutes to create a printer object against a live TCP/IP port versus ~4
// seconds against the local NUL: port. Rows using such a driver automatically
// get the create-against-NUL-then-rebind-to-real-port treatment with no user
// toggle (see Create-Printers.ps1 line 1503 for the original observation,
// where this used to be a manual BindNulPort checkbox instead).
//
// This is pure business logic with no I/O and no Win32 dependency on purpose,
// kept in this platform-independent package (not internal/printer/windows) so
// it's unit-testable without a Windows build tag, and so the low-level
// port/printer wrappers never need to know *why* they were pointed at NUL: -
// they just get told a port name by the orchestrator that calls this.
func RequiresNulPortWorkaround(manufacturer, resolvedDriverName string) bool {
	return strings.EqualFold(manufacturer, "HP") &&
		strings.Contains(strings.ToLower(resolvedDriverName), "universal")
}
