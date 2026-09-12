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
// One file per manufacturer (MacCatalogFileName, living inside that
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
	Models     map[string][]MacCatalogVariant `json:"models"`
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
}

// MacCatalogFileName returns the catalog filename for manufacturer, meant
// to live inside that manufacturer's own Drivers/macOS/<Manufacturer>/
// folder - "catalog.<manufacturer, lowercased, spaces stripped>.json" (e.g.
// Canon -> "catalog.canon.json", Konica Minolta ->
// "catalog.konicaminolta.json"). Per-manufacturer, not one combined file -
// see MacManufacturerCatalog's own doc comment for why.
func MacCatalogFileName(manufacturer string) string {
	folder := strings.ToLower(strings.ReplaceAll(manufacturer, " ", ""))
	return "catalog." + folder + ".json"
}

// LoadMacManufacturerCatalog reads path's own catalog file - an empty,
// ready-to-use catalog (never nil maps) for a missing or unparsable file,
// exactly like config.LoadConfig's own "recover to sane defaults" contract,
// since this is a rebuildable cache, not user data: a corrupt/missing
// catalog file just costs one full re-inspection of that one manufacturer,
// the same cost every build paid before this file existed at all.
func LoadMacManufacturerCatalog(path string) MacManufacturerCatalog {
	empty := MacManufacturerCatalog{Provenance: map[string]MacFamilyProvenance{}, Models: map[string][]MacCatalogVariant{}}
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

// IsCurrent reports whether cat already has provenance recorded for family
// whose outermost chain entry exactly matches pkg (Path, ModTime, and Size
// all equal) - the cheap, mount-free check that lets BuildMacModelIndex
// skip re-inspecting a package it's already indexed. Deliberately not a
// content hash: matches listFileSizes' own reasoning in copytree.go for
// exactly the same tradeoff - a downloaded driver package is never
// silently modified in place, so path+modtime+size is already as good as a
// hash here, without needing to read the package's own tens/hundreds of MB
// to compute one.
func (cat MacManufacturerCatalog) IsCurrent(family string, pkg MacPackage) bool {
	prov, ok := cat.Provenance[family]
	if !ok || len(prov.Chain) == 0 {
		return false
	}
	outer := prov.Chain[0]
	return outer.Path == pkg.Path && outer.ModTime.Equal(pkg.ModTime) && outer.Size == pkg.Size
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
