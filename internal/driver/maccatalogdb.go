package driver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MacManufacturerCatalog is the persistent, on-disk record of every
// model/PPD one family-preference manufacturer's model index
// (BuildMacModelIndex) has ever indexed from a real driver package, plus
// exactly which package (and its own parent chain, for Canon's real nested
// .dmg/.pkg/sub-package structure) produced it, and when. Confirmed live
// that mounting + inspecting a real vendor package is genuinely expensive
// even after the pkgutil --expand-full -> --expand/cpio fix
// (packagePPDEntries' own doc comment) - several seconds per family,
// dominated by .dmg mount/unmount overhead this file's whole reason to
// exist is to let a later build skip paying again when nothing has
// actually changed (see IsCurrent below).
//
// One file per manufacturer (CatalogFileName, living inside that
// manufacturer's own Drivers/macOS/<Manufacturer>/ folder), not one
// combined catalog - deliberately, so rebuilding or deleting one
// manufacturer's own catalog (to force a full re-index of just that one)
// never touches any other manufacturer's already-indexed data. JSON on
// disk - matching every other persisted PDT format (Settings, SavedConfig):
// no new dependency, stays human-readable, and (this is the point) travels
// with a portable Drivers folder exactly like the packages it describes, so
// a technician's flash drive carries its own already-built index from one
// Mac to the next. See app_darwin.go's loadCatalog for where this file
// actually lives and the local-vs-removable-drive read/write split.
//
// Nothing here is Canon-specific - every field is keyed by family/model
// strings, so the same file format and the same BuildMacModelIndex logic
// already generalizes to any future macFamilyPreference entry with no
// format change needed.
type MacManufacturerCatalog struct {
	Provenance map[string]MacFamilyProvenance `json:"provenance"`
	// ExtraProvenance: family -> packagePath -> MacFamilyProvenance - one
	// entry per OLDER, intentionally-kept compatible package for that family
	// (the current *newest* one still lives in Provenance above, exactly as
	// before). Added once more than one coexisting version of the same
	// family became individually selectable (see MacModelIndex's own doc
	// comment) rather than always collapsing to "newest wins" - each older
	// version gets its own independent staleness record, so it doesn't need
	// re-inspecting on every launch just because it isn't the newest.
	// Additive: omitempty, and absent entirely in every catalog.<mfg>.json
	// written before this field existed - LoadMacManufacturerCatalog treats
	// that the same as "no extra versions cached yet," never a parse error.
	ExtraProvenance map[string]map[string]MacFamilyProvenance `json:"extraProvenance,omitempty"`
	Models          map[string][]MacCatalogVariant            `json:"models"`
	// FailedPackages: family -> packagePath -> the outer package's own
	// path/modTime/size at the moment indexFamilyPackage tried and failed to
	// find any real PPD content in it - confirmed live as a real, live gap
	// (2026-09-18): some packages are permanently unopenable on Windows (see
	// openDmg's own doc comment - some newer/cryptex-style APFS .dmg
	// variants may not extract via 7-Zip at all, a known/accepted
	// limitation, not a bug to chase further), yet nothing remembered that a
	// given package had already failed, so BuildMacModelIndex retried the
	// exact same doomed attempt - opening the outer package, running 7z,
	// failing - from scratch on every single launch, adding several real
	// seconds to Windows startup for a result that could never succeed until
	// the package itself actually changed. Checked before ever calling
	// indexFamilyPackage again for an unchanged package (same path/modTime/
	// size); cleared automatically the moment the package's own modTime/size
	// changes (a new version might fix whatever failed) or it succeeds after
	// having failed before. Additive: omitempty, absent in any
	// catalog.<mfg>.json written before this field existed - same
	// no-entries-yet contract ExtraProvenance already has.
	FailedPackages map[string]map[string]MacPackageRef `json:"failedPackages,omitempty"`
}

// MacPackageRef identifies one file in a package's own chain of nesting, at
// the moment it was actually inspected. ModTime/Size are populated (and
// meaningful for IsCurrent's own staleness check) only for the chain's
// outermost entry - the actual file BuildMacCatalog's directory scan found
// and MacPackage already carries free identity for (see that type's own
// Size field doc comment). Every other entry in the chain (a nested
// .dmg/.pkg/sub-package) only exists inside a mounted volume, gone the
// moment this process unmounts it - there's nothing cheaper to check on a
// later run than mounting the outermost file again, so those entries carry
// Version where one was found (readPackageInfoVersion) and are purely
// informational, recording "which package, specifically" for a human to
// read, never compared against on a later run.
type MacPackageRef struct {
	Path    string    `json:"path"`
	ModTime time.Time `json:"modTime,omitempty"`
	Size    int64     `json:"size,omitempty"`
	Version string    `json:"version,omitempty"`
}

// MacFamilyProvenance is the full chain behind one manufacturer/family's
// currently-indexed entries, outermost first - e.g. Canon's UFR II:
// UFRII_v10.19.25_mac.dmg -> mac-UFRII-LIPSLX-v101925-05.dmg ->
// UFRII_LT_LIPS_LX_Installer.pkg -> Canon_Family_Printer_Device.pkg. Exactly
// the "which package, and its parents, produced this - and when" record
// asked for, and the raw material for comparing what changed the next time
// a newer package replaces this one (see DiffModels).
type MacFamilyProvenance struct {
	Chain     []MacPackageRef `json:"chain"`
	IndexedAt time.Time       `json:"indexedAt"`
}

// MacCatalogVariant is MacManufacturerCatalog's own persisted shape of one
// MacPPDVariant (macmodel.go) - identical fields, JSON-tagged for disk
// storage; kept as a separate type rather than reusing MacPPDVariant
// directly so a field either type needs later doesn't have to leak into the
// other (e.g. Label is deliberately recomputed at load time, not persisted,
// so a display-string change never requires migrating every
// already-written catalog file on disk).
type MacCatalogVariant struct {
	Language           string `json:"language"`
	NickName           string `json:"nickName"`
	Filename           string `json:"filename"`
	PackagePath        string `json:"packagePath,omitempty"`
	LooseCachedPPDPath string `json:"looseCachedPPDPath,omitempty"`
	// SourcePackagePath is the *outer* package (pkg.Path, the real file
	// BuildMacCatalog's own directory scan found) that produced this variant -
	// always set, unlike PackagePath, which is deliberately left empty for a
	// no-installer/loose-PPD family (deploy_darwin.go's own installVariant
	// reads that emptiness to mean "hand the cached PPD straight to lpadmin,
	// no `installer -pkg` run needed" - PackagePath's existing meaning can't
	// be repurposed without breaking that). ModelsForFamilyPackage and
	// BuildMacModelIndex's own per-package pruning match on this field, not
	// PackagePath, so a loose-PPD family's cached variants are found
	// correctly on a cache-hit rebuild too.
	SourcePackagePath string `json:"sourcePackagePath,omitempty"`
	// PackageModTime is the source package's own file modification time, at
	// the moment this variant was indexed - persisted (rather than re-stat'd
	// live every time) so a decorated multi-version label can be rebuilt
	// straight from the catalog file alone.
	PackageModTime time.Time `json:"packageModTime,omitempty"`
	// OSVersionFolder mirrors MacPPDVariant's own field of the same name -
	// persisted so a cache-hit rebuild (toMacPPDVariant) still knows which
	// real OS-version folder this variant came from, without needing to
	// re-inspect the source package (GitHub issue #16 follow-up, 2026-09-19).
	OSVersionFolder string `json:"osVersionFolder,omitempty"`
}

// CatalogFileName returns the catalog filename for manufacturer, meant to
// live inside that manufacturer's own Drivers/<platform>/<Manufacturer>/
// folder - "catalog.<manufacturer, lowercased, spaces stripped>.json" (e.g.
// Canon -> "catalog.canon.json", Konica Minolta ->
// "catalog.konicaminolta.json"). Per-manufacturer, not one combined file -
// see MacManufacturerCatalog's own doc comment for why. Shared by both
// platforms (renamed from MacCatalogFileName once the Windows side grew its
// own catalog.<mfg>.json too, GitHub issue #10) - pure string logic, nothing
// mac-specific about it.
func CatalogFileName(manufacturer string) string {
	folder := strings.ToLower(strings.ReplaceAll(manufacturer, " ", ""))
	return "catalog." + folder + ".json"
}

// IsCatalogFileName reports whether name (a bare file name, not a full path)
// is a per-manufacturer catalog file in the shape CatalogFileName produces -
// "catalog.<anything>.json". Used by Cloud Sync (internal/cloudsync/plan.go)
// to exclude catalog.<mfg>.json from being synced at all: unlike
// PdtInfCacheDirName's own contents (deterministic .inf bytes, genuinely
// worth sharing - see IsIgnoredDotEntry's own doc comment), a catalog file
// bakes in a fresh IndexedAt build timestamp on every rebuild plus the
// building machine's own view of each source archive's modTime, so two
// technicians independently rebuilding a catalog for the identical
// underlying driver repo never produce byte-identical files - Cloud Sync's
// own byte-size conflict check (plan.go) then flags a permanent,
// never-resolvable conflict for a file that isn't actually wrong. Cheaply
// rebuildable locally on demand (see MacManufacturerCatalog's own doc
// comment - a cache, not user data), so excluding it from Sync entirely
// costs nothing but a few seconds of local re-indexing, once per machine per
// real change.
func IsCatalogFileName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "catalog.") && strings.HasSuffix(lower, ".json")
}

// LoadMacManufacturerCatalog reads path's own catalog file - an empty,
// ready-to-use catalog (never nil maps) for a missing or unparsable file,
// exactly like config.LoadConfig's own "recover to sane defaults" contract,
// since this is a rebuildable cache, not user data: a corrupt/missing
// catalog file just costs one full re-inspection of that one manufacturer,
// the same cost every build paid before this file existed at all.
func LoadMacManufacturerCatalog(path string) MacManufacturerCatalog {
	empty := MacManufacturerCatalog{
		Provenance:      map[string]MacFamilyProvenance{},
		ExtraProvenance: map[string]map[string]MacFamilyProvenance{},
		Models:          map[string][]MacCatalogVariant{},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	cat := empty
	if err := json.Unmarshal(data, &cat); err != nil {
		return empty
	}
	if cat.Provenance == nil {
		cat.Provenance = map[string]MacFamilyProvenance{}
	}
	if cat.ExtraProvenance == nil {
		cat.ExtraProvenance = map[string]map[string]MacFamilyProvenance{}
	}
	if cat.Models == nil {
		cat.Models = map[string][]MacCatalogVariant{}
	}
	return cat
}

// SaveMacManufacturerCatalog writes cat to path as indented JSON, creating
// path's own parent directory if needed (a fresh manufacturer folder has no
// catalog file in it yet until the first successful build writes one).
func SaveMacManufacturerCatalog(path string, cat MacManufacturerCatalog) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// relToDriversRoot converts absPath (expected to be under driversRoot, e.g.
// a MacPackage.Path from a fresh BuildMacCatalog scan) into a
// driversRoot-relative form for persisting into MacPackageRef.Path/
// MacCatalogVariant.PackagePath/SourcePackagePath - GitHub issue #13: a
// catalog.<mfg>.json travels with the portable Drivers folder itself, but an
// absolute path baked in at index time is platform- and machine-specific
// (a different drive letter, username, or OS entirely), so it can never
// string-match what a *different* machine's own fresh scan computes for the
// identical file - IsCurrent then never recognizes the catalog as current,
// silently losing the whole point of caching it. Falls back to absPath
// unchanged if it isn't actually under driversRoot (defensive - never worse
// than the old, always-absolute behavior, just doesn't gain the fix for
// that one entry).
func relToDriversRoot(driversRoot, absPath string) string {
	if absPath == "" {
		return ""
	}
	rel, err := filepath.Rel(driversRoot, absPath)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return absPath
	}
	// Stored slash-normalized (forward slash), never the native OS
	// separator - confirmed live as a real bug before shipping this: a
	// Windows-built relative path like "macOS\Kyocera\x.dmg" stores fine on
	// Windows, but filepath.Join on a real Mac treats a literal backslash as
	// just another filename character, not a separator, so it would never
	// resolve to the real nested file there. Forward slash works as a valid
	// separator on both platforms (Windows' own filepath package already
	// accepts it as an alternate separator natively), so this is the one
	// storage form that's actually portable either direction. See
	// absFromDriversRoot's own FromSlash conversion on the way back.
	return filepath.ToSlash(rel)
}

// absFromDriversRoot is relToDriversRoot's own inverse, used when reading a
// persisted value back into something a live caller can actually open.
// Critically: relPath already being absolute (a catalog.<mfg>.json written
// before this fix existed, when every path was stored absolute) is left
// unchanged rather than wrongly re-joined with driversRoot - old entries
// just keep exhibiting the pre-fix cross-machine-mismatch behavior (one
// harmless re-index), never a wrong or garbled path. No schema/version bump
// needed: the field is still a plain string, only what it means changed,
// and this function is the one place both meanings are accepted.
func absFromDriversRoot(driversRoot, relPath string) string {
	if relPath == "" || filepath.IsAbs(relPath) {
		return relPath
	}
	return filepath.Join(driversRoot, filepath.FromSlash(relPath))
}

// relToPPDCacheRoot/absFromPPDCacheRoot are relToDriversRoot/
// absFromDriversRoot's own siblings for MacCatalogVariant.LooseCachedPPDPath
// - the same GitHub issue #13 portability problem, on a field issue #13's
// own fix missed. Confirmed live: LooseCachedPPDPath used to be persisted
// absolute and untouched (installedAppDataDir-based, e.g.
// "/Users/ken/Library/.../PPDCache/Canon/PPD/<hash>/<file>.PPD.gz"), which
// bakes the current machine's own username straight into catalog.<mfg>.json
// - a file Cloud Sync diffs by raw byte size (internal/cloudsync/plan.go).
// Two technicians' machines can never produce a byte-identical catalog for
// any manufacturer with loose (no-installer) PPD variants this way, even
// with 100% identical real driver data, so it showed up as a perpetual,
// never-resolving Cloud Sync conflict rather than a one-time staleness blip.
// The subpath under ppdCacheDir (CachePPDFile's own destDir/filename) is
// fully content-derived - manufacturer, family, a package-identity hash,
// and the real PPD filename, nothing machine-specific - so relativizing it
// the same way issue #13 already relativizes PackagePath/SourcePackagePath
// is safe and produces identical bytes across any two machines with the
// same real cached content.
func relToPPDCacheRoot(ppdCacheDir, absPath string) string {
	return relToDriversRoot(ppdCacheDir, absPath)
}

func absFromPPDCacheRoot(ppdCacheDir, relPath string) string {
	return absFromDriversRoot(ppdCacheDir, relPath)
}

// IsCurrent reports whether cat already has provenance recorded for family
// whose outermost chain entry exactly matches pkg (Path, ModTime, and Size
// all equal) - the cheap, mount-free check that lets BuildMacModelIndex
// skip re-inspecting a package it's already indexed. Deliberately not a
// content hash: matches listFileSizes' own reasoning in copytree.go for
// exactly the same tradeoff - a downloaded driver package is never
// silently modified in place, so path+modtime+size is already as good as a
// hash here, without needing to read the package's own tens/hundreds of MB
// to compute one.
//
// Compares on the driversRoot-relative form (relToDriversRoot(driversRoot,
// pkg.Path)), not pkg.Path directly - see relToDriversRoot's own doc
// comment (GitHub issue #13). A pre-fix catalog file's own outer.Path is
// still absolute; comparing it against a freshly-relativized value simply
// never matches, which is exactly the safe, backward-compatible "treat as
// stale, re-index once" degrade every other pre-fix entry already gets.
func (cat MacManufacturerCatalog) IsCurrent(family string, pkg MacPackage, driversRoot string) bool {
	prov, ok := cat.Provenance[family]
	if !ok || len(prov.Chain) == 0 {
		return false
	}
	outer := prov.Chain[0]
	return outer.Path == relToDriversRoot(driversRoot, pkg.Path) && outer.ModTime.Equal(pkg.ModTime) && outer.Size == pkg.Size
}

// ModelsForFamily returns cat's own already-recorded variants whose
// Language == family - what BuildMacModelIndex reuses in place of
// re-inspecting a package IsCurrent already confirmed is unchanged.
func (cat MacManufacturerCatalog) ModelsForFamily(family string) map[string][]MacCatalogVariant {
	out := map[string][]MacCatalogVariant{}
	for model, variants := range cat.Models {
		for _, v := range variants {
			if v.Language == family {
				out[model] = append(out[model], v)
			}
		}
	}
	return out
}

// ModelsForFamilyPackage is ModelsForFamily narrowed to just the variants
// that came from sourcePackagePath specifically - needed once more than one
// package can contribute variants to the same family at once (see
// ExtraProvenance's own doc comment); ModelsForFamily itself still answers
// "every variant for this family, from any package," which is what
// DiffModels' own whole-family diff wants. Matches on SourcePackagePath, not
// PackagePath - the latter is deliberately empty for a loose-PPD family (see
// MacCatalogVariant's own doc comment), which would otherwise never be found
// here at all.
func (cat MacManufacturerCatalog) ModelsForFamilyPackage(family, sourcePackagePath string) map[string][]MacCatalogVariant {
	out := map[string][]MacCatalogVariant{}
	for model, variants := range cat.Models {
		for _, v := range variants {
			if v.Language == family && v.SourcePackagePath == sourcePackagePath {
				out[model] = append(out[model], v)
			}
		}
	}
	return out
}

// IsCurrentForPackage is IsCurrent's own sibling for one specific,
// intentionally-kept OLDER package within a family (see ExtraProvenance's
// own doc comment) - identical check, just against
// cat.ExtraProvenance[family][relPkgPath] instead of cat.Provenance[family].
// ExtraProvenance is itself keyed by the driversRoot-relative form (GitHub
// issue #13), same as cat.Provenance[family].Chain[0].Path.
func (cat MacManufacturerCatalog) IsCurrentForPackage(family string, pkg MacPackage, driversRoot string) bool {
	relPkgPath := relToDriversRoot(driversRoot, pkg.Path)
	prov, ok := cat.ExtraProvenance[family][relPkgPath]
	if !ok || len(prov.Chain) == 0 {
		return false
	}
	outer := prov.Chain[0]
	return outer.Path == relPkgPath && outer.ModTime.Equal(pkg.ModTime) && outer.Size == pkg.Size
}

// IsKnownFailed reports whether pkg (identified by its own driversRoot-
// relative path) already failed to index for family, and hasn't changed
// (same modTime/size) since that attempt - see FailedPackages' own doc
// comment for why this exists: without it, a package indexFamilyPackage can
// never actually open (a known, accepted gap for some dmg variants - see
// openDmg) gets retried at full cost on every single BuildMacModelIndex
// call forever, since a failed attempt was never otherwise distinguished
// from "never attempted at all."
func (cat MacManufacturerCatalog) IsKnownFailed(family string, pkg MacPackage, driversRoot string) bool {
	failed, ok := cat.FailedPackages[family][relToDriversRoot(driversRoot, pkg.Path)]
	if !ok {
		return false
	}
	return failed.ModTime.Equal(pkg.ModTime) && failed.Size == pkg.Size
}

// DiffModels compares the model names cat already had recorded for family
// against current (a fresh inspection's own result, keyed the same way
// ModelsForFamily returns) and reports which model names are newly present
// or no longer present - the raw "what changed between this package and
// the last one indexed" signal, logged by the caller (app_darwin.go's
// loadCatalog) rather than this package trying to judge staleness or
// regressions itself; that's a human call once they can see what actually
// changed.
func DiffModels(cat MacManufacturerCatalog, family string, current map[string][]MacCatalogVariant) (added, removed []string) {
	previous := cat.ModelsForFamily(family)
	for model := range current {
		if _, ok := previous[model]; !ok {
			added = append(added, model)
		}
	}
	for model := range previous {
		if _, ok := current[model]; !ok {
			removed = append(removed, model)
		}
	}
	return added, removed
}
