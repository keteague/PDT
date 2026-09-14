package driver

import (
	"regexp"
	"strings"
)

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
// carries (e.g. "KONICA MINOLTA C751i PS") - confirmed live, 2026-09-13,
// across all 60 real PPDs inspected (both real sub-packages of the current
// WW_Letter download) that "PS" is universal: Konica Minolta ships no
// non-PostScript language variant at all here, unlike Canon's genuine UFR
// II/PS/PPD split, so it's never a real distinguishing choice, just vendor
// boilerplate worth stripping for a clean friendly model name.
var konicaMinoltaPSSuffixRe = regexp.MustCompile(` PS$`)

// konicaMinoltaIsSimplexDefaultVariant reports whether nickName names one of
// Konica Minolta's own real "(S)" PPDs - the package's own second,
// non-default installer choice/sub-package ("..._1.pkg", localized title
// "Print (1-Sided) Driver Default" - confirmed live, 2026-09-13, straight
// from the package's own Resources/en.lproj/Localizable.strings: "TITLE" =
// "Print (2-Sided) Driver Default", "TITLE_S" = "Print (1-Sided) Driver
// Default"). Confirmed live against the real PPD content too: for the exact
// same physical model (C751i), the plain PPD's own *DefaultKMDuplex is
// "Double" and the "(S)" PPD's is "Single" - nothing else differs (same 30
// real model numbers appear in both sub-packages one-to-one, same
// underlying PDE/framework bundles per the real Distribution script) -
// "(S)" is purely a different factory-default Duplex value baked into an
// otherwise-identical PPD, not a different physical model or feature set.
// Ken's own explicit choice (2026-09-13): skip these entirely rather than
// index them as separate models - PDT already sets its own explicit Duplex
// default on every queue it creates (PrintDefaultsForNewQueue/
// SetPrintDefaults) regardless of which PPD variant installs, so the "(S)"
// copy is pure dropdown clutter with no real capability the plain PPD
// doesn't already offer under PDT's own control.
func konicaMinoltaIsSimplexDefaultVariant(nickName string) bool {
	return strings.HasSuffix(nickName, " PS (S)")
}

// konicaMinoltaCleanNickNames is indexFamilyPackage's own Konica
// Minolta-specific entry-transformation hook (see macPPDEntryExpander).
// Drops every "(S)" simplex-default duplicate entirely
// (konicaMinoltaIsSimplexDefaultVariant) - unlike Toshiba's own one-to-many
// expansion, this can only ever shrink the entry count, never grow it.
// Every entry that survives gets its own generic " PS" suffix stripped
// (konicaMinoltaPSSuffixRe) for a clean friendly model name.
func konicaMinoltaCleanNickNames(entries []ppdEntry) []ppdEntry {
	out := make([]ppdEntry, 0, len(entries))
	for _, e := range entries {
		if konicaMinoltaIsSimplexDefaultVariant(e.NickName) {
			continue
		}
		out = append(out, ppdEntry{Path: e.Path, NickName: konicaMinoltaPSSuffixRe.ReplaceAllString(e.NickName, "")})
	}
	return out
}
