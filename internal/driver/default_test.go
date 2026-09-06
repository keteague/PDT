package driver

import "testing"

func TestDefaultDriverNameFor(t *testing.T) {
	cat := testCatalog(t)

	cases := []struct {
		manufacturer string
		want         string
	}{
		{"Canon", "Canon Generic Plus UFR II"},
		{"HP", "HP Universal Printing PCL 6"},
		{"Ricoh", "RICOH PCL6 UniversalDriver V4.45"},
		{"Sharp", "SHARP UD3 PCL6"},
		{"Toshiba", "TOSHIBA Universal Printer 2"},
		{"Xerox", "Xerox GPD PCL6 V5.1076.4.0"},
		{"Konica Minolta", "KONICA MINOLTA Universal PCL v3.9.13"},
		{"Lexmark", "Lexmark Universal v2"},
		{"Kyocera", ""}, // no rule defined for Kyocera
	}
	for _, c := range cases {
		if got := DefaultDriverNameFor(cat, c.manufacturer); got != c.want {
			t.Errorf("DefaultDriverNameFor(%q) = %q, want %q", c.manufacturer, got, c.want)
		}
	}
}

func TestRicohVagueNamesFilteredFromCatalog(t *testing.T) {
	cat := testCatalog(t)
	ricoh := cat["Ricoh"]

	if _, ok := ricoh["PCL6 Driver for Universal Print"]; ok {
		t.Error("vague, manufacturer-less Ricoh alias should have been filtered out of the catalog entirely")
	}
	if _, ok := ricoh["PS Driver for Universal Print"]; ok {
		t.Error("vague, manufacturer-less Ricoh alias should have been filtered out of the catalog entirely")
	}
	if _, ok := ricoh["RICOH PCL6 UniversalDriver V4.45"]; !ok {
		t.Error("expected the RICOH-branded PCL6 driver name to remain in the catalog")
	}
	if _, ok := ricoh["RICOH PS UniversalDriver V4.45"]; !ok {
		t.Error("expected the RICOH-branded PS driver name to remain in the catalog")
	}
}

// TestDefaultDriverNameFor_PrefersVersionedNameOnDateTie confirms the
// Konica Minolta case specifically: two catalog names for the exact same
// underlying driver (same DriverVer, so an exact date tie) - the plain
// "KONICA MINOLTA Universal PCL" and the version-suffixed
// "...Universal PCL v3.9.13". Alphabetically the plain name sorts first
// (it's a prefix of the other), so this only passes if the tie-break
// actually prefers the versioned name, not just falls through to
// alphabetical.
func TestDefaultDriverNameFor_PrefersVersionedNameOnDateTie(t *testing.T) {
	cat := testCatalog(t)
	km := cat["Konica Minolta"]
	if _, ok := km["KONICA MINOLTA Universal PCL"]; !ok {
		t.Fatal("expected the plain Konica Minolta name to be present in the catalog too (not filtered out - it's still a valid, selectable candidate)")
	}
	if got, want := DefaultDriverNameFor(cat, "Konica Minolta"), "KONICA MINOLTA Universal PCL v3.9.13"; got != want {
		t.Errorf("DefaultDriverNameFor(Konica Minolta) = %q, want %q", got, want)
	}
}

// TestBuildCatalog_ManufacturerFolderNameIgnoresSpaces confirms
// testdata/KonicaMinolta (no space) is still attributed to the
// "Konica Minolta" (with a space) manufacturer key - matches the real
// Drivers folder, where the on-disk folder name omits the space.
func TestBuildCatalog_ManufacturerFolderNameIgnoresSpaces(t *testing.T) {
	cat := testCatalog(t)
	if len(cat["Konica Minolta"]) == 0 {
		t.Error("expected the testdata/KonicaMinolta folder to be scanned into the \"Konica Minolta\" catalog entry, but it's empty")
	}
}

// TestXeroxGenericAliasIsSelectableButNotDefault: unlike Ricoh's vague alias
// (filtered out of the catalog entirely, since nothing about it identifies
// the vendor), Xerox's "Global Print Driver" name does carry "Xerox" in it -
// it should stay in the catalog as a real, selectable candidate, just not be
// the Defaults panel's own pre-selected default (that's the GPD-branded,
// versioned name, per DefaultDriverNameFor's own token match).
func TestXeroxGenericAliasIsSelectableButNotDefault(t *testing.T) {
	cat := testCatalog(t)
	xerox := cat["Xerox"]
	if _, ok := xerox["Xerox Global Print Driver PCL6"]; !ok {
		t.Error("expected the generic Xerox alias to remain selectable in the catalog")
	}
	if got, want := DefaultDriverNameFor(cat, "Xerox"), "Xerox GPD PCL6 V5.1076.4.0"; got != want {
		t.Errorf("DefaultDriverNameFor(Xerox) = %q, want %q", got, want)
	}
}
