package printer

import "strings"

// RequiresNulPortWorkaround reports whether resolvedDriverName (the actual
// installed/to-be-installed driver name, from driver.ResolvedDriver.Name)
// belongs to a driver family observed to be significantly slower to create a
// printer object against a live TCP/IP port than against the local NUL:
// port. Rows using such a driver automatically get the
// create-against-NUL-then-rebind-to-real-port treatment with no user toggle
// (see Create-Printers.ps1 line 1503 for the original HP observation, where
// this used to be a manual BindNulPort checkbox instead):
//
//   - HP's Universal Print Driver family: ~4 minutes live vs. ~4 seconds
//     against NUL:.
//   - Kyocera, any driver: observed taking noticeably longer than other
//     manufacturers (Canon, Ricoh) to deploy against a live port - applied
//     manufacturer-wide rather than to one driver family, since Kyocera's
//     own driver naming doesn't single out a "universal" variant the way
//     HP's does.
//
// This is pure business logic with no I/O and no Win32 dependency on purpose,
// kept in this platform-independent package (not internal/printer/windows) so
// it's unit-testable without a Windows build tag, and so the low-level
// port/printer wrappers never need to know *why* they were pointed at NUL: -
// they just get told a port name by the orchestrator that calls this.
func RequiresNulPortWorkaround(manufacturer, resolvedDriverName string) bool {
	if strings.EqualFold(manufacturer, "HP") && strings.Contains(strings.ToLower(resolvedDriverName), "universal") {
		return true
	}
	return strings.EqualFold(manufacturer, "Kyocera")
}
