package driver

import "strings"

// DirectDownloadPlatformWindows/DirectDownloadPlatformMac are the two
// platform keys Settings > Direct Downloads (GitHub issue #19) and
// MatchDirectDownloadFamily both use - matching this codebase's own
// established "Windows"/"macOS" capitalization convention elsewhere
// (Drivers/Windows/<version>/..., Drivers/macOS/<Manufacturer>/...).
const (
	DirectDownloadPlatformWindows = "Windows"
	DirectDownloadPlatformMac     = "macOS"
)

// directDownloadFamily is one manufacturer/platform's own named driver
// family for Settings > Direct Downloads - Tokens is the required
// substring set (matched the same way defaultDriverTokens already does:
// normalizeDriverNameForMatch on both sides, every token must be present,
// case/whitespace-insensitive, order-independent) that identifies a real
// Driver-field string as belonging to this family. A nil/empty Tokens list
// is only valid when this is the *sole* family for that manufacturer/
// platform pair (see MatchDirectDownloadFamily's own doc comment for why
// that's a real, necessary case, not a shortcut).
type directDownloadFamily struct {
	Label  string
	Tokens []string
}

// directDownloadFamilies: manufacturer -> platform -> the ordered families
// Settings > Direct Downloads offers a URL field for, and
// MatchDirectDownloadFamily matches a real Driver-field string against.
// Deliberately not exhaustive over every manufacturer/platform this
// codebase already knows about (see Manufacturers) - only the ones a
// technician actually wants a direct download link for get an entry here;
// everything else is simply absent from Settings > Direct Downloads entirely
// (Ken's own explicit scope, 2026-09-18: "For now, these are the primary
// vendors I work with... we can add more later" - adding a manufacturer or
// family later is exactly one new map entry here, no other code changes
// needed).
//
// Token provenance, since this codebase's own culture is "confirmed live,
// not guessed" wherever possible:
//   - Windows Canon "UFR II" (["UFR","II"]), Kyocera "KX" (["KX"]), Ricoh
//     "PCL6"/"PS" (["PCL6"]/["PS"]), and Sharp "UD3 PCL6" (["UD3"]) are all
//     confirmed against this codebase's own real .inf test fixtures/doc
//     comments (default.go's own defaultDriverTokens; model_test.go;
//     testdata/Ricoh/.../ricoh.inf; testdata/Sharp/.../sharp.inf) - real
//     vendor-declared driver names, not guessed.
//   - Windows Canon "PCL6"/"PS" (["PCL6"]/["PS3"]) are inferred from Canon's
//     own confirmed "Generic Plus <X>" naming convention (only "Generic
//     Plus UFR II" is independently confirmed in this codebase) plus the
//     real download filenames Ken gave directly (Generic_Plus_PCL6_v3.50,
//     Generic_Plus_PS3_v3.50) - reasonable, but not independently confirmed
//     against a real installed Canon PCL6/PS driver the way UFR II is. "PS3"
//     specifically, not bare "PS" - confirmed live as a real false-positive
//     risk during testing: Canon's own genuinely real "LIPS4" printer
//     language contains "ps" as a plain substring.
//   - macOS Canon families use the real *displayed* Driver-field text
//     (macVariantLabel/macLanguageDisplayNames, macmodel.go), not the
//     internal classification token - a real Canon UFR II mac variant's
//     own Driver-field label literally reads "<model> (UFR II)", PS reads
//     "(PostScript)", and PPD reads "(Generic PPD)", confirmed straight out
//     of macLanguageDisplayNames itself.
//   - Kyocera/Ricoh/Sharp macOS ("KPDL"/"PPD"/"PPD") are each their
//     manufacturer's own *sole* real family for Direct Downloads' purposes
//     (Kyocera and Sharp each genuinely ship only one real mac package;
//     Ricoh ships several, but Ken's own example gives one representative
//     URL, not per-model coverage) - and, having only one real family, none
//     of these three needs a Tokens check at all (matches unconditionally -
//     see the entries themselves below) regardless of what the Driver-field
//     label actually reads: Kyocera's genuinely does show its real language
//     ("<model> (KPDL)", confirmed live, Ken, 2026-09-23), Sharp's is fully
//     generic ("<model> (Driver)", via macLanguageDisplayNames' own
//     "MacPS" -> "Driver" mapping), and Ricoh's real labels all read
//     "(PostScript)" regardless of which of its many real downloads
//     produced them - not usefully distinguishing for this narrower
//     purpose either way.
var directDownloadFamilies = map[string]map[string][]directDownloadFamily{
	"Canon": {
		DirectDownloadPlatformWindows: {
			{Label: "PCL6", Tokens: []string{"PCL6"}},
			// "PS3" (Postscript 3), not bare "PS" - confirmed live as a real
			// false-positive risk during testing: Canon's own real "LIPS4"
			// printer-language name contains "ps" as a plain substring
			// ("...pLIpS4..." normalized), which a bare "PS" token would
			// wrongly match.
			{Label: "PS", Tokens: []string{"PS3"}},
			{Label: "UFR II", Tokens: []string{"UFR", "II"}},
		},
		DirectDownloadPlatformMac: {
			{Label: "PPD", Tokens: []string{"Generic PPD"}},
			{Label: "PS", Tokens: []string{"PostScript"}},
			{Label: "UFR II", Tokens: []string{"UFR", "II"}},
		},
	},
	"Kyocera": {
		DirectDownloadPlatformWindows: {
			{Label: "KX", Tokens: []string{"KX"}},
		},
		DirectDownloadPlatformMac: {
			{Label: "KPDL"}, // sole family - see doc comment above
		},
	},
	"Ricoh": {
		DirectDownloadPlatformWindows: {
			{Label: "PCL6", Tokens: []string{"PCL6"}},
			{Label: "PS", Tokens: []string{"PS"}},
		},
		DirectDownloadPlatformMac: {
			{Label: "PPD"}, // sole family - see doc comment above
		},
	},
	"Sharp": {
		DirectDownloadPlatformWindows: {
			{Label: "UD3 PCL6", Tokens: []string{"UD3"}},
		},
		DirectDownloadPlatformMac: {
			{Label: "PPD"}, // sole family - see doc comment above
		},
	},
	// Konica Minolta/HP/Lexmark/Toshiba/Xerox added 2026-09-20 (GitHub issue
	// #19's own remaining scope) - Ken's own real, hand-verified links, the
	// same "primary vendors I work with" set default.go's own
	// defaultDriverTokens already covers for the Defaults panel.
	"Konica Minolta": {
		// Windows and macOS each ship exactly one real universal download
		// covering every real driver-language variant at once (a single PCL
		// & PS combo installer on Windows; a single PS package on macOS,
		// confirmed by Ken's own two URLs) - sole family either way, same
		// "nothing to disambiguate" reasoning as Kyocera/Sharp/Ricoh(mac)
		// above.
		DirectDownloadPlatformWindows: {
			{Label: "PCL & PS"},
		},
		DirectDownloadPlatformMac: {
			{Label: "PS"},
		},
	},
	"HP": {
		DirectDownloadPlatformWindows: {
			// "PCL","6" is defaultDriverTokens' own confirmed real HP UPD
			// display name ("HP Universal Printing PCL 6" - see its own doc
			// comment/candidates_test.go). "PS" for the sibling PostScript
			// UPD isn't independently confirmed the same way, but follows
			// this codebase's own established bare-"PS" convention
			// (Ricoh's own Windows PS family, right above).
			{Label: "PCL6", Tokens: []string{"PCL", "6"}},
			{Label: "PS", Tokens: []string{"PS"}},
		},
		DirectDownloadPlatformMac: {
			// HP Easy Start is an installer *app* (HP's own guided setup
			// bundle), not a PPD/driver package the way every other mac
			// family here is - still the one real, sole download HP
			// provides for macOS, so it gets the same "sole family, no
			// tokens" treatment.
			{Label: "HP Easy Start"},
		},
	},
	"Lexmark": {
		// Windows ships one real universal combo installer (PCL6 & PS at
		// once, "Lexmark Universal v2 UD1") - sole family, same reasoning
		// as Konica Minolta's own Windows entry above. This is a different,
		// broader package than defaultDriverTokens' own "Universal v2 XL"
		// preferred-driver rule (an extra-large-format *variant* of the
		// same base driver, not a different family) - no conflict, just a
		// different concern.
		DirectDownloadPlatformWindows: {
			{Label: "PCL6 & PS"},
		},
		// macOS genuinely ships two separate real packages (Color/Mono),
		// unlike Windows' single combo installer - Label/Tokens both
		// best-effort inferred from the download filenames themselves
		// (UC1/UM1 - "Universal Color 1"/"Universal Mono 1"), not confirmed
		// against a real installed Lexmark mac PPD's own displayed
		// Driver-field text the way Canon's UFR II/PS/PPD labels are -
		// matching may not always succeed against a real Lexmark mac
		// deploy's own Driver value, same epistemic caveat this file's own
		// top-of-file doc comment already applies to Canon's inferred
		// PCL6/PS3 Windows tokens.
		DirectDownloadPlatformMac: {
			{Label: "Color", Tokens: []string{"Color"}},
			{Label: "Mono", Tokens: []string{"Mono"}},
		},
	},
	"Toshiba": {
		// Windows ships one real universal combo installer (PCL6 & PS at
		// once) - sole family. Distinct from defaultDriverTokens' own
		// "Universal Printer 2" rule, which names the *installed driver's*
		// own display text, not this download package's own contents.
		DirectDownloadPlatformWindows: {
			{Label: "PCL6 & PS"},
		},
		// macOS genuinely ships two separate real packages (Color/Mono,
		// confirmed real filenames: TOSHIBA_ColorMFP.dmg.gz/
		// TOSHIBA_MonoMFP.dmg.gz - see mactoshiba.go's own real
		// "TOSHIBA_ColorMFP_X7.gz" reference) - same best-effort,
		// inferred-not-confirmed Label/Tokens caveat as Lexmark's own mac
		// entry above: Toshiba's real mac PPDs expand into many per-model
		// *Product entries (toshibaExpandProductEntries) whose own
		// Driver-field text is the specific model name, not literally
		// "Color"/"Mono" - this token set is a best-effort convenience for
		// direct free-text searching, not a guaranteed auto-match.
		DirectDownloadPlatformMac: {
			{Label: "Color", Tokens: []string{"Color"}},
			{Label: "Mono", Tokens: []string{"Mono"}},
		},
	},
	"Xerox": {
		// Unlike every other manufacturer added here, Xerox genuinely ships
		// two distinct real Windows downloads (PCL from the Global Print
		// Driver bundle, PS from a separate model-specific driver page) -
		// "GPD" is defaultDriverTokens' own confirmed real Xerox display
		// name prefix ("Xerox GPD PCL6 V5.1076.4.0"); "PS" isn't
		// independently confirmed the same way but follows the same
		// bare-token convention as Ricoh/HP's own PS families above.
		DirectDownloadPlatformWindows: {
			{Label: "PCL", Tokens: []string{"GPD", "PCL", "6"}},
			{Label: "PS", Tokens: []string{"GPD", "PS"}},
		},
		DirectDownloadPlatformMac: {
			{Label: "PPD"}, // sole family - see doc comment above
		},
	},
}

// DirectDownloadManufacturers lists, in Manufacturers' own fixed order,
// every manufacturer directDownloadFamilies actually has an entry for -
// what Settings > Direct Downloads iterates to decide which manufacturers
// to render a section for at all (one with no entry here is simply absent
// from that tab, not shown with empty fields).
func DirectDownloadManufacturers() []string {
	var out []string
	for _, m := range Manufacturers {
		if _, ok := directDownloadFamilies[m]; ok {
			out = append(out, m)
		}
	}
	return out
}

// DirectDownloadFamiliesFor returns manufacturer's own ordered family labels
// for platform (DirectDownloadPlatformWindows/Mac) - what Settings > Direct
// Downloads renders one URL text field per, for that manufacturer/platform
// pair. nil if manufacturer has no Direct Downloads entry at all, or none
// for that specific platform.
func DirectDownloadFamiliesFor(manufacturer, platform string) []string {
	families := directDownloadFamilies[manufacturer][platform]
	if len(families) == 0 {
		return nil
	}
	out := make([]string, len(families))
	for i, f := range families {
		out[i] = f.Label
	}
	return out
}

// MatchDirectDownloadFamily returns which of manufacturer's own Direct
// Download families (for platform) driverOrLabel most likely belongs to -
// best-effort, matching defaultDriverTokens' own established discipline
// (every token in a family's own Tokens list must appear, case/whitespace-
// insensitive, substring, via normalizeDriverNameForMatch on both sides).
// driverOrLabel is whatever's actually sitting in the Driver field right
// now - a plain Windows catalog name, a decorated multi-version label, or a
// macOS MacPPDVariant.Label - this doesn't care which, since it only ever
// substring-matches against the token set.
//
// A family with a nil/empty Tokens list (the "sole family for this
// manufacturer/platform" case - see directDownloadFamilies' own doc
// comment) always matches, regardless of driverOrLabel's own content -
// there's nothing to disambiguate when there's only one real answer.
// Checked only when it's genuinely the only family for that
// manufacturer/platform pair (len(families) == 1), never as a silent
// fallback among several real candidates - a family that's one of several
// still needs its own real tokens to match, or none of them win.
//
// ok is false when manufacturer/platform has no Direct Downloads entry at
// all, or driverOrLabel doesn't match any of its families - the caller (a
// future #18/#15 consumer) then has no automatic answer and falls back to
// whatever it already does without a direct download link.
func MatchDirectDownloadFamily(manufacturer, platform, driverOrLabel string) (family string, ok bool) {
	families := directDownloadFamilies[manufacturer][platform]
	if len(families) == 0 {
		return "", false
	}
	if len(families) == 1 && len(families[0].Tokens) == 0 {
		return families[0].Label, true
	}
	norm := normalizeDriverNameForMatch(driverOrLabel)
	for _, f := range families {
		if len(f.Tokens) == 0 {
			continue
		}
		matched := true
		for _, tok := range f.Tokens {
			if !strings.Contains(norm, normalizeDriverNameForMatch(tok)) {
				matched = false
				break
			}
		}
		if matched {
			return f.Label, true
		}
	}
	return "", false
}
