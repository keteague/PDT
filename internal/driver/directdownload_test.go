package driver

import "testing"

func TestMatchDirectDownloadFamily_Windows(t *testing.T) {
	tests := []struct {
		manufacturer, driver, wantFamily string
		wantOK                           bool
	}{
		// Real, confirmed .inf-declared names (see directDownloadFamilies'
		// own doc comment for provenance).
		{"Canon", "Canon Generic Plus UFR II", "UFR II", true},
		{"Kyocera", "Kyocera FS-1100 KX", "KX", true},
		{"Kyocera", "Kyocera TASKalfa 8353ci KX", "KX", true},
		{"Ricoh", "RICOH PCL6 UniversalDriver V4.45", "PCL6", true},
		{"Ricoh", "PCL6 Driver for Universal Print", "PCL6", true},
		{"Ricoh", "RICOH PS UniversalDriver V4.45", "PS", true},
		{"Sharp", "SHARP UD3 PCL6", "UD3 PCL6", true},
		// Inferred (Canon's own "Generic Plus <X>" convention) - still
		// expected to match.
		{"Canon", "Canon Generic Plus PCL6", "PCL6", true},
		{"Canon", "Canon Generic Plus PS3", "PS", true},
		// A decorated multi-version label (Candidates' own real shape,
		// candidates.go) must still match - the family tokens are a
		// substring check, decoration or not.
		{"Kyocera", "Kyocera FS-1100 KX (v8.7.0422.0 - 2026-04-22, 64bit)", "KX", true},
		// No match: an unrelated real name.
		{"Canon", "Canon Generic Plus LIPS4", "", false},
		// HP's own confirmed real UPD name (defaultDriverTokens' own "PCL","6"
		// - see candidates_test.go) - added 2026-09-20.
		{"HP", "HP Universal Printing PCL 6", "PCL6", true},
		{"HP", "HP Universal Printing PS", "PS", true},
		// Xerox's own confirmed real GPD display name
		// (defaultDriverTokens' own "GPD","PCL","6") - added 2026-09-20.
		{"Xerox", "Xerox GPD PCL6 V5.1076.4.0", "PCL", true},
	}
	for _, tt := range tests {
		family, ok := MatchDirectDownloadFamily(tt.manufacturer, DirectDownloadPlatformWindows, tt.driver)
		if ok != tt.wantOK || family != tt.wantFamily {
			t.Errorf("MatchDirectDownloadFamily(%q, Windows, %q) = (%q, %v), want (%q, %v)",
				tt.manufacturer, tt.driver, family, ok, tt.wantFamily, tt.wantOK)
		}
	}
}

func TestMatchDirectDownloadFamily_Mac(t *testing.T) {
	tests := []struct {
		manufacturer, label, wantFamily string
		wantOK                          bool
	}{
		// Real macVariantLabel shapes (macmodel.go's own macLanguageDisplayNames).
		{"Canon", "Canon iR-ADV C5840/5850 (UFR II)", "UFR II", true},
		{"Canon", "Canon iR-ADV C5840/5850 (PostScript)", "PS", true},
		{"Canon", "Canon iR-ADV C5840/5850 (Generic PPD)", "PPD", true},
		// Sole-family manufacturers match unconditionally, regardless of the
		// label's own text - there's nothing more specific to check. Kyocera's
		// real label actually does carry its language ("(KPDL)" - Ken,
		// 2026-09-23), but Sharp/Xerox below carry a fully generic "(Driver)"
		// - either way, matching doesn't depend on it.
		{"Kyocera", "Kyocera CS 255c (KPDL)", "KPDL", true},
		{"Ricoh", "RICOH IM C3000 (PostScript)", "PPD", true},
		{"Sharp", "SHARP MX-C55 (Driver)", "PPD", true},
		// Xerox's own sole mac family - added 2026-09-20 - matches
		// unconditionally too.
		{"Xerox", "Xerox C300 Color Printer, 5.19.3 (Driver)", "PPD", true},
		// No entry at all for this manufacturer/platform.
		{"Nonexistent Brand", "anything", "", false},
	}
	for _, tt := range tests {
		family, ok := MatchDirectDownloadFamily(tt.manufacturer, DirectDownloadPlatformMac, tt.label)
		if ok != tt.wantOK || family != tt.wantFamily {
			t.Errorf("MatchDirectDownloadFamily(%q, macOS, %q) = (%q, %v), want (%q, %v)",
				tt.manufacturer, tt.label, family, ok, tt.wantFamily, tt.wantOK)
		}
	}
}

func TestDirectDownloadManufacturers_OnlyThoseWithEntries(t *testing.T) {
	got := DirectDownloadManufacturers()
	want := map[string]bool{
		"Canon": true, "Kyocera": true, "Ricoh": true, "Sharp": true,
		"HP": true, "Lexmark": true, "Toshiba": true, "Xerox": true, "Konica Minolta": true,
	}
	if len(got) != len(want) {
		t.Fatalf("DirectDownloadManufacturers() = %v, want exactly %v", got, want)
	}
	for _, m := range got {
		if !want[m] {
			t.Errorf("unexpected manufacturer %q in DirectDownloadManufacturers()", m)
		}
	}
}

func TestDirectDownloadFamiliesFor_UnknownManufacturerIsNil(t *testing.T) {
	if got := DirectDownloadFamiliesFor("Nonexistent Brand", DirectDownloadPlatformWindows); got != nil {
		t.Errorf("DirectDownloadFamiliesFor(Nonexistent Brand, Windows) = %v, want nil (no Direct Downloads entry)", got)
	}
}

func TestDirectDownloadFamiliesFor_RealOrder(t *testing.T) {
	got := DirectDownloadFamiliesFor("Canon", DirectDownloadPlatformWindows)
	want := []string{"PCL6", "PS", "UFR II"}
	if len(got) != len(want) {
		t.Fatalf("DirectDownloadFamiliesFor(Canon, Windows) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("DirectDownloadFamiliesFor(Canon, Windows)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
