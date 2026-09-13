package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testModelCatalog builds the catalog from testdata_mac_model - real fixtures
// standing in for Canon's own UFR II/PostScript/"loose PPD" split, so
// BuildMacModelIndex's own family-unification and no-installer-family
// handling can be exercised without needing the user's own real,
// multi-hundred-MB Canon downloads. Two pkgbuild-built .pkg fixtures
// (UFR II, PostScript) each carry one real PPD; the third ("PPD" family) is
// a real nested-.dmg fixture with no .pkg inside at all, matching Canon's
// actual no-installer "PPD" bucket shape (confirmed live - see
// internal/driver/macmodel.go's own doc comments). "TestVendor Model X" is
// covered by all three families (UFRII: no suffix, PS: " PS" suffix, PPD:
// " PPD" suffix, per the real Canon naming convention this whole feature is
// built around); "TestVendor Model Y" exists only in the UFRII package, to
// exercise a model with just one variant. Skips on non-macOS - LocatePkg/
// LocateLoosePPDs both shell out to real macOS-only tools (pkgutil, hdiutil).
func testModelCatalog(t *testing.T) MacCatalog {
	t.Helper()
	disableOSVersionFiltering(t)
	if _, err := exec.LookPath("pkgutil"); err != nil {
		t.Skip("pkgutil not on PATH (not running on macOS)")
	}
	cat, err := BuildMacCatalog("testdata_mac_model")
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}
	return cat
}

func TestIsJapanMarketOnly(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Canon iR-ADV C5840/5850", false},
		{"Canon 971Ci JP", true},
		// Confirmed against real Canon data: JP comes after the language
		// token, not instead of it - stripLanguageSuffix never sees this
		// shape at all since it checks nickName's own literal suffix.
		{"Canon 971Ci PS JP", true},
		{"Canon iR-ADV C5840/5850 PPD", false},
		// Must be a real trailing token, not a coincidental substring.
		{"Canon LBPJP100", false},
		// Ricoh's own two real conventions, neither matching Canon's shape -
		// confirmed against a real 427-model Ricoh catalog build, 2026-09.
		{"RICOH MP 1301 JPN PS", true},
		{"RICOH IM 2509J PS", true},
		{"RICOH MP 2554J PS", true},
		// Must be a real digit-J token, not a coincidental one.
		{"RICOH IM C300", false},
		// Matches even with nothing after the "J" (end-of-string counts as a
		// boundary too, not just a following space) - a bare Japan-market
		// model number with no language suffix at all is still a real shape
		// this needs to catch.
		{"RICOH IM C300J", true},
	}
	for _, tt := range tests {
		if got := isJapanMarketOnly(tt.name); got != tt.want {
			t.Errorf("isJapanMarketOnly(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestBuildMacModelIndex_UnifiesModelAcrossAllThreeLanguageFamilies(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	variants := index["Canon"]["TestVendor Model X"]
	if len(variants) != 3 {
		t.Fatalf("expected 3 variants for TestVendor Model X (UFRII+PS+PPD), got %d: %+v", len(variants), variants)
	}

	byLang := map[string]MacPPDVariant{}
	for _, v := range variants {
		byLang[v.Language] = v
	}
	for _, lang := range []string{"UFRII", "PS", "PPD"} {
		if _, ok := byLang[lang]; !ok {
			t.Errorf("expected a %s variant, got none among %+v", lang, variants)
		}
	}

	if byLang["UFRII"].PackagePath == "" {
		t.Error("UFRII variant should have a PackagePath (installer-backed family)")
	}
	if byLang["PS"].PackagePath == "" {
		t.Error("PS variant should have a PackagePath (installer-backed family)")
	}
	if byLang["PPD"].PackagePath != "" {
		t.Errorf("PPD (loose) variant should have no PackagePath, got %q", byLang["PPD"].PackagePath)
	}
	if byLang["PPD"].LooseCachedPPDPath == "" {
		t.Error("PPD (loose) variant should have a LooseCachedPPDPath (no-installer family)")
	}
}

func TestBuildMacModelIndex_ModelWithOnlyOneFamilyGetsOneVariant(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	variants := index["Canon"]["TestVendor Model Y"]
	if len(variants) != 1 {
		t.Fatalf("expected exactly 1 variant for TestVendor Model Y (UFRII-only), got %d: %+v", len(variants), variants)
	}
	if variants[0].Language != "UFRII" {
		t.Errorf("expected the one variant to be UFRII, got %q", variants[0].Language)
	}
}

func TestBuildMacModelIndex_ManufacturerWithNoFamilyTableIsAbsent(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)
	// "HP", not "Kyocera", "Ricoh" or "Sharp" - all three got their own real
	// macFamilyPreference entries (see macfamily.go's own doc comment) once
	// each got a real model index built, so none of them is a valid example
	// of "a manufacturer with no family table at all" anymore.
	if _, ok := index["HP"]; ok {
		t.Error("expected no model index entry at all for a manufacturer with no macFamilyPreference table")
	}
}

func TestMacModels_RanksByFilterText(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	all := MacModels(index, "Canon", "")
	if len(all) != 2 {
		t.Fatalf("expected 2 known models, got %v", all)
	}

	narrowed := MacModels(index, "Canon", "Model Y")
	if len(narrowed) == 0 || narrowed[0] != "TestVendor Model Y" {
		t.Errorf("expected \"TestVendor Model Y\" ranked first for filterText \"Model Y\", got %v", narrowed)
	}
}

func TestMacModelCandidates_PreferenceOrderedWhenNoFilter(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	labels := MacModelCandidates(index, "Canon", "TestVendor Model X", "")
	if len(labels) != 3 {
		t.Fatalf("expected 3 labels, got %v", labels)
	}
	want := []string{"TestVendor Model X (UFR II)", "TestVendor Model X (PostScript)", "TestVendor Model X (Generic PPD)"}
	for i, w := range want {
		if labels[i] != w {
			t.Errorf("labels[%d] = %q, want %q (full list: %v)", i, labels[i], w, labels)
		}
	}
}

// TestMacModelCandidates_BlankModelListsEveryModel guards against a real,
// previously-shipped bug: a blank Model returned zero candidates instead of
// every model's own variants, collapsing DriverCandidates down to
// Manufacturer's own single guessed-package label on every real Canon
// download (641 real models, none ever offered) until MacModelCandidates
// grew its own blank-model branch - see that function's own doc comment.
func TestMacModelCandidates_BlankModelListsEveryModel(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	labels := MacModelCandidates(index, "Canon", "", "")
	if len(labels) != 4 {
		t.Fatalf("expected 4 labels (3 for Model X + 1 for Model Y), got %v", labels)
	}
	// Grouped by language rank (UFR II, then PostScript, then Generic PPD -
	// macFamilyPreference's own order), models sorted alphabetically within
	// each language group - fully deterministic, not map iteration order.
	want := []string{
		"TestVendor Model X (UFR II)", "TestVendor Model Y (UFR II)",
		"TestVendor Model X (PostScript)", "TestVendor Model X (Generic PPD)",
	}
	for i, w := range want {
		if labels[i] != w {
			t.Errorf("labels[%d] = %q, want %q (full list: %v)", i, labels[i], w, labels)
		}
	}
}

func TestMacModelCandidates_UnknownModelReturnsEmpty(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)
	labels := MacModelCandidates(index, "Canon", "No Such Model", "")
	if len(labels) != 0 {
		t.Errorf("expected no candidates for an unknown model, got %v", labels)
	}
}

// TestBuildMacModelIndex_SecondBuildReusesCatalogWithoutReinspecting is the
// whole point of catalog.<mfg>.json (MacManufacturerCatalog): a second call
// against the exact same packages must produce the identical index without
// paying the mount/expand cost again. Proven two ways: identical results,
// and dramatically faster (the first build here still takes several
// seconds - real mounting/expanding of the real testdata fixtures - a
// cache-hit second build has no mounting to do at all).
func TestBuildMacModelIndex_SecondBuildReusesCatalogWithoutReinspecting(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()

	first, changes := BuildMacModelIndex(cat, dir, dir, true)
	if len(first["Canon"]) == 0 {
		t.Fatal("expected the first build to actually index something")
	}
	if len(changes) != 0 {
		t.Errorf("expected no changes on a first-ever build (nothing to diff against), got %v", changes)
	}

	catalogPath := filepath.Join(dir, "Canon", MacCatalogFileName("Canon"))
	if _, err := os.Stat(catalogPath); err != nil {
		t.Fatalf("expected %s to exist after a persisted build: %v", catalogPath, err)
	}

	start := time.Now()
	second, changes := BuildMacModelIndex(cat, dir, dir, true)
	elapsed := time.Since(start)

	if len(changes) != 0 {
		t.Errorf("expected no changes on a cache-hit build (nothing actually changed), got %v", changes)
	}
	if elapsed > 2*time.Second {
		t.Errorf("second build took %s - expected a cache hit (no mounting) to be near-instant", elapsed)
	}

	for _, mfg := range []string{"Canon"} {
		for model, variants := range first[mfg] {
			if len(second[mfg][model]) != len(variants) {
				t.Errorf("%s/%q: first build had %d variant(s), second (cached) build had %d", mfg, model, len(variants), len(second[mfg][model]))
			}
		}
	}
}

// testMultiVersionCatalog builds the catalog from testdata_mac_multiversion -
// its own isolated fixture directory (deliberately separate from
// testdata_mac_model, which several other tests already make assumptions
// about a single UFRII package producing) containing two byte-identical
// copies of the same real UFRII .pkg fixture, forced to deterministic,
// distinct mtimes the same way testMacCatalog already does for its own
// Older/Newer pair - git doesn't preserve mtimes across a clone/checkout, so
// both files would otherwise land with essentially the same checkout-time
// mtime.
func testMultiVersionCatalog(t *testing.T) MacCatalog {
	t.Helper()
	disableOSVersionFiltering(t)
	if _, err := exec.LookPath("pkgutil"); err != nil {
		t.Skip("pkgutil not on PATH (not running on macOS)")
	}
	now := time.Now()
	older := filepath.Join("testdata_mac_multiversion", "macOS", "Canon", "26-Tahoe", "UFRII_test_fixture_v1.pkg")
	newer := filepath.Join("testdata_mac_multiversion", "macOS", "Canon", "26-Tahoe", "UFRII_test_fixture_v2.pkg")
	if err := os.Chtimes(older, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("os.Chtimes(%s): %v", older, err)
	}
	if err := os.Chtimes(newer, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("os.Chtimes(%s): %v", newer, err)
	}
	cat, err := BuildMacCatalog("testdata_mac_multiversion")
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}
	return cat
}

// TestBuildMacModelIndex_TwoCoexistingVersionsBothIndexedAndDecorated is the
// real end-to-end proof for issue #4: two compatible UFRII packages sitting
// in the Drivers folder at once must both get indexed (not just the newest,
// unlike before this feature existed), each with its own decorated Label so
// a technician can tell them apart in the Driver dropdown - mirroring
// Windows' own Candidates() decoration for a multi-version driver name.
func TestBuildMacModelIndex_TwoCoexistingVersionsBothIndexedAndDecorated(t *testing.T) {
	cat := testMultiVersionCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	variants := index["Canon"]["TestVendor Model X"]
	var ufrii []MacPPDVariant
	for _, v := range variants {
		if v.Language == "UFRII" {
			ufrii = append(ufrii, v)
		}
	}
	if len(ufrii) != 2 {
		t.Fatalf("expected 2 coexisting UFRII variants for TestVendor Model X, got %d: %+v", len(ufrii), ufrii)
	}

	for _, v := range ufrii {
		if v.Label == "TestVendor Model X (UFR II)" {
			t.Errorf("variant from %s kept the plain undecorated label %q - expected it decorated once a second coexisting version exists", v.PackagePath, v.Label)
		}
	}
	if ufrii[0].Label == ufrii[1].Label {
		t.Errorf("both coexisting variants got the identical label %q - expected them distinguishable", ufrii[0].Label)
	}

	// A model with only one real variant elsewhere in the *same* build
	// (there isn't one here - both fixtures register the same two models) is
	// covered by TestMacVariantForDeploy_ExactLabelWins's own plain-label
	// assertion already; this test's own job is just the multi-version case.
}

// TestMacVariantForDeploy_BlankSelectionAlwaysPicksNewestVersion is Ken's own
// explicit requirement (2026-09-13): once more than one coexisting version
// is individually selectable, a blank/ambiguous Driver selection must still
// always resolve to the *latest* version, never an arbitrary older one left
// around for manual selection.
func TestMacVariantForDeploy_BlankSelectionAlwaysPicksNewestVersion(t *testing.T) {
	cat := testMultiVersionCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	variant, ok := MacVariantForDeploy(index, "Canon", "TestVendor Model X", "")
	if !ok {
		t.Fatal("expected a match")
	}
	if !strings.Contains(variant.PackagePath, "_v2") {
		t.Errorf("expected the newer package (_v2) to win a blank/ambiguous selection, got %q", variant.PackagePath)
	}
}

// TestBuildMacModelIndex_MultiVersionSecondBuildReusesCatalog proves the
// cache-hit path works correctly for *each* coexisting package
// independently (ExtraProvenance/IsCurrentForPackage/ModelsForFamilyPackage) -
// not just the single-newest-package cache path
// TestBuildMacModelIndex_SecondBuildReusesCatalogWithoutReinspecting already
// covers.
func TestBuildMacModelIndex_MultiVersionSecondBuildReusesCatalog(t *testing.T) {
	cat := testMultiVersionCatalog(t)
	dir := t.TempDir()

	first, _ := BuildMacModelIndex(cat, dir, dir, true)
	second, _ := BuildMacModelIndex(cat, dir, dir, true)

	firstCount := 0
	for _, v := range first["Canon"]["TestVendor Model X"] {
		if v.Language == "UFRII" {
			firstCount++
		}
	}
	secondCount := 0
	for _, v := range second["Canon"]["TestVendor Model X"] {
		if v.Language == "UFRII" {
			secondCount++
		}
	}
	if firstCount != 2 || secondCount != 2 {
		t.Fatalf("expected 2 UFRII variants both before and after a cache-hit rebuild, got %d then %d", firstCount, secondCount)
	}
}

// TestBuildMacModelIndex_MigratesLegacyCatalogMissingSourcePackagePath
// guards a real bug found live (2026-09-13) against Ken's own real Drivers
// folder: a catalog.<mfg>.json written before SourcePackagePath existed has
// every persisted entry's own SourcePackagePath empty. The cache-reuse
// lookup (ModelsForFamilyPackage, matching on SourcePackagePath) came back
// empty for every such legacy entry, and cachedVariantFilesExist's own
// vacuous-true-on-an-empty-map behavior meant that empty result was
// silently trusted as "already correctly cached, nothing to do" instead of
// falling through to a real reindex - Kyocera and Ricoh's entire Model
// dropdown went silently empty as a result, even though their real
// downloaded packages were completely untouched. Also confirms the
// migration actually *replaces* the stale untagged entries rather than
// just adding freshly-tagged ones alongside them.
func TestBuildMacModelIndex_MigratesLegacyCatalogMissingSourcePackagePath(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()

	first, _ := BuildMacModelIndex(cat, dir, dir, true)
	if len(first["Canon"]) == 0 {
		t.Fatal("expected the first build to actually index something")
	}

	// Simulate a pre-2026-09-13 catalog file by stripping SourcePackagePath
	// from every persisted entry, exactly like a real catalog.json written
	// before that field existed.
	catalogPath := filepath.Join(dir, "Canon", MacCatalogFileName("Canon"))
	legacy := LoadMacManufacturerCatalog(catalogPath)
	for model, variants := range legacy.Models {
		for i := range variants {
			variants[i].SourcePackagePath = ""
		}
		legacy.Models[model] = variants
	}
	if err := SaveMacManufacturerCatalog(catalogPath, legacy); err != nil {
		t.Fatalf("writing simulated legacy catalog: %v", err)
	}

	second, _ := BuildMacModelIndex(cat, dir, dir, true)
	if len(second["Canon"]) == 0 {
		t.Fatal("expected a legacy catalog (missing SourcePackagePath) to be transparently reindexed, not silently emptied - this is the exact real bug found live")
	}
	if len(second["Canon"]) != len(first["Canon"]) {
		t.Errorf("expected the same model count after migrating a legacy catalog: first=%d second=%d", len(first["Canon"]), len(second["Canon"]))
	}

	migrated := LoadMacManufacturerCatalog(catalogPath)
	for model, variants := range migrated.Models {
		seen := map[string]int{}
		for _, v := range variants {
			seen[v.Language]++
		}
		for lang, count := range seen {
			if count > 1 {
				t.Errorf("%q has %d duplicate %s entries after migration - stale legacy entries were not replaced, just added alongside", model, count, lang)
			}
		}
	}
}

// TestBuildMacModelIndex_PrunesOrphanedPackageNoLongerInCurrentSet guards a
// real bug found live (2026-09-13), right after the SourcePackagePath
// migration fix above: a real Kyocera catalog had the exact same download
// copied into 10 different OS-version folders (the established Drivers
// folder convention), and packagesInFamily's own (basename, size)
// deduplication correctly collapsed those down to a single representative
// package - but the 9 *other* copies, each individually tracked as their
// own "package" under an earlier code path (before dedup existed, or simply
// no longer part of the current set for any other reason - a file genuinely
// removed works the same way), were never revisited by BuildMacModelIndex's
// own per-package loop again at all once they dropped out of
// packagesInFamily's result, so their own stale cat.Models entries and
// ExtraProvenance keys lingered forever - every one of 460 real Kyocera
// models showed up with 10 duplicate "versions" of the identical download.
func TestBuildMacModelIndex_PrunesOrphanedPackageNoLongerInCurrentSet(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()

	first, _ := BuildMacModelIndex(cat, dir, dir, true)
	if len(first["Canon"]) == 0 {
		t.Fatal("expected the first build to actually index something")
	}

	// Inject an orphaned "extra" package entry - simulating a package that
	// used to be individually tracked but is no longer part of the current
	// set at all (the real scenario: a duplicate OS-folder copy that
	// packagesInFamily's own dedup now correctly excludes).
	catalogPath := filepath.Join(dir, "Canon", MacCatalogFileName("Canon"))
	withOrphan := LoadMacManufacturerCatalog(catalogPath)
	const orphanPath = "/nonexistent/orphaned/UFRII_test_fixture_stale_copy.pkg"
	if withOrphan.ExtraProvenance["UFRII"] == nil {
		withOrphan.ExtraProvenance["UFRII"] = map[string]MacFamilyProvenance{}
	}
	withOrphan.ExtraProvenance["UFRII"][orphanPath] = MacFamilyProvenance{
		Chain: []MacPackageRef{{Path: orphanPath}},
	}
	withOrphan.Models["TestVendor Model X"] = append(withOrphan.Models["TestVendor Model X"], MacCatalogVariant{
		Language: "UFRII", NickName: "TestVendor Model X", Filename: "TESTX1.ppd",
		PackagePath: orphanPath, SourcePackagePath: orphanPath,
	})
	if err := SaveMacManufacturerCatalog(catalogPath, withOrphan); err != nil {
		t.Fatalf("writing catalog with injected orphan: %v", err)
	}

	second, _ := BuildMacModelIndex(cat, dir, dir, true)

	for _, v := range second["Canon"]["TestVendor Model X"] {
		if v.PackagePath == orphanPath {
			t.Errorf("orphaned package entry %q survived a rebuild - should have been pruned since it's no longer part of the current package set", orphanPath)
		}
	}

	migrated := LoadMacManufacturerCatalog(catalogPath)
	if _, stillThere := migrated.ExtraProvenance["UFRII"][orphanPath]; stillThere {
		t.Errorf("orphaned ExtraProvenance entry for %q survived a rebuild", orphanPath)
	}
	for _, v := range migrated.Models["TestVendor Model X"] {
		if v.PackagePath == orphanPath {
			t.Errorf("orphaned Models entry for %q survived on disk after a rebuild", orphanPath)
		}
	}
}

// TestBuildMacModelIndex_PrunesFamilyThatDisappearedEntirely guards issue
// #5's own original real bug: moving a real Ricoh package into an Archive
// folder (or deleting it outright) correctly dropped it from the live
// in-memory index, but its own entries in cat.Models/cat.Provenance/
// cat.ExtraProvenance were never touched at all, since packagesInFamily
// returning zero packages for that family short-circuited straight past
// the pruning logic entirely - the persisted catalog file accumulated
// permanently dead entries for any archived/removed vendor package.
func TestBuildMacModelIndex_PrunesFamilyThatDisappearedEntirely(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()

	first, _ := BuildMacModelIndex(cat, dir, dir, true)
	if len(first["Canon"]) == 0 {
		t.Fatal("expected the first build to actually index something")
	}

	// Simulate every real Canon package disappearing entirely (deleted, or
	// moved to Archive) - an empty catalog, same persisted Drivers root.
	empty := MacCatalog{Packages: map[string][]MacPackage{}, OpenPrintingPPDs: map[string][]string{}}
	second, changes := BuildMacModelIndex(empty, dir, dir, true)

	if len(second["Canon"]) != 0 {
		t.Errorf("expected Canon to be completely absent from the live index once every package is gone, got %d model(s)", len(second["Canon"]))
	}
	if len(changes) == 0 {
		t.Error("expected a 'models removed' change to be reported")
	}

	catalogPath := filepath.Join(dir, "Canon", MacCatalogFileName("Canon"))
	persisted := LoadMacManufacturerCatalog(catalogPath)
	if len(persisted.Models) != 0 {
		t.Errorf("expected the persisted catalog to be pruned to zero models, got %d - stale entries survived", len(persisted.Models))
	}
	if len(persisted.Provenance) != 0 {
		t.Errorf("expected Provenance to be pruned too, got %v", persisted.Provenance)
	}
	if len(persisted.ExtraProvenance) != 0 {
		t.Errorf("expected ExtraProvenance to be pruned too, got %v", persisted.ExtraProvenance)
	}
}

// TestMacVariantForDeploy_AutoSwitchesWhenSavedConfigMovesToADifferentOSVersion
// answers Ken's own real question (2026-09-13): if a technician saves a
// configuration on a laptop running one macOS release, then loads the same
// saved config from a flash drive plugged into a client endpoint running an
// older release, does the deployed driver automatically switch to whatever
// that endpoint's own OS-appropriate version is, or does deploy try to use
// the (now OS-incompatible) driver label the saved config file remembers?
//
// Confirmed: automatic, no manual reselection needed. MacVariantForDeploy's
// own existing "exact label match, else fall back to family-preference
// order" logic already handles this for free, since the model index itself
// is rebuilt fresh (and OS-filtered - see macosversion.go) every time
// BuildMacModelIndex runs, which happens on every app launch. A saved
// driverLabel from a different OS simply won't exact-match anything in the
// newly-rebuilt, differently-filtered index, so resolution falls through to
// whichever variant *is* available on the current machine.
func TestMacVariantForDeploy_AutoSwitchesWhenSavedConfigMovesToADifferentOSVersion(t *testing.T) {
	t.Cleanup(func() { currentMacOSVersionPrefixFunc = detectCurrentMacOSVersionPrefix })
	if _, err := exec.LookPath("pkgutil"); err != nil {
		t.Skip("pkgutil not on PATH (not running on macOS)")
	}

	cat, err := BuildMacCatalog("testdata_mac_osswitch")
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}

	// Simulate building/saving the configuration on a laptop running macOS
	// 26 (Tahoe): the model index only sees the Tahoe-scoped package, and
	// the technician's own committed Driver label reflects that.
	currentMacOSVersionPrefixFunc = func() string { return "26" }
	dir := t.TempDir()
	tahoeIndex, _ := BuildMacModelIndex(cat, dir, dir, true)
	tahoeVariant, ok := MacVariantForDeploy(tahoeIndex, "Canon", "TestVendor Model X", "")
	if !ok {
		t.Fatal("expected a match while building on Tahoe")
	}
	if !strings.Contains(tahoeVariant.PackagePath, "tahoe") {
		t.Fatalf("expected the Tahoe-scoped package to win while building on Tahoe, got %q", tahoeVariant.PackagePath)
	}
	savedDriverLabel := tahoeVariant.Label // what actually gets written into the saved config's Driver field

	// Now simulate the SAME saved config being opened on a flash drive
	// plugged into a client endpoint still running macOS 10.15 (Catalina) -
	// a fresh BuildMacModelIndex call (a new process launch, a new sw_vers
	// query) sees a different current OS, and this endpoint has never
	// indexed anything before (a fresh cache dir).
	currentMacOSVersionPrefixFunc = func() string { return "10.15" }
	dir2 := t.TempDir()
	catalinaIndex, _ := BuildMacModelIndex(cat, dir2, dir2, true)

	// Resolve using the *saved* (Tahoe-specific) driver label, exactly as
	// deploy_darwin.go's own resolveDriver would with an already-committed
	// row loaded from a saved configuration.
	resolved, ok := MacVariantForDeploy(catalinaIndex, "Canon", "TestVendor Model X", savedDriverLabel)
	if !ok {
		t.Fatal("expected a match on the Catalina endpoint too")
	}
	if !strings.Contains(resolved.PackagePath, "catalina") {
		t.Errorf("expected deploy on the Catalina endpoint to automatically switch to the Catalina-scoped package, got %q (the saved Tahoe label was %q)", resolved.PackagePath, savedDriverLabel)
	}
}

func TestMacVariantForDeploy_ExactLabelWins(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	variant, ok := MacVariantForDeploy(index, "Canon", "TestVendor Model X", "TestVendor Model X (PostScript)")
	if !ok {
		t.Fatal("expected a match")
	}
	if variant.Language != "PS" {
		t.Errorf("expected the PS variant (explicit label commit), got %q", variant.Language)
	}
}

func TestMacVariantForDeploy_NoDriverCommitFallsBackToPreferenceOrder(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	variant, ok := MacVariantForDeploy(index, "Canon", "TestVendor Model X", "")
	if !ok {
		t.Fatal("expected a match")
	}
	if variant.Language != "UFRII" {
		t.Errorf("expected the UFRII variant (first in Canon's own preference order), got %q", variant.Language)
	}
}

func TestMacVariantForDeploy_ModelTypedWithDifferentCasingStillMatches(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)

	variant, ok := MacVariantForDeploy(index, "Canon", "testvendor model x", "")
	if !ok {
		t.Fatal("expected a fold-match against \"TestVendor Model X\" despite the casing difference")
	}
	if variant.Language != "UFRII" {
		t.Errorf("expected the UFRII variant, got %q", variant.Language)
	}
}

func TestMacVariantForDeploy_UnknownModelReportsNotFound(t *testing.T) {
	cat := testModelCatalog(t)
	dir := t.TempDir()
	index, _ := BuildMacModelIndex(cat, dir, dir, true)
	if _, ok := MacVariantForDeploy(index, "Canon", "Totally Unknown Model", ""); ok {
		t.Error("expected no match for a model absent from the index - caller should fall back to ResolveMacFamily/choosePPD")
	}
}
