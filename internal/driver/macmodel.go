package driver

import (
	"path/filepath"
	"sort"
	"strings"
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

// indexFamilyPackage inspects family's own newest package (path) once,
// returning every PPD it would register, keyed by friendly model name. Tries
// a real installer package first (LocatePkg + packagePPDEntries - the same
// read-only pkgutil --expand-full inspection PackagePPDNickNames already
// does); when path has no .pkg inside at all (LocatePkg's own not-found
// error), falls back to LocateLoosePPDs for the no-installer family shape,
// permanently caching each matched PPD under cacheDir (CachePPDFile) since
// the mounted volume it's read from won't still be mounted at deploy time.
// cacheDir == "" skips the loose-PPD fallback entirely (nowhere to persist a
// copy) rather than erroring - the caller just gets fewer variants indexed.
func indexFamilyPackage(path, family string, tokens []string, cacheDir string) map[string][]MacPPDVariant {
	out := map[string][]MacPPDVariant{}

	pkgPath, pkgCleanup, pkgErr := LocatePkg(path)
	if pkgErr == nil {
		defer pkgCleanup()
		entries, err := packagePPDEntries(pkgPath)
		if err != nil {
			return out
		}
		for _, e := range entries {
			model, _ := stripLanguageSuffix(e.NickName, tokens)
			out[model] = append(out[model], MacPPDVariant{
				Language:    family,
				Label:       model + " (" + languageDisplayName(family) + ")",
				NickName:    e.NickName,
				Filename:    filepath.Base(e.Path),
				PackagePath: path,
			})
		}
		return out
	}
	pkgCleanup()

	if cacheDir == "" {
		return out
	}
	ppdPaths, cleanup, err := LocateLoosePPDs(path)
	defer cleanup()
	if err != nil {
		return out
	}
	for _, p := range ppdPaths {
		nick, ok := ReadPPDNickName(p)
		if !ok {
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
	return out
}

// BuildMacModelIndex builds the model->variant index for every manufacturer
// macFamilyPreference lists, inspecting only each family's own newest
// package (the same one ResolveMacFamily would pick for that family) - never
// every version-folder's own copy - keeping the one-time cost bounded to
// "one pkgutil --expand-full (or one dmg mount) per family", not per
// package. cacheDir is where a loose (no-installer) family's matched PPDs
// get permanently copied (see MacPPDVariant's own doc comment). Best-effort
// throughout: a family whose package fails to expand/mount is silently
// skipped rather than failing the whole build, the same "index what's
// readable, degrade for the rest" spirit BuildMacCatalog itself already has
// for a corrupt/unreadable file.
func BuildMacModelIndex(catalog MacCatalog, cacheDir string) MacModelIndex {
	index := MacModelIndex{}
	for mfg, tokens := range macFamilyPreference {
		packages := catalog.Packages[mfg]
		byModel := map[string][]MacPPDVariant{}
		for _, family := range tokens {
			pkg, ok := newestInFamily(packages, tokens, family)
			if !ok {
				continue
			}
			famCacheDir := ""
			if cacheDir != "" {
				famCacheDir = filepath.Join(cacheDir, mfg, family)
			}
			for model, variants := range indexFamilyPackage(pkg.Path, family, tokens, famCacheDir) {
				byModel[model] = append(byModel[model], variants...)
			}
		}
		if len(byModel) > 0 {
			index[mfg] = byModel
		}
	}
	return index
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
