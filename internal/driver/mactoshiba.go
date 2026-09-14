package driver

import (
	"path/filepath"
	"strings"
)

// toshibaCanonicalModelName turns one raw *Product line's own inner text
// (already stripped of its "(...)" wrapper - see ppdProductRe) into the
// friendly model name BuildMacModelIndex registers. Confirmed live
// (2026-09-13) against Toshiba's own real PPDs that the same physical model
// can appear as more than one raw *Product line - e.g. all three of
// "TOSHIBA e-STUDIO5008LP_Loops-LP50", "TOSHIBA e-STUDIO5008LP Loops-LP50",
// and "e-STUDIO5008LP_Loops-LP50" name the same real product (an
// underscore/space spelling difference, and an inconsistent "TOSHIBA "
// prefix) - normalizing both lets toshibaExpandProductEntries deduplicate
// them down to one real model entry instead of three near-identical ones
// cluttering the dropdown.
func toshibaCanonicalModelName(raw string) string {
	name := strings.TrimSpace(raw)
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.Join(strings.Fields(name), " ")
	if !strings.HasPrefix(strings.ToUpper(name), "TOSHIBA ") {
		name = "TOSHIBA " + name
	}
	return name
}

// toshibaDriverHintFromFilename derives the short, real underlying PDL-
// variant name ("ColorMFP-S2") from a real Toshiba PPD's own filename
// ("TOSHIBA_ColorMFP_S2.gz") - strips the "TOSHIBA_" prefix and file
// extension, then turns the remaining underscores into hyphens to match
// each file's own real *NickName shape (confirmed live, 2026-09-13:
// "TOSHIBA ColorMFP-X7"/"-S2"/"-CN", hyphenated, vs. the bare
// "TOSHIBA ColorMFP" with no suffix at all for the base file - the
// underscore-to-hyphen swap produces exactly this shape for all 8 real
// files, color and mono alike). Computed from Filename rather than carried
// as a separate field through ppdEntry/MacPPDVariant/MacCatalogVariant,
// since Filename is already available (and already persisted in
// catalog.<mfg>.json) everywhere a Toshiba variant's own Label gets built -
// both the fresh-index path (indexFamilyPackage) and the cached-reload path
// (toMacPPDVariant), with no schema change needed either way. Used instead
// of the default "<model> (Driver)" shape languageDisplayName(family)
// alone would produce - confirmed live (2026-09-13) that Ken found this
// genuinely confusing in the running app: selecting "TOSHIBA e-STUDIO2525AC"
// populated the Driver field with "TOSHIBA e-STUDIO2525AC (Driver)",
// reading as if the driver name just repeats the model name, when the real
// underlying file is "TOSHIBA ColorMFP-S2" - useful troubleshooting
// information a technician would want visible, not hidden behind a
// meaningless "(Driver)" filler that only ever existed because Toshiba
// (like Kyocera/Xerox) has just one macFamilyPreference token, not a real
// choice of distinguishable driver families the way Canon's UFRII/PS/PPD
// tokens are.
func toshibaDriverHintFromFilename(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	name = strings.TrimPrefix(name, "TOSHIBA_")
	return strings.ReplaceAll(name, "_", "-")
}

// toshibaExpandProductEntries is indexFamilyPackage's own Toshiba-specific
// entry-expansion hook (see macPPDEntryExpander) - registered via
// macFamilyPreference's own "Toshiba" entry needing a genuinely different
// model-discovery mechanism from every other manufacturer here. Every other
// manufacturer's real PPDs name their own single model directly in
// *NickName (one PPD file per model); Toshiba's own real PPDs are the
// opposite - a small number of generic PDL/controller-generation files
// (e.g. "TOSHIBA_ColorMFP_X7.gz"), each one's own *NickName staying just as
// generic ("TOSHIBA ColorMFP-X7") regardless of which specific e-STUDIO
// model it's actually being used for. Confirmed live (2026-09-13) that the
// real per-model data exists anyway, just under a different PPD keyword:
// each of Toshiba's real PPDs declares many *Product lines, one per real
// e-STUDIO model that generic file covers (22 for ColorMFP-X7 alone,
// ranging e-STUDIO2040C through e-STUDIO6570C). This expands one file-level
// ppdEntry (whose own NickName is the generic PDL-variant name) into one
// ppdEntry per *Product line instead - same Path (they all really do
// install from the identical underlying PPD file), NickName replaced with
// the real, technician-recognizable model name - so BuildMacModelIndex ends
// up keying the Model dropdown by real e-STUDIO model numbers the same way
// every other manufacturer's own dropdown already works, instead of by the
// 4 generic PDL-variant names a technician would otherwise need Toshiba's
// own compatibility documentation to map to their physical hardware.
// Falls back to the original, unexpanded (generic-NickName) entry rather
// than dropping it outright if a file somehow declares no *Product line at
// all - confirmed live that none of Toshiba's real 8 files actually hit
// this (9 to 29 *Product lines each), but never silently losing a whole
// file's own coverage to a parsing gap is the same defensive discipline
// this codebase applies everywhere else a vendor's own real data can't be
// fully trusted ahead of time.
func toshibaExpandProductEntries(entries []ppdEntry) []ppdEntry {
	var out []ppdEntry
	for _, e := range entries {
		products, ok := ReadPPDProducts(e.Path)
		if !ok || len(products) == 0 {
			out = append(out, e)
			continue
		}
		seen := map[string]bool{}
		for _, raw := range products {
			name := toshibaCanonicalModelName(raw)
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, ppdEntry{Path: e.Path, NickName: name})
		}
	}
	return out
}

