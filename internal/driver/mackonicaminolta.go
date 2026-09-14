package driver

import "regexp"

// isKonicaMinoltaA4RegionDir reports whether name is one of Konica
// Minolta's own real A4-paper-region variant folder names ("A4", "WW_A4" -
// confirmed live, 2026-09-13, across both real download generations
// inspected: the older shape nests "Driver/OS_<ver>_x/A4|Letter/", the
// newest nests "WW_A4|WW_Letter/" directly) - skipped entirely during the
// catalog scan (scanMacPackages) so only the Letter-region copy is ever
// catalogued. Ken's own explicit choice (2026-09-13): US Letter only,
// matching every other US-region default already established in this
// project (HP's own "raw" LPD queue name, Xerox's own "lp") - a real
// region choice, not a decoy/junk file the way Sharp's own
// Generic_GUC_PrinterSoftware package was; both region copies are genuine,
// differently-sized, different-checksum real driver packages (confirmed
// live), just built for a paper size US deployments don't use. The two
// region copies share the exact same real filename (confirmed live) - the
// only way to tell them apart is the folder they sit under, which is why
// this needs to skip a whole directory during the walk rather than
// classifyMacFamily's own basename-only substring convention, which can't
// see path segments at all.
func isKonicaMinoltaA4RegionDir(name string) bool {
	return name == "A4" || name == "WW_A4"
}

// konicaMinoltaPSSuffixRe matches the generic, non-distinguishing " PS"
// trailing language tag every real Konica Minolta PPD's own *NickName
// carries (e.g. "KONICA MINOLTA C751i PS", "KONICA MINOLTA C3321i PS (S)") -
// confirmed live, 2026-09-13, across all 60 real PPDs inspected (both real
// sub-packages of the current WW_Letter download) that "PS" is universal:
// Konica Minolta ships no non-PostScript language variant at all here,
// unlike Canon's genuine UFR II/PS/PPD split, so it's never a real
// distinguishing choice, just vendor boilerplate worth stripping for a
// clean friendly model name. The trailing "(S)" qualifier some models also
// carry is deliberately preserved, not stripped along with it: it names a
// real, separately-installable driver variant (that package's own second,
// non-default installer Choice1/sub-package, "..._1.pkg" - confirmed live
// via the real Distribution script - covering a genuinely different,
// non-overlapping set of model suffixes from the first, default
// sub-package) rather than a cosmetic naming difference; collapsing it away
// would silently merge two real driver variants under the same friendly
// model name. Matches " PS" either at the very end, or immediately before a
// trailing " (S)".
var konicaMinoltaPSSuffixRe = regexp.MustCompile(` PS( \(S\))?$`)

// konicaMinoltaCleanNickNames is indexFamilyPackage's own Konica
// Minolta-specific entry-transformation hook (see macPPDEntryExpander) -
// unlike Toshiba's own one-to-many expansion, Konica Minolta's real PPDs
// already name their own model directly (one PPD per real model, confirmed
// live), so this keeps the entry count 1:1 and only rewrites each entry's
// own NickName to strip the generic " PS" suffix (konicaMinoltaPSSuffixRe).
func konicaMinoltaCleanNickNames(entries []ppdEntry) []ppdEntry {
	out := make([]ppdEntry, len(entries))
	for i, e := range entries {
		out[i] = ppdEntry{Path: e.Path, NickName: konicaMinoltaPSSuffixRe.ReplaceAllString(e.NickName, "$1")}
	}
	return out
}
