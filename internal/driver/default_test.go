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
