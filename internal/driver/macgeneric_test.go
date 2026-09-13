package driver

import "testing"

func TestGenericDriverCandidates_BlankFilterReturnsBoth(t *testing.T) {
	got := GenericDriverCandidates("")
	if len(got) != 2 {
		t.Fatalf("expected both generic options for a blank filter, got %v", got)
	}
	if got[0] != GenericPostScriptLabel || got[1] != GenericPCLLabel {
		t.Errorf("got %v, want [%q, %q]", got, GenericPostScriptLabel, GenericPCLLabel)
	}
}

func TestGenericDriverCandidates_FilterNarrows(t *testing.T) {
	got := GenericDriverCandidates("PCL")
	if len(got) != 1 || got[0] != GenericPCLLabel {
		t.Errorf("expected just the PCL option for filterText \"PCL\", got %v", got)
	}
}

func TestGenericDriverCandidates_NoMatchReturnsEmptyNotNil(t *testing.T) {
	got := GenericDriverCandidates("ZZZNoMatch")
	if got == nil {
		t.Error("expected an empty slice, not nil (crosses the Wails JSON bridge)")
	}
	if len(got) != 0 {
		t.Errorf("expected no matches, got %v", got)
	}
}

func TestGenericDriverModelByLabel(t *testing.T) {
	tests := []struct {
		label     string
		wantModel string
		wantOK    bool
	}{
		{GenericPostScriptLabel, GenericPostScriptModel, true},
		{GenericPCLLabel, GenericPCLModel, true},
		{"Canon iR C3000 Series (UFR II)", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		model, ok := GenericDriverModelByLabel(tt.label)
		if model != tt.wantModel || ok != tt.wantOK {
			t.Errorf("GenericDriverModelByLabel(%q) = (%q, %v), want (%q, %v)", tt.label, model, ok, tt.wantModel, tt.wantOK)
		}
	}
}
