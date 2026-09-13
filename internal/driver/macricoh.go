package driver

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

