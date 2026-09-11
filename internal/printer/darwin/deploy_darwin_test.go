package darwin

import (
	"os"
	"path/filepath"
	"testing"
)

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

// TestChoosePPD_MatchesRealCanonStyleCrypticFilenameByNickNameContent proves
// the actual bug this was built to fix: real Canon PPD filenames are cryptic
// codes ("CNPZUIRAC5840ZU.ppd") with no matchable relationship to how a
// technician would type the model ("iR-ADV C5840") - confirmed live that
// FuzzyMatchScore(filename, "iR-ADV C5840") is a hard -1 for a real Canon
// filename, since its subsequence fallback fails outright on the "-" and " "
// characters the filename never contains. choosePPD has to read each
// candidate's own *NickName (driver.ReadPPDNickName) to have any chance of
// matching a package with cryptic-but-real vendor filenames like this.
func TestChoosePPD_MatchesRealCanonStyleCrypticFilenameByNickNameContent(t *testing.T) {
	dir := t.TempDir()
	write := func(name, nickName string) string {
		path := filepath.Join(dir, name)
		content := "*PPD-Adobe: \"4.3\"\n*NickName: \"" + nickName + "\"\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return path
	}
	// Real cryptic Canon-style filenames, confirmed live to score a hard -1
	// against "iR-ADV C5840" if matched by filename alone.
	wrongModel := write("CNPZUIRAC5250ZU.ppd", "Canon iR-ADV C5250/5255")
	rightModel := write("CNPZUIRAC5840ZU.ppd", "Canon iR-ADV C5840/5850")

	chosen, ambiguous := choosePPD([]string{wrongModel, rightModel}, "iR-ADV C5840")
	if chosen != rightModel {
		t.Errorf("choosePPD matched %q, want the real C5840 PPD %q - filename-only matching would have failed to distinguish either (or both) of these", chosen, rightModel)
	}
	if ambiguous {
		t.Errorf("expected a confident match (only one candidate's NickName contains \"C5840\"), got ambiguous=true")
	}
}
