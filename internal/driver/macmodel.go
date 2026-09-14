package driver

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// macLanguageDisplayNames maps a macFamilyPreference token to how it reads
// in a Driver-combobox label - the same tokens classifyMacFamily already
// classifies real Canon filenames into (macfamily.go), just spelled the way
// a technician would actually read them rather than as a bare filename
// fragment.
var macLanguageDisplayNames = map[string]string{
	"UFRII":   "UFR II",
	"PS":      "PostScript",
	"PPD":     "Generic PPD",
	"Kyocera": "Driver",
	"MacPS":   "Driver",
	"Xerox":   "Driver",
	"Toshiba": "Driver",
	".pkg":    "Driver",
}

func init() {
	// Every real Ricoh download's own PPDs declare a *NickName ending in
	// " PS" (confirmed live across all 9 modern downloads plus the legacy
	// RicohPrinterDrivers bundle) - all genuinely PostScript, unlike
	// Kyocera's single non-language-specific "Driver" bucket. Registered
	// here rather than inline in the literal map above so ricohFamilyTokens
	// (macricoh.go) stays the one place that list needs maintaining.
	for _, tok := range ricohFamilyTokens {
		macLanguageDisplayNames[tok] = "PostScript"
	}
}

func languageDisplayName(token string) string {
	if name, ok := macLanguageDisplayNames[token]; ok {
		return name
	}
	return token
}

// macVariantLabel builds one variant's own Driver-field Label. For every
// manufacturer except Toshiba this is the existing "<model> (<family>[,
// <versionTag>])" shape (versionTag "" - the normal case - omits that
// clause entirely; decorateMultiVersionLabels is the only caller that ever
// passes one, once 2+ coexisting package versions need telling apart).
//
// Toshiba is a deliberate, real exception (2026-09-14, Ken's own explicit
// ask): once its own generic file-level entries are expanded into one entry
// per real *Product model (toshibaExpandProductEntries), composing
// "<model> (<generic-family-name>)" the same way stopped making sense - a
// technician would see e.g. "TOSHIBA e-STUDIO2525AC (Driver)", reading as
// if the driver name just repeats the model, or (once
// toshibaDriverHintFromFilename first fixed that, v0.9.10) "TOSHIBA
// e-STUDIO2525AC (ColorMFP-S2)" - still composed with the model name Ken
// didn't want repeated. What he asked for instead: the Driver field should
// read exactly like macOS's own Printers & Scanners > Printer Details shows
// it for that exact queue - i.e. literally the real underlying PPD's own
// *NickName ("TOSHIBA ColorMFP-S2"), on its own, not combined with the
// model name at all (the model is already shown in its own separate Model
// field). toshibaDriverHintFromFilename derives that exact string from the
// variant's own Filename (mactoshiba.go) - "TOSHIBA_ColorMFP_S2.gz" ->
// "ColorMFP-S2", reassembled here as "TOSHIBA ColorMFP-S2", confirmed live
// to match the real PPD's own *NickName byte-for-byte.
func macVariantLabel(model, family, filename, versionTag string) string {
	if family == "Toshiba" {
		label := "TOSHIBA " + toshibaDriverHintFromFilename(filename)
		if versionTag != "" {
			label += " (" + versionTag + ")"
		}
		return label
	}
	suffix := languageDisplayName(family)
	if versionTag != "" {
		suffix += ", " + versionTag
	}
	return model + " (" + suffix + ")"
}

// MacPPDVariant is one specific PPD available for one specific friendly
// model name, on one specific printer-language package -
// macFamilyPreference's per-manufacturer family tokens (Canon: UFR II/
// PostScript/Generic PPD), confirmed against real Canon downloads to be
// genuinely separate driver packages with overlapping-but-not-identical
// model coverage, not just version variants of one driver (see macfamily.go's
// own doc comment).
//
// PackagePath is set for a family whose PPDs come from a real installer
// package (UFR II, PS - confirmed live both need `installer -pkg` run at
// least once: their PPDs declare *cupsFilter entries pointing at vendor
// filter binaries the package deposits under /Library/Printers/Canon/...,
// not just the PPD itself). Filename is that PPD's own basename, which
// EnsureDriverInstalled's install preserves verbatim under ppdResourcesDir -
// deploy_darwin.go can join the two directly, with no post-install
// diff/fuzzy-match needed at all, unlike the non-catalog fallback path
// (choosePPD) a manufacturer with no model index still uses.
//
// LooseCachedPPDPath is set instead, with PackagePath left "", for a family
// that ships loose PPD files with no installer package at all (Canon's own
// "PPD" bucket, confirmed live: mounts to a folder-per-model tree of plain
// *.PPD.gz files, none declaring a *cupsFilter line - a real Generic-
// PostScript-only PPD, not a proprietary Canon driver, so there's nothing to
// install: a permanent local copy made once at index-build time (see
// CachePPDFile) is all deploy_darwin.go needs to hand straight to
// `lpadmin -P`).
type MacPPDVariant struct {
	Language           string
	Label              string
	NickName           string
	Filename           string
	PackagePath        string
	LooseCachedPPDPath string
	// SourcePackagePath is the outer package (pkg.Path) that produced this
	// variant - always set, unlike PackagePath (see MacCatalogVariant's own
	// doc comment for why PackagePath itself can't be repurposed for this).
	SourcePackagePath string
	// PackageModTime is PackagePath's own file modification time - set
	// whenever PackagePath is, used to tell two coexisting versions of the
	// same family apart (finalizeMultiVersionLabels) and to make sure a
	// blank/ambiguous selection always resolves to the newest one
	// (MacVariantForDeploy). See MacModelIndex's own doc comment for why
	// more than one package can now contribute a variant to the same
	// (model, family) pair at once.
	PackageModTime time.Time
}

// MacModelIndex: manufacturer -> friendly model name (language suffix
// stripped - see stripLanguageSuffix) -> every variant available for that
// model. Built once (BuildMacModelIndex, at catalog build/refresh time - the
// same "expensive extraction happens once, every later lookup is a plain map
// read" pattern internal/driver/model.go's own BuildModelIndex established
// for Windows' Kyocera model index), populated only for a manufacturer
// macFamilyPreference lists - a manufacturer with just one real driver
// package has no "which package" ambiguity to resolve ahead of install at
// all; choosePPD's existing post-install NickName match (deploy_darwin.go)
// already handles that case correctly.
//
// A model's own variant list isn't just one entry per language/family
// anymore (2026-09-13) - if more than one compatible package version sits in
// the Drivers folder for the same family at once (a technician deliberately
// holding a fleet back on an already-validated older version - real parity
// with Windows' own Candidates() decoration for a multi-version driver name,
// see internal/driver/candidates.go), each one gets its own separate variant
// here too, its Label decorated to tell them apart (decorateMultiVersionLabels)
// - a plain, undecorated Label means only one version exists, nothing to
// disambiguate. BuildMacModelIndex's own per-family loop always processes
// packagesInFamily's newest-first order, so within any one (model, family)
// pair the newest package's variant always comes first - the property
// MacVariantForDeploy/MacModelCandidates both rely on to make "no explicit
// pick" always resolve to the latest version, never an arbitrary older one.
type MacModelIndex map[string]map[string][]MacPPDVariant

// stripLanguageSuffix strips a trailing " <token>" (checked against every
// token in tokens) from nickName, returning the bare model name plus which
// token matched ("" when none matched - the case a family's own canonical/
// first-preference package's PPD is expected to hit). Confirmed against a
// real Canon UFR II PPD that its own *NickName carries no language suffix at
// all, only the PS/PPD packages append one: "Canon iR-ADV C5840/5850" vs.
// "Canon iR-ADV C5840/5850 PS" vs. "Canon iR-ADV C5840/5850 PPD".
func stripLanguageSuffix(nickName string, tokens []string) (model, matchedToken string) {
	for _, tok := range tokens {
		suffix := " " + tok
		if strings.HasSuffix(nickName, suffix) {
			return strings.TrimSuffix(nickName, suffix), tok
		}
	}
	return nickName, ""
}

// ricohJapanModelNumberRe matches Ricoh's own second real Japan-market
// convention - a bare "J" glued directly onto the model number itself, no
// space ("RICOH IM 2509J PS", "RICOH MP 3554J PS") - confirmed against a
// real Ricoh catalog build (427 models, 2026-09) that all 16 real
// occurrences are genuinely Japan-only SKUs, with zero false positives
// anywhere else in the same catalog.
var ricohJapanModelNumberRe = regexp.MustCompile(`[0-9]J(\s|$)`)

// isJapanMarketOnly reports whether nickName names a Japan-market-only SKU -
// confirmed against a real Canon catalog build (641 models) that 166 of
// them (26%) end in " JP", and that this isn't just a cosmetic label:
// Canon's own raw NickName puts JP *after* the language token
// ("...iR-ADV C5840/5850 PS JP", not "...PS" then a separate "...JP"
// stripped later), so stripLanguageSuffix's own trailing-token match never
// fires for one of these at all - a JP variant never unifies with its
// non-JP siblings the way stripLanguageSuffix's whole design otherwise
// guarantees. Checked against the raw NickName, before stripLanguageSuffix
// runs, for exactly that reason: "JP" is reliably the very last token
// regardless of whether a language token happens to precede it, so this
// catches every shape Canon's real data actually has. English-market
// deployments have no use for these; filtered out entirely (never indexed
// at all, not just hidden from the UI) rather than carried as catalog
// clutter nothing in this codebase ever resolves a deploy against.
//
// Ricoh's own real data (confirmed 2026-09, 427-model catalog) needed two
// more, neither matching Canon's shape at all: an explicit "JPN" token that
// sits *before* the language suffix, not after ("RICOH MP 1301 JPN PS", not
// "...PS JPN" - 55 real occurrences), and ricohJapanModelNumberRe's own
// glued-on-"J" convention (16 real occurrences). Checked as plain substring/
// suffix matches against the raw NickName, same as Canon's own check.
func isJapanMarketOnly(nickName string) bool {
	if strings.HasSuffix(nickName, " JP") {
		return true
	}
	if strings.Contains(nickName, " JPN ") || strings.HasSuffix(nickName, " JPN") {
		return true
	}
	return ricohJapanModelNumberRe.MatchString(nickName)
}

// indexFamilyPackage inspects family's own newest package (pkg) once,
// returning every PPD it would register (keyed by friendly model name)
// alongside the full provenance chain that produced them - see
// MacFamilyProvenance's own doc comment. Tries a real installer package
// first (LocatePkgWithChain + packagePPDEntries); when pkg has no .pkg
// inside at all (LocatePkg's own not-found error), falls back to
// LocateLoosePPDs for the no-installer family shape, permanently caching
// each matched PPD under cacheDir (CachePPDFile) since the mounted volume
// it's read from won't still be mounted at deploy time. cacheDir == ""
// skips the loose-PPD fallback entirely (nowhere to persist a copy) rather
// than erroring - the caller just gets fewer variants indexed.
// macSubPackageRestrictor returns indexFamilyPackage's own sub-package
// restrictor for a manufacturer, or nil for every manufacturer whose whole
// Distribution shape doesn't need one (everyone except Kyocera today - see
// kyoceraRestrictSubPackages' own doc comment for why Kyocera specifically
// needs one: its "Web Build" Distribution ships the identical PPD set
// duplicated across 3 sub-packages).
func macSubPackageRestrictor(manufacturer string) func(expandDir string) (map[string]bool, bool) {
	if manufacturer == "Kyocera" {
		return kyoceraRestrictSubPackages
	}
	return nil
}

func indexFamilyPackage(pkg MacPackage, family string, tokens []string, cacheDir string, restrict func(expandDir string) (map[string]bool, bool), ppdFallback ppdExtractionFallback, expand func([]ppdEntry) []ppdEntry) (map[string][]MacPPDVariant, MacFamilyProvenance) {
	out := map[string][]MacPPDVariant{}
	outerRef := MacPackageRef{Path: pkg.Path, ModTime: pkg.ModTime, Size: pkg.Size}

	pkgPath, chain, pkgCleanup, pkgErr := LocatePkgWithChain(pkg.Path)
	if pkgErr == nil {
		defer pkgCleanup()
		// expand (Toshiba only today - see macPPDEntryExpander/
		// toshibaExpandProductEntries) turns each file-level entry (one per
		// generic PDL-variant PPD, *NickName staying just as generic) into
		// one entry per real model its own *Product lines declare - MUST
		// happen inside packagePPDEntriesFilteredFallback itself, before its
		// own tmpDir is removed (confirmed live, 2026-09-13, as a real bug:
		// expanding here instead, after that function already returned,
		// always found zero *Product lines, since every entry's own Path
		// pointed at an already-deleted temp file by then). Everything
		// downstream (Japan-market filtering, model-map keying, provenance)
		// treats the expanded entries exactly like any other manufacturer's
		// own real per-model PPDs.
		entries, subs, err := packagePPDEntriesFilteredFallback(pkgPath, restrict, ppdFallback, expand)
		if err != nil {
			return out, MacFamilyProvenance{}
		}
		for _, e := range entries {
			if isJapanMarketOnly(e.NickName) {
				continue
			}
			model, _ := stripLanguageSuffix(e.NickName, tokens)
			filename := filepath.Base(e.Path)
			out[model] = append(out[model], MacPPDVariant{
				Language:          family,
				Label:             macVariantLabel(model, family, filename, ""),
				NickName:          e.NickName,
				Filename:          filename,
				PackagePath:       pkg.Path,
				SourcePackagePath: pkg.Path,
				PackageModTime:    pkg.ModTime,
			})
		}
		if len(out) == 0 {
			return out, MacFamilyProvenance{}
		}
		refs := []MacPackageRef{outerRef}
		for _, p := range chain[1:] { // chain[0] duplicates pkg.Path/outerRef
			refs = append(refs, MacPackageRef{Path: p})
		}
		for _, s := range subs {
			refs = append(refs, MacPackageRef{Path: s.Name, Version: s.Version})
		}
		return out, MacFamilyProvenance{Chain: refs, IndexedAt: time.Now()}
	}
	pkgCleanup()

	if cacheDir == "" {
		return out, MacFamilyProvenance{}
	}
	ppdPaths, cleanup, err := LocateLoosePPDs(pkg.Path)
	defer cleanup()
	if err != nil {
		return out, MacFamilyProvenance{}
	}
	for _, p := range ppdPaths {
		nick, ok := ReadPPDNickName(p)
		if !ok || isJapanMarketOnly(nick) {
			continue
		}
		model, _ := stripLanguageSuffix(nick, tokens)
		filename := filepath.Base(p)
		cached, err := CachePPDFile(p, cacheDir, filename)
		if err != nil {
			continue
		}
		out[model] = append(out[model], MacPPDVariant{
			Language:           family,
			Label:              model + " (" + languageDisplayName(family) + ")",
			NickName:           nick,
			Filename:           filename,
			LooseCachedPPDPath: cached,
			SourcePackagePath:  pkg.Path,
			PackageModTime:     pkg.ModTime,
		})
	}
	if len(out) == 0 {
		return out, MacFamilyProvenance{}
	}
	return out, MacFamilyProvenance{Chain: []MacPackageRef{outerRef}, IndexedAt: time.Now()}
}

// toMacPPDVariant converts a MacCatalogVariant (catalog.json's own
// persisted shape - see MacManufacturerCatalog) back to the in-memory
// MacPPDVariant BuildMacModelIndex returns, for a family a cached catalog
// entry is being reused for (IsCurrent matched, no re-inspection needed).
// Label is recomputed here rather than persisted - see MacCatalogVariant's
// own doc comment for why.
func toMacPPDVariant(model string, v MacCatalogVariant) MacPPDVariant {
	return MacPPDVariant{
		Language:           v.Language,
		Label:              macVariantLabel(model, v.Language, v.Filename, ""),
		NickName:           v.NickName,
		Filename:           v.Filename,
		PackagePath:        v.PackagePath,
		LooseCachedPPDPath: v.LooseCachedPPDPath,
		SourcePackagePath:  v.SourcePackagePath,
		PackageModTime:     v.PackageModTime,
	}
}

// packageCacheKey turns a package's own path into a short, filesystem-safe
// directory-name fragment - gives each package within a family its own
// loose-PPD cache subdirectory now that more than one package can be
// "current" for the same family at once (see MacModelIndex's own doc
// comment), so two coexisting versions that happen to share a model's own
// PPD filename can't silently overwrite each other's permanently-cached
// copy (CachePPDFile). Keeps the real basename as a human-readable prefix
// (easier to spot which cache directory belongs to which download when
// looking at the filesystem directly) plus a short hash for guaranteed
// uniqueness, rather than trying to sanitize a full path into a directory
// name.
func packageCacheKey(path string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(path))
	return fmt.Sprintf("%s-%08x", filepath.Base(path), h.Sum32())
}

// packageVersionTag renders a short, cheap-to-compute "which download, and
// when" tag for a decorated multi-version Driver-dropdown label - the bare
// filename (no extension) plus the file's own modification date. Not
// PackageLabel (macmount.go): that one's first attempt is a real
// `pkgutil --expand-full` call, confirmed elsewhere in this codebase to be
// genuinely expensive (packagePPDEntries' own doc comment - 6.4s/255MB for a
// real Canon package) - far too costly to pay for every variant on every
// catalog build just to decorate a label, when the bare filename is already
// the same "honest signal" ResolveMac itself trusts for ordering (see that
// function's own doc comment: macOS has no reliable package-level version
// field at all).
func packageVersionTag(packagePath string, modTime time.Time) string {
	base := filepath.Base(packagePath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if modTime.IsZero() {
		return base
	}
	return base + " - " + modTime.Format("2006-01-02")
}

// decorateMultiVersionLabels mutates byModel in place: for any (model,
// family) pair with more than one variant - two coexisting package versions
// both registering a PPD for the same model - every one of those variants'
// own Label gets packageVersionTag appended, so a technician can tell them
// apart in the Driver dropdown (mirrors Windows' own Candidates() decoration
// for a multi-version driver name). A model/family pair with only one
// variant is untouched - its Label stays exactly as plain as it always was.
// Variants are left in packagesInFamily's own newest-first order (already
// guaranteed by BuildMacModelIndex's own per-family loop), so the first
// (newest) one is always still first after decoration too - see
// MacVariantForDeploy's own doc comment for why that ordering is what makes
// "no explicit pick" always resolve to the latest version.
func decorateMultiVersionLabels(byModel map[string][]MacPPDVariant) {
	for model, variants := range byModel {
		byLanguage := map[string][]int{}
		for i, v := range variants {
			byLanguage[v.Language] = append(byLanguage[v.Language], i)
		}
		for _, idxs := range byLanguage {
			if len(idxs) < 2 {
				continue
			}
			for _, i := range idxs {
				v := &variants[i]
				v.Label = macVariantLabel(model, v.Language, v.Filename, packageVersionTag(v.PackagePath, v.PackageModTime))
			}
		}
		// variants shares byModel[model]'s own backing array (both came from
		// the same `for model, variants := range byModel` above) - the
		// in-place edits above are already reflected there, no reassignment
		// needed.
	}
}

// cachedVariantFilesExist reports whether every LooseCachedPPDPath in
// variants still exists on disk - confirmed necessary for correctness, not
// just belt-and-suspenders: catalog.json travels with a portable Drivers
// folder (see MacManufacturerCatalog's own doc comment), but the PPD cache
// it references does not (installedAppDataDir - per-machine, deliberately,
// since a flash drive is normally write-protected in the field - see
// driversfolder.go's own ensureDriversScaffold doc comment). A fresh
// machine reading someone else's already-built catalog.json needs this
// check to notice its own local cache doesn't have the file yet and
// re-inspect (cheap now - see packagePPDEntries) rather than reuse a
// LooseCachedPPDPath that doesn't resolve to anything on this machine.
// Irrelevant (always true) for an installer-backed family, which has no
// LooseCachedPPDPath entries at all.
func cachedVariantFilesExist(variants map[string][]MacCatalogVariant) bool {
	for _, vs := range variants {
		for _, v := range vs {
			if v.LooseCachedPPDPath == "" {
				continue
			}
			if _, err := os.Stat(v.LooseCachedPPDPath); err != nil {
				return false
			}
		}
	}
	return true
}

// BuildMacModelIndex builds the model->variant index for every manufacturer
// macFamilyPreference lists, inspecting every package classifyMacFamily
// assigns to each family (packagesInFamily) - not just the newest - so more
// than one compatible version of the same family can sit in the Drivers
// folder at once and still each be individually indexed and selectable in
// the Driver dropdown (a technician deliberately holding a printer fleet
// back on an already-validated older version, the same real capability
// Windows' own Candidates() decoration already gives Kyocera - see
// MacModelIndex's own doc comment). Each package is still only ever
// re-inspected when it's actually new or changed since the last build (see
// MacManufacturerCatalog.IsCurrent/IsCurrentForPackage) - the newest
// package's own provenance still lives in cat.Provenance (unchanged from
// before this existed), every other, older-but-kept package's own
// provenance lives in cat.ExtraProvenance instead - keeping the one-time
// cost bounded to "one pkgutil --expand (or one dmg mount) per package
// that's actually new," not paid again on a later launch once a package has
// already been indexed once, regardless of how many coexisting versions
// there are.
//
// A model with more than one variant for the *same* family (two coexisting
// versions both registering a PPD for it) gets each of those variants'
// own Label decorated with which package produced it (packageVersionTag) -
// a single-version model's Label stays exactly as plain as it always was.
// Auto-selection (MacVariantForDeploy, MacModelCandidates) always still
// resolves to the newest one absent an explicit pick: every per-family loop
// below processes packagesInFamily's own newest-first order, so the newest
// package's own variants are always appended to byModel[model] before any
// older package's - Ken's own explicit requirement (2026-09-13).
//
// macRoot is the Drivers/macOS directory - each manufacturer with real
// per-model data gets its own catalog file there (inside that
// manufacturer's own subfolder - MacCatalogFileName), so it travels with a
// portable Drivers folder and can be deleted per-manufacturer to force a
// full re-index of just that one. persist controls whether an updated
// catalog actually gets written back to macRoot at all - false when this
// exact running copy is on a removable drive itself (see
// app_darwin.go's loadCatalog and flashdrive.IsRemovableDrive): a
// technician's laptop is where catalog.json gets created/updated, a flash
// drive plugged into a different machine only ever reads whatever's
// already there. ppdCacheDir is where a loose (no-installer) family's
// matched PPDs get permanently copied (see MacPPDVariant's own doc
// comment) - always the per-machine installedAppDataDir, regardless of
// persist, since deploy needs a real file to exist on whichever machine is
// actually running right now.
//
// Best-effort throughout: a family whose package fails to expand/mount is
// silently skipped rather than failing the whole build, the same "index
// what's readable, degrade for the rest" spirit BuildMacCatalog itself
// already has for a corrupt/unreadable file.
func BuildMacModelIndex(catalog MacCatalog, macRoot, ppdCacheDir string, persist bool) (MacModelIndex, []string) {
	index := MacModelIndex{}
	var changes []string
	for mfg, tokens := range macFamilyPreference {
		packages := catalog.Packages[mfg]
		mfgFolder := strings.ReplaceAll(mfg, " ", "")
		catalogPath := filepath.Join(macRoot, mfgFolder, MacCatalogFileName(mfg))
		cat := LoadMacManufacturerCatalog(catalogPath)
		dirty := false

		byModel := map[string][]MacPPDVariant{}
		for _, family := range tokens {
			all := packagesInFamily(packages, tokens, family)
			if len(all) == 0 {
				// This family's last package(s) disappeared entirely -
				// deleted, or moved to an Archive folder scanMacPackages
				// already skips (issue #5, confirmed live: moving a real
				// Ricoh package to Archive correctly dropped it from the
				// live in-memory index, but its own entries sat in
				// cat.Models forever, since nothing reached the pruning
				// logic below when there was no package left to
				// (re-)index at all). Reuses DiffModels against an empty
				// "current" map - every model this family previously had
				// reports as removed, the same diff mechanism a real
				// content change already produces.
				if _, hadPrevious := cat.Provenance[family]; hadPrevious {
					if _, removed := DiffModels(cat, family, map[string][]MacCatalogVariant{}); len(removed) > 0 {
						changes = append(changes, mfg+" "+languageDisplayName(family)+": "+formatModelDiff(nil, removed))
					}
					for model, vs := range cat.Models {
						kept := vs[:0]
						for _, v := range vs {
							if v.Language != family {
								kept = append(kept, v)
							}
						}
						if len(kept) == 0 {
							delete(cat.Models, model)
						} else {
							cat.Models[model] = kept
						}
					}
					delete(cat.Provenance, family)
					delete(cat.ExtraProvenance, family)
					dirty = true
				}
				continue
			}

			for i, pkg := range all {
				newest := i == 0 // packagesInFamily is newest-first

				var cached bool
				if newest {
					cached = cat.IsCurrent(family, pkg)
				} else {
					cached = cat.IsCurrentForPackage(family, pkg)
				}
				if cached {
					cachedModels := cat.ModelsForFamilyPackage(family, pkg.Path)
					// len(cachedModels) > 0 is required, not just
					// cachedVariantFilesExist (which is vacuously true for an
					// empty map) - confirmed live as a real bug: a catalog
					// written before SourcePackagePath existed (any
					// pre-2026-09-13 catalog.<mfg>.json) has every entry's own
					// SourcePackagePath empty, so this lookup always came back
					// empty for it, and an empty-but-vacuously-"valid" cache
					// hit was silently trusted as "nothing to index" instead
					// of falling through to a real reindex - Kyocera/Ricoh
					// disappeared from the Model dropdown entirely as a
					// result, even though their real packages were untouched.
					if len(cachedModels) > 0 && cachedVariantFilesExist(cachedModels) {
						for model, variants := range cachedModels {
							for _, v := range variants {
								byModel[model] = append(byModel[model], toMacPPDVariant(model, v))
							}
						}
						continue
					}
				}

				famCacheDir := ""
				if ppdCacheDir != "" {
					// Each package within a family gets its own cache
					// subdirectory (packageCacheKey) - without this, two
					// coexisting versions that happen to share a model's own
					// loose-PPD filename would silently overwrite each
					// other's permanently-cached copy (CachePPDFile), which
					// would defeat the whole point of keeping an older
					// version reachable.
					famCacheDir = filepath.Join(ppdCacheDir, mfg, family, packageCacheKey(pkg.Path))
				}
				variants, prov := indexFamilyPackage(pkg, family, tokens, famCacheDir, macSubPackageRestrictor(mfg), macSubPackagePPDFallback(mfg), macPPDEntryExpander(mfg))
				if len(variants) == 0 {
					continue
				}
				for model, vs := range variants {
					byModel[model] = append(byModel[model], vs...)
				}

				current := map[string][]MacCatalogVariant{}
				for model, vs := range variants {
					for _, v := range vs {
						current[model] = append(current[model], MacCatalogVariant{
							Language: v.Language, NickName: v.NickName, Filename: v.Filename,
							PackagePath: v.PackagePath, LooseCachedPPDPath: v.LooseCachedPPDPath,
							SourcePackagePath: v.SourcePackagePath, PackageModTime: v.PackageModTime,
						})
					}
				}
				// Diffed against whatever was there before this *package's*
				// own entries get overwritten below - only when there was a
				// previous build to compare against at all (a fresh/
				// first-ever index has nothing meaningful to diff; everything
				// would show as "added", which isn't a real change). See
				// DiffModels' own doc comment for why this only reports
				// *what* changed, not whether that's good or bad - that's a
				// human call. Only run for the newest package - DiffModels
				// itself compares against ModelsForFamily's whole-family
				// view, which would otherwise also see (and misreport
				// against) any older, unrelated package's own entries.
				if newest {
					if _, hadPrevious := cat.Provenance[family]; hadPrevious {
						if added, removed := DiffModels(cat, family, current); len(added) > 0 || len(removed) > 0 {
							changes = append(changes, mfg+" "+languageDisplayName(family)+": "+formatModelDiff(added, removed))
						}
					}
				}
				// Drop this *specific package's* previous entries (family
				// AND packagePath both) before merging in the fresh ones -
				// not the whole family, since another coexisting package's
				// own already-cached entries for the same family must
				// survive this untouched. A legacy entry with no
				// SourcePackagePath at all (written before that field
				// existed) is only ever treated as "belonging" to the
				// *newest* package being reindexed, never an older one - the
				// old single-version format only ever cached one package per
				// family in the first place, so that's the only package a
				// legacy entry could possibly have come from. Without this,
				// reindexing after the len(cachedModels)>0 fix above would
				// add fresh, correctly-tagged entries *alongside* the old
				// untagged ones instead of replacing them, leaving orphaned
				// duplicates in the catalog file forever.
				for model, vs := range cat.Models {
					kept := vs[:0]
					for _, v := range vs {
						isThisPackage := v.SourcePackagePath == pkg.Path || (newest && v.SourcePackagePath == "")
						if !(v.Language == family && isThisPackage) {
							kept = append(kept, v)
						}
					}
					if len(kept) == 0 {
						delete(cat.Models, model)
					} else {
						cat.Models[model] = kept
					}
				}
				if cat.Models == nil {
					cat.Models = map[string][]MacCatalogVariant{}
				}
				for model, vs := range current {
					cat.Models[model] = append(cat.Models[model], vs...)
				}
				if newest {
					cat.Provenance[family] = prov
				} else {
					if cat.ExtraProvenance[family] == nil {
						cat.ExtraProvenance[family] = map[string]MacFamilyProvenance{}
					}
					cat.ExtraProvenance[family][pkg.Path] = prov
				}
				dirty = true
			}

			// Prune any entry/provenance left over for a package that's no
			// longer part of this family's *current* package set at all -
			// confirmed live as a real, immediate bug (not a hypothetical):
			// a package that used to be individually tracked (before
			// packagesInFamily's own (basename, size) deduplication existed
			// earlier today) but has since collapsed away into another
			// package's own representative entry never gets revisited by
			// the loop above at all once it's gone from `all` - without
			// this, its own stale cat.Models entries and
			// cat.ExtraProvenance key sit there forever, showing up as
			// bogus duplicate "versions" of the exact same real download
			// (confirmed live: a real Kyocera catalog carried 10 duplicate
			// entries per model, one per OS-version folder, after the
			// dedup fix alone).
			currentPaths := make(map[string]bool, len(all))
			for _, pkg := range all {
				currentPaths[pkg.Path] = true
			}
			for model, vs := range cat.Models {
				kept := vs[:0]
				for _, v := range vs {
					if v.Language != family || currentPaths[v.SourcePackagePath] {
						kept = append(kept, v)
					} else {
						dirty = true
					}
				}
				if len(kept) == 0 {
					delete(cat.Models, model)
				} else {
					cat.Models[model] = kept
				}
			}
			for path := range cat.ExtraProvenance[family] {
				if !currentPaths[path] {
					delete(cat.ExtraProvenance[family], path)
					dirty = true
				}
			}
		}

		decorateMultiVersionLabels(byModel)

		if len(byModel) > 0 {
			index[mfg] = byModel
		}
		if dirty && persist {
			_ = SaveMacManufacturerCatalog(catalogPath, cat)
		}
	}
	return index, changes
}

// formatModelDiff renders added/removed model-name lists as one short,
// human-readable summary - "+3 model(s): A, B, C" style, truncated past a
// handful of names so one big driver-family replacement doesn't produce an
// unreadable wall of text in the log.
func formatModelDiff(added, removed []string) string {
	sort.Strings(added)
	sort.Strings(removed)
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "+"+strconv.Itoa(len(added))+" new: "+truncatedNameList(added))
	}
	if len(removed) > 0 {
		parts = append(parts, "-"+strconv.Itoa(len(removed))+" no longer present: "+truncatedNameList(removed))
	}
	return strings.Join(parts, ", ")
}

// truncatedNameList joins names with ", ", capped at 5 - the rest
// summarized as "+N more" rather than printed - see formatModelDiff.
func truncatedNameList(names []string) string {
	const max = 5
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:max], ", ") + " (+" + strconv.Itoa(len(names)-max) + " more)"
}

// lookupMacModel resolves model (as typed/committed, possibly differing in
// case/spacing from the index's own key) to the index's own canonical key
// and its variants - exact match first, falling back to the same
// case/space-insensitive fold catalog.go's own manufacturer-name matching
// uses (foldMatchIgnoringSpaces), since a technician typing straight into
// the Model combobox isn't guaranteed to hit the exact capitalization/
// spacing MacModels itself returned.
func lookupMacModel(index MacModelIndex, manufacturer, model string) (canonicalModel string, variants []MacPPDVariant, ok bool) {
	models := index[manufacturer]
	if models == nil || model == "" {
		return "", nil, false
	}
	if v, ok := models[model]; ok {
		return model, v, true
	}
	for key, v := range models {
		if foldMatchIgnoringSpaces(key, model) {
			return key, v, true
		}
	}
	return "", nil, false
}

// MacModels lists manufacturer's known models from index (see
// BuildMacModelIndex - Canon only today), ranked by FuzzyMatchScore against
// filterText the same way driver.Models ranks Kyocera's own Windows model
// index. []string{}, not nil, for a manufacturer with no model data at all -
// see driver.Models' own doc comment for why that distinction matters across
// the Wails JSON bridge.
func MacModels(index MacModelIndex, manufacturer, filterText string) []string {
	byModel := index[manufacturer]
	type scored struct {
		name  string
		score int
	}
	candidates := make([]scored, 0, len(byModel))
	for m := range byModel {
		score := 0
		if filterText != "" {
			if score = FuzzyMatchScore(m, filterText); score < 0 {
				continue
			}
		}
		candidates = append(candidates, scored{m, score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].name < candidates[j].name
	})
	models := make([]string, len(candidates))
	for i, c := range candidates {
		models[i] = c.name
	}
	return models
}

// MacModelCandidates lists a model's own language-variant labels (see
// MacPPDVariant.Label), ranked by FuzzyMatchScore against filterText -
// darwin's own DriverCandidates (drivercatalog_darwin.go) calls this once
// model resolves to a real MacModelIndex entry, the mac equivalent of
// Windows' Candidates narrowing by Kyocera's own model index. []string{},
// not nil, when model doesn't resolve to any index entry (same JSON-bridge
// reasoning as MacModels). Preference-ordered (macFamilyPreference's own
// token order - UFR II before PostScript before Generic PPD for Canon) when
// filterText is empty, matching ResolveMacFamily's own preference order
// rather than an arbitrary map iteration order.
//
// A blank model lists every variant of every model index has for
// manufacturer, not nothing - confirmed live as a real, previously-shipped
// bug against a real Canon download (641 real models indexed successfully,
// MacModelCandidates still returning empty for a blank Model because
// lookupMacModel's own single-model lookup - the right behavior for
// MacVariantForDeploy's exact-match use below, left untouched - treats a
// blank model as "nothing to look up" rather than "everything"). Windows'
// own Candidates (internal/driver/candidates.go) already does the
// un-narrowed "list every driver name" thing when its own model is blank;
// DriverCandidates' own doc comment claims this mirrors that same two-step
// Model-narrows-Driver behavior, which requires the blank case to behave the
// same way on both platforms - without this, DriverCandidates' own
// ResolveMac fallback silently took over instead, collapsing the whole
// dropdown down to Manufacturer's own single guessed package label the
// moment Model wasn't narrowed down yet, on every real Canon download.
func MacModelCandidates(index MacModelIndex, manufacturer, model, filterText string) []string {
	var variants []MacPPDVariant
	if model == "" {
		byModel := index[manufacturer]
		if len(byModel) == 0 {
			return []string{}
		}
		modelNames := make([]string, 0, len(byModel))
		for m := range byModel {
			modelNames = append(modelNames, m)
		}
		sort.Strings(modelNames)
		for _, m := range modelNames {
			variants = append(variants, byModel[m]...)
		}
	} else {
		_, v, ok := lookupMacModel(index, manufacturer, model)
		if !ok {
			return []string{}
		}
		variants = v
	}
	tokens := macFamilyPreference[manufacturer]
	rank := make(map[string]int, len(tokens))
	for i, t := range tokens {
		rank[t] = i
	}
	sorted := append([]MacPPDVariant(nil), variants...)
	sort.SliceStable(sorted, func(i, j int) bool { return rank[sorted[i].Language] < rank[sorted[j].Language] })

	if filterText == "" {
		out := make([]string, len(sorted))
		for i, v := range sorted {
			out[i] = v.Label
		}
		return out
	}
	type scored struct {
		label string
		score int
	}
	var results []scored
	for _, v := range sorted {
		s := FuzzyMatchScore(v.Label, filterText)
		if s >= 0 {
			results = append(results, scored{v.Label, s})
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.label
	}
	return out
}

// MacVariantForDeploy resolves (manufacturer, model, driverLabel) - a grid
// row's own committed selection - to the exact MacPPDVariant to deploy,
// straight from the index built once at catalog build/refresh time: no
// re-inspection of any package at deploy time at all. driverLabel, when it
// exactly matches one of model's own variant labels (the technician actually
// picked one from the Driver dropdown, the most authoritative signal
// available - same "an explicit selection beats a guess" rule
// OpenPrintingPPDByLabel already applies on the non-catalog fallback path),
// wins outright; otherwise falls back to whichever variant matches the
// manufacturer's own family-preference order first (mirrors
// ResolveMacFamily's own preference order for the "Model chosen, language
// not decided" case). ok is false whenever model doesn't resolve to any
// index entry at all - the caller falls back to ResolveMacFamily/choosePPD.
//
// When a family has more than one coexisting package version indexed (see
// MacModelIndex's own doc comment), "matches the family-preference order
// first" also means "the newest one" without needing any extra logic here -
// BuildMacModelIndex's own per-family loop always appends a family's newest
// package's variants to variants before any older package's, so the first
// v.Language == tok match found below is guaranteed to already be the
// latest version. Ken's own explicit requirement (2026-09-13): a blank/
// ambiguous selection must always resolve to the newest version, never an
// older one left around for manual selection.
func MacVariantForDeploy(index MacModelIndex, manufacturer, model, driverLabel string) (MacPPDVariant, bool) {
	_, variants, ok := lookupMacModel(index, manufacturer, model)
	if !ok || len(variants) == 0 {
		return MacPPDVariant{}, false
	}
	for _, v := range variants {
		if v.Label == driverLabel {
			return v, true
		}
	}
	tokens := macFamilyPreference[manufacturer]
	for _, tok := range tokens {
		for _, v := range variants {
			if v.Language == tok {
				return v, true
			}
		}
	}
	return variants[0], true
}
