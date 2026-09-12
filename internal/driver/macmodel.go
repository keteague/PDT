package driver

import (
	"os"
	"path/filepath"
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
	"UFRII": "UFR II",
	"PS":    "PostScript",
	"PPD":   "Generic PPD",
}

func languageDisplayName(token string) string {
	if name, ok := macLanguageDisplayNames[token]; ok {
		return name
	}
	return token
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
}

// MacModelIndex: manufacturer -> friendly model name (language suffix
// stripped - see stripLanguageSuffix) -> every language variant available
// for that model. Built once (BuildMacModelIndex, at catalog build/refresh
// time - the same "expensive extraction happens once, every later lookup is
// a plain map read" pattern internal/driver/model.go's own BuildModelIndex
// established for Windows' Kyocera model index), populated only for a
// manufacturer macFamilyPreference lists (Canon today) - a manufacturer with
// just one real driver package has no "which package" ambiguity to resolve
// ahead of install at all; choosePPD's existing post-install NickName match
// (deploy_darwin.go) already handles that case correctly.
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
func isJapanMarketOnly(nickName string) bool {
	return strings.HasSuffix(nickName, " JP")
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
func indexFamilyPackage(pkg MacPackage, family string, tokens []string, cacheDir string) (map[string][]MacPPDVariant, MacFamilyProvenance) {
	out := map[string][]MacPPDVariant{}
	outerRef := MacPackageRef{Path: pkg.Path, ModTime: pkg.ModTime, Size: pkg.Size}

	pkgPath, chain, pkgCleanup, pkgErr := LocatePkgWithChain(pkg.Path)
	if pkgErr == nil {
		defer pkgCleanup()
		entries, subs, err := packagePPDEntries(pkgPath)
		if err != nil {
			return out, MacFamilyProvenance{}
		}
		for _, e := range entries {
			if isJapanMarketOnly(e.NickName) {
				continue
			}
			model, _ := stripLanguageSuffix(e.NickName, tokens)
			out[model] = append(out[model], MacPPDVariant{
				Language:    family,
				Label:       model + " (" + languageDisplayName(family) + ")",
				NickName:    e.NickName,
				Filename:    filepath.Base(e.Path),
				PackagePath: pkg.Path,
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
		Label:              model + " (" + languageDisplayName(v.Language) + ")",
		NickName:           v.NickName,
		Filename:           v.Filename,
		PackagePath:        v.PackagePath,
		LooseCachedPPDPath: v.LooseCachedPPDPath,
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
// macFamilyPreference lists, inspecting only each family's own newest
// package (the same one ResolveMacFamily would pick for that family) - and
// only when it's actually new or changed since the last build (see
// MacManufacturerCatalog.IsCurrent) - never every version-folder's own
// copy, keeping the one-time cost bounded to "one pkgutil --expand (or one
// dmg mount) per family that's actually new", not per package, and not
// even paid again on a later launch once a package has already been
// indexed once.
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
			pkg, ok := newestInFamily(packages, tokens, family)
			if !ok {
				continue
			}

			if cat.IsCurrent(family, pkg) {
				cached := cat.ModelsForFamily(family)
				if cachedVariantFilesExist(cached) {
					for model, variants := range cached {
						for _, v := range variants {
							byModel[model] = append(byModel[model], toMacPPDVariant(model, v))
						}
					}
					continue
				}
			}

			famCacheDir := ""
			if ppdCacheDir != "" {
				famCacheDir = filepath.Join(ppdCacheDir, mfg, family)
			}
			variants, prov := indexFamilyPackage(pkg, family, tokens, famCacheDir)
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
					})
				}
			}
			// Diffed against whatever was there before this family gets
			// overwritten below - only when there was a previous build to
			// compare against at all (a fresh/first-ever index has nothing
			// meaningful to diff; everything would show as "added", which
			// isn't a real change). See DiffModels' own doc comment for why
			// this only reports *what* changed, not whether that's good or
			// bad - that's a human call.
			if _, hadPrevious := cat.Provenance[family]; hadPrevious {
				if added, removed := DiffModels(cat, family, current); len(added) > 0 || len(removed) > 0 {
					changes = append(changes, mfg+" "+languageDisplayName(family)+": "+formatModelDiff(added, removed))
				}
			}
			// Drop this family's previous entries before merging in the
			// fresh ones - a superseded model (no longer in the new
			// package at all) shouldn't linger in the catalog forever.
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
			if cat.Models == nil {
				cat.Models = map[string][]MacCatalogVariant{}
			}
			for model, vs := range current {
				cat.Models[model] = append(cat.Models[model], vs...)
			}
			cat.Provenance[family] = prov
			dirty = true
		}

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
