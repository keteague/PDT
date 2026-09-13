package driver

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

// ricohFamilyTokens: Ricoh's own macFamilyPreference entry, in preference
// order - confirmed against 9 real macOS downloads (2026-09-12) that Ricoh's
// own shape is genuinely different from Canon (one driver line, periodically
// superseded) or Kyocera (one current "Web Build"): Ricoh ships many small,
// independent downloads side by side, each covering its own small, disjoint
// set of models with no version relationship to any other -
// "IM_C300_C400_LIO_1.5.0.0.dmg" isn't a newer build superseding
// "IM_C6500_C8000_LIO_1.3.0.0.dmg", it's a completely different printer
// family that happens to also be current. Each token is a distinguishing,
// collision-free substring of exactly one real filename (verified
// pairwise - no token is ever a substring of another), the same
// classifyMacFamily mechanism Canon's UFRII/PS/PPD tokens already use.
//
// Confirmed live that a download's own filename systematically UNDERSELLS
// its real model coverage - "Ricoh_IM_2500_3500_4000_LIO" reads as 3 models,
// its own real ppds.pkg Payload actually registers 9
// (2500/2509J/3000/3009J/3500/3509J/4000/5000/6000). Same as every other
// manufacturer here, the model list itself always comes from the real PPD
// *NickName fields (indexFamilyPackage), never guessed from a token - these
// tokens exist purely to classify *which file* a token belongs to, nothing
// more.
//
// "RicohPrinterDrivers" is a different, older shape entirely - a legacy,
// Apple Software-Update-distributed bundle (identifier
// "com.apple.pkg.RicohPrinterDrivers", 356 real PPDs covering Ricoh's older
// Aficio/imagio/IPSiO/SP-branded models, confirmed via *NickName, not
// filename) - listed last (lowest preference) since a current vendor
// download should win over it for any model both happen to cover, though
// none were found to overlap in practice.
var ricohFamilyTokens = []string{
	"IM_C3000_C3500_C4500",
	"IM_2500_3500_4000",
	"IM_3010_3510_4510",
	"IM_370_460",
	"IM_C300_C400",
	"IM_C3010_C3510_C4510",
	"IM_C6500_C8000",
	"IM_C6510_C8010",
	"Vol5_EXP",
	"RicohPrinterDrivers",
}

// ricohPPDResourcesInstallLocation is the exact install-location every real
// modern Ricoh download's own baseline PPD-only sub-package declares
// (confirmed against all 9 real "Web Build"-style downloads inspected
// 2026-09-12: identifier "com.RICOH.print.<model-group>.ppds.pkg" every
// time, e.g. "com.RICOH.print.IM_C300_C400.ppds.pkg") - the cheap,
// manufacturer-agnostic signal ricohPPDExtractionFallback uses to recognize
// "every file in this sub-package's own Payload is a real PPD" without
// needing to hard-code Ricoh's own sub-package naming convention at all.
// None of these real PPDs carry any file extension whatsoever (confirmed
// live via `file`: genuine "PPD file, version 4.3" content under names like
// "RICOH IM C3000") - the fast, extension-based cpio glob every other
// manufacturer's real PPDs already match can never find them.
const ricohPPDResourcesInstallLocation = "/Library/Printers/PPDs/Contents/Resources/"

// ricohLegacyPPDPathFragment is the path fragment every real PPD in the
// legacy "RicohPrinterDrivers.pkg" bundle lives under - that package's own
// PackageInfo declares no install-location at all (its Payload bakes the
// real destination into each entry's own relative path instead, e.g.
// "./Library/Printers/PPDs/Contents/Resources/RICOH Aficio 3224C.gz" -
// confirmed live), so ricohPPDExtractionFallback falls back to finding PPDs
// by path instead of by declared destination.
const ricohLegacyPPDPathFragment = "/PPDs/Contents/Resources/"

// macSubPackagePPDFallback returns indexFamilyPackage's own content-based
// PPD-extraction fallback for a manufacturer, or nil for every manufacturer
// whose real PPDs are already found correctly by the fast, extension-based
// glob (everyone except Ricoh today) - see ppdExtractionFallback's own doc
// comment (macppd.go) for the exact contract.
func macSubPackagePPDFallback(manufacturer string) ppdExtractionFallback {
	if manufacturer == "Ricoh" {
		return ricohPPDExtractionFallback
	}
	return nil
}

// ricohPPDExtractionFallback is tried only once the fast, extension-based
// cpio glob finds nothing in a given sub-package's own Payload. Two real
// shapes, confirmed live: a modern download's own baseline PPD sub-package
// declares install-location itself (extract its whole Payload - already
// known, from that declaration, to contain nothing but real PPDs); the
// legacy bundle doesn't declare one at all (fall back to finding PPDs by
// path instead, extracting only the matched entries - never the whole
// Payload, which would also pull down hundreds of unrelated driver-
// framework/PDE-plugin files sharing the very same Payload). Either way,
// nothing extracted here is trusted by name alone -
// packagePPDEntriesFilteredFallback's own caller content-sniffs every
// non-suffix-matched file (looksLikeRealPPD) before accepting it.
func ricohPPDExtractionFallback(pkgDir, payloadPath, destDir string) bool {
	if loc, ok := readPackageInfoInstallLocation(filepath.Join(pkgDir, "PackageInfo")); ok && loc == ricohPPDResourcesInstallLocation {
		extractAllFromPayload(payloadPath, destDir)
	} else {
		extractPathContainingFromPayload(payloadPath, ricohLegacyPPDPathFragment, destDir)
	}
	removeNonPPDFiles(destDir)
	return dirHasAnyFile(destDir)
}

// RicohPPDPathForDefaults extracts just ppdFilename out of the real,
// already-located driver package at pkgPath into a caller-owned temp
// directory (removed via the returned cleanup once the caller is done
// reading it) - Ricoh's own batched deploy plan (planRicohBatchRow,
// canonbatch_darwin.go) needs this to compute print defaults ahead of its
// one privileged call, since - unlike Canon/Kyocera - Ricoh's own install is
// never selective: a plain full `installer -pkg` run places every real PPD
// at once, so there's no already-staged copy of just this one model to read
// defaults from the way Canon/Kyocera's own selective extraction leaves
// behind as a side effect. Reuses the exact same detection logic proven
// live in BuildMacModelIndex (extractPPDsFromExpandedPkgFiltered plus the
// Ricoh content-based fallback) against a temp dir this function owns
// outright, rather than one gone by the time packagePPDEntriesFiltered's
// own caller sees it.
func RicohPPDPathForDefaults(pkgPath, ppdFilename string) (ppdPath string, cleanup func(), err error) {
	noop := func() {}
	tmpDir, err := os.MkdirTemp("", "pdt-ricoh-defaults-*")
	if err != nil {
		return "", noop, err
	}
	cleanup = func() { os.RemoveAll(tmpDir) }

	expandDir := filepath.Join(tmpDir, "expand")
	if err := exec.Command("pkgutil", "--expand", pkgPath, expandDir).Run(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("expanding %s: %w", pkgPath, err)
	}
	extractDir := filepath.Join(tmpDir, "ppds")
	extractPPDsFromExpandedPkgFiltered(expandDir, extractDir, nil, macSubPackagePPDFallback("Ricoh"))

	var found string
	_ = filepath.WalkDir(extractDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(path) == ppdFilename {
			found = path
		}
		return nil
	})
	if found == "" {
		cleanup()
		return "", noop, fmt.Errorf("could not find %q in %s's own real payload", ppdFilename, pkgPath)
	}
	return found, cleanup, nil
}
