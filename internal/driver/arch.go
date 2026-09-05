package driver

import "runtime"

// PreferredArchTokens ports Get-PreferredArchTokens: which of the
// arch-normalized catalog tokens (see BuildCatalog) this machine can use, in
// preference order. "x64" and "64bit" are genuinely distinct spellings
// different vendors use for the same amd64 architecture (Canon/HP use "x64",
// Kyocera uses "64bit") and both must be listed; "32bit" already absorbs the
// "32BIT" vs "32bit" case-variance vendors use for the same 32-bit token via
// BuildCatalog's lowercase normalization, so unlike the original there is
// only one 32-bit token to return.
func PreferredArchTokens() []string {
	switch runtime.GOARCH {
	case "arm64":
		return []string{"arm64"}
	case "amd64":
		return []string{"x64", "64bit"}
	default:
		return []string{"32bit"}
	}
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
