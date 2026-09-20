package printer

// RequiresNulPortWorkaround reports whether a Windows deploy should create the
// printer object against the local NUL: port first and rebind it to the real
// Standard TCP/IP port afterward. Now true for every row (Ken, 2026-09-20):
// creating a printer object against a live TCP/IP port makes the spooler
// probe the device, which is slow and varies by driver - real numbers from a
// nine-printer deploy (2026-09-20): Xerox 21s, Toshiba 19s and Canon 16s to
// create the object against the live port, versus 4s for the HP Universal
// driver once it was bound to NUL: (it took ~4 minutes against a live port,
// the original observation - see Create-Printers.ps1 line 1503, where this
// used to be a manual BindNulPort checkbox). Applied to everything rather
// than a growing per-manufacturer list: HP Universal, Kyocera and Lexmark
// were already on it, and the rebind step is the same either way.
//
// The parameters are kept (rather than dropped) so a driver or manufacturer
// that ever genuinely needs the live-port path has one obvious place to opt
// out. Pure business logic with no I/O and no Win32 dependency on purpose,
// kept in this platform-independent package so it stays unit-testable without
// a Windows build tag.
func RequiresNulPortWorkaround(manufacturer, resolvedDriverName string) bool {
	return true
}
