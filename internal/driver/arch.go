package driver

// PreferredArchTokens ports Get-PreferredArchTokens: which of the
// arch-normalized catalog tokens (see BuildCatalog) to prefer, in order.
// "x64" and "64bit" are genuinely distinct spellings different vendors use
// for the same amd64 architecture (Canon/HP use "x64", Kyocera uses
// "64bit") and both must be listed; "32bit" already absorbs the "32BIT" vs
// "32bit" case-variance vendors use for the same 32-bit token via
// BuildCatalog's lowercase normalization, so unlike the original there is
// only one 32-bit token to return.
//
// A fixed universal order, not switched on runtime.GOARCH anymore - that
// used to mean "the architecture of whatever machine is running PDT
// itself", which was already a fragile assumption even Windows-only (a
// technician running PDT.exe from an ARM64 Windows laptop against an x64
// print server would pick the wrong variant), but became actively, always
// wrong once PDT started running on macOS too (GitHub issue #3's own
// mirror work): a Mac's own arm64/amd64 CPU has nothing to do with the
// REMOTE Windows machine PDT is configuring a printer on - PDT never
// installs a Windows driver onto the machine it itself runs on. Confirmed
// live as a real, reported bug (2026-09-21): on an Apple Silicon Mac, every
// manufacturer whose real Windows driver has no genuine ARM64 build (Canon,
// Sharp, Toshiba, Xerox - confirmed against real local .inf data) offered
// zero Windows Driver candidates at all, since archMapCompatible only ever
// matched "arm64". x64 first, as the real-world standard modern target;
// arm64 next (HP's own Windows-on-ARM builds today); 32bit last, legacy.
func PreferredArchTokens() []string {
	return []string{"x64", "64bit", "arm64", "32bit"}
}

// archMapCompatible ports Test-ArchMapCompatible.
func archMapCompatible(archMap map[string]ArchEntry) bool {
	if _, ok := archMap["any"]; ok {
		return true
	}
	for _, tok := range PreferredArchTokens() {
		if _, ok := archMap[tok]; ok {
			return true
		}
	}
	return false
}
