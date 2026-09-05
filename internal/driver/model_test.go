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
