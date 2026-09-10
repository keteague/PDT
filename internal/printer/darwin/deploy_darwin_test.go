package darwin

import "testing"

// Fixture drawn from a real install: a single Kyocera driver install
// registered both "Kyocera TASKalfa MZ6001ci" and "Kyocera TASKalfa MZ6001i"
// (confirmed live against this exact machine's own PPD directory) - close
// enough to each other that choosePPD's own fuzzy scoring needs to actually
// tell them apart correctly, not just pick whichever sorts first.
var realKyoceraPPDs = []string{
	"Kyocera TASKalfa MZ6001ci",
	"Kyocera TASKalfa MZ6001i",
	"Kyocera ECOSYS MA4500ifx",
}

func TestChoosePPD_SingleCandidateNeverAmbiguous(t *testing.T) {
	chosen, ambiguous := choosePPD([]string{"Kyocera TASKalfa MZ6001ci"}, "")
	if chosen != "Kyocera TASKalfa MZ6001ci" || ambiguous {
		t.Errorf("choosePPD(single, \"\") = (%q, %v), want (%q, false)", chosen, ambiguous, "Kyocera TASKalfa MZ6001ci")
	}
}

func TestChoosePPD_EmptyModelIsAmbiguousAmongMultiple(t *testing.T) {
	chosen, ambiguous := choosePPD(realKyoceraPPDs, "")
	if chosen != realKyoceraPPDs[0] || !ambiguous {
		t.Errorf("choosePPD(multiple, \"\") = (%q, %v), want (%q, true)", chosen, ambiguous, realKyoceraPPDs[0])
	}
}

func TestChoosePPD_ExactModelConfidentlyPicksTheRightOne(t *testing.T) {
	chosen, ambiguous := choosePPD(realKyoceraPPDs, "ECOSYS MA4500ifx")
	if chosen != "Kyocera ECOSYS MA4500ifx" || ambiguous {
		t.Errorf("choosePPD(model=%q) = (%q, %v), want (%q, false)", "ECOSYS MA4500ifx", chosen, ambiguous, "Kyocera ECOSYS MA4500ifx")
	}
}

func TestChoosePPD_ModelSubstringOfTwoDoesNotActuallyTie(t *testing.T) {
	// "MZ6001" is a substring of both the ci and non-ci real PPD names, but
	// FuzzyMatchScore's own tie-break (shorter matched text wins) means these
	// don't actually score equally - confirmed here rather than assumed:
	// "...MZ6001i" (shorter) deterministically outscores "...MZ6001ci"
	// (longer), so choosePPD reports this confident, not ambiguous. Worth
	// knowing: a model query that's a plain substring of two same-family PPD
	// names will always resolve to whichever one has the shorter overall
	// name, not a coin flip - true ambiguity (see the next test) only
	// happens when two candidates are exactly the same length.
	chosen, ambiguous := choosePPD(realKyoceraPPDs, "MZ6001")
	if ambiguous {
		t.Errorf("choosePPD(model=%q) ambiguous = true, want false (FuzzyMatchScore's length tie-break already picks a winner)", "MZ6001")
	}
	if chosen != "Kyocera TASKalfa MZ6001i" {
		t.Errorf("choosePPD(model=%q) = %q, want %q (shorter of the two matching names)", "MZ6001", chosen, "Kyocera TASKalfa MZ6001i")
	}
}

func TestChoosePPD_TrueTieBetweenEqualLengthCandidatesIsAmbiguous(t *testing.T) {
	// A genuine tie needs candidates of identical length, so FuzzyMatchScore's
	// own "shorter wins" tie-break can't already have picked a winner - real
	// vendor PPD names essentially never land on this by chance (see the test
	// above), so this uses a constructed pair to exercise the ambiguous path
	// itself directly.
	candidates := []string{"AAAA MZ6001", "BBBB MZ6001"}
	chosen, ambiguous := choosePPD(candidates, "MZ6001")
	if !ambiguous {
		t.Errorf("choosePPD(equal-length tie) ambiguous = false, want true")
	}
	if chosen != candidates[0] && chosen != candidates[1] {
		t.Errorf("choosePPD(equal-length tie) = %q, want one of %v", chosen, candidates)
	}
}

func TestChoosePPD_ModelMatchingNothingFallsBackToFirst(t *testing.T) {
	chosen, ambiguous := choosePPD(realKyoceraPPDs, "zzz-no-such-model")
	if chosen != realKyoceraPPDs[0] || !ambiguous {
		t.Errorf("choosePPD(no-match) = (%q, %v), want (%q, true)", chosen, ambiguous, realKyoceraPPDs[0])
	}
}
