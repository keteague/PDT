package driver

import "testing"

func TestModelFromDriverName(t *testing.T) {
	cases := []struct{ mfg, driver, want string }{
		{"Kyocera", "Kyocera FS-1100 KX", "FS-1100"},
		{"Kyocera", "Kyocera TASKalfa 8353ci KX", "TASKalfa 8353ci"},
		{"Canon", "Canon Generic Plus UFR II", ""},
		{"HP", "HP Universal Printing PCL 6", ""},
	}
	for _, c := range cases {
		if got := ModelFromDriverName(c.mfg, c.driver); got != c.want {
			t.Errorf("ModelFromDriverName(%q, %q) = %q, want %q", c.mfg, c.driver, got, c.want)
		}
	}
}

func TestBuildModelIndex(t *testing.T) {
	cat := testCatalog(t)
	index := BuildModelIndex(cat)
	names, ok := index["Kyocera"]["FS-1100"]
	if !ok || len(names) == 0 {
		t.Fatalf("expected Kyocera model index to have an FS-1100 entry, got %v", index["Kyocera"])
	}
	if names[0] != "Kyocera FS-1100 KX" {
		t.Errorf("model index entry = %q, want 'Kyocera FS-1100 KX'", names[0])
	}
	if _, ok := index["Canon"]["Generic Plus UFR II"]; ok {
		t.Error("Canon driver names should not be indexed by model (not model-specific)")
	}
}

func TestModels_EmptyNotNilForManufacturerWithNoModelData(t *testing.T) {
	cat := testCatalog(t)
	index := BuildModelIndex(cat)
	got := Models(index, "Canon", "")
	if len(got) != 0 {
		t.Errorf("Models(Canon) = %v, want empty - Canon driver names aren't model-specific, so there's no lookup to offer", got)
	}
	if got == nil {
		t.Error("Models(Canon) = nil, want a non-nil empty slice - nil marshals to JSON null, which the frontend combobox calls .map()/.length on with no defensive fallback")
	}
}

func TestModels_AlphabeticalWhenFilterTextEmpty(t *testing.T) {
	index := map[string]map[string][]string{
		"Kyocera": {
			"TASKalfa 8353ci": {"Kyocera TASKalfa 8353ci KX"},
			"FS-1100":         {"Kyocera FS-1100 KX"},
			"ECOSYS M3655idn": {"Kyocera ECOSYS M3655idn KX"},
		},
	}
	got := Models(index, "Kyocera", "")
	want := []string{"ECOSYS M3655idn", "FS-1100", "TASKalfa 8353ci"}
	if len(got) != len(want) {
		t.Fatalf("Models(Kyocera, \"\") = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Models(Kyocera, \"\")[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestModels_FiltersAndRanksByFuzzyMatch(t *testing.T) {
	index := map[string]map[string][]string{
		"Kyocera": {
			"TASKalfa 8353ci": {"Kyocera TASKalfa 8353ci KX"},
			"FS-1100":         {"Kyocera FS-1100 KX"},
			"ECOSYS M3655idn": {"Kyocera ECOSYS M3655idn KX"},
		},
	}
	got := Models(index, "Kyocera", "8353")
	if len(got) != 1 || got[0] != "TASKalfa 8353ci" {
		t.Errorf("Models(Kyocera, \"8353\") = %v, want [\"TASKalfa 8353ci\"]", got)
	}
	if got := Models(index, "Kyocera", "zzz-no-match"); len(got) != 0 {
		t.Errorf("Models(Kyocera, \"zzz-no-match\") = %v, want no matches", got)
	}
}
