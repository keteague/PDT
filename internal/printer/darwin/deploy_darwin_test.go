package darwin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"PDT/internal/driver"
	"PDT/internal/printer"
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

// TestEnsureInstalledOnce_SkipsAlreadyCachedPackage guards against a real,
// confirmed-live regression: installVariant/resolveDriver used to call
// EnsureDriverInstalled unconditionally for every row, even when an earlier
// row in the exact same deploy run had already installed the identical
// package - 4m38s per install against a real Canon UFR II package, so 3
// identical test rows cost ~14 minutes of pure redundant work (plus extra
// password prompts, since each multi-minute install routinely outlasts
// macOS's own few-minutes authorization cache). Pre-populating
// installedThisRun and confirming the cached PPD list comes straight back -
// without ever reaching EnsureDriverInstalled's own real
// LocatePkg/installer/osascript chain - is what proves the short-circuit
// actually short-circuits, not just that the map got written to.
func TestEnsureInstalledOnce_SkipsAlreadyCachedPackage(t *testing.T) {
	d := NewDeployer(driver.MacCatalog{}, nil)
	const fakePath = "/nonexistent/Canon/UFRII_v10.19.25_mac.dmg"
	want := []string{"/Library/Printers/PPDs/Contents/Resources/CNPZUIRAC5840ZU.ppd.gz"}
	d.installedThisRun[fakePath] = want

	log := &printer.Logger{}
	got, err := d.ensureInstalledOnce(context.Background(), &driver.ResolvedMacPackage{Path: fakePath}, log)
	if err != nil {
		// A real (non-cached) attempt against a path that doesn't exist
		// would fail - any error here means the cache was bypassed.
		t.Fatalf("ensureInstalledOnce returned an error for an already-cached package (cache was bypassed): %v", err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("ensureInstalledOnce = %v, want the cached %v", got, want)
	}
}

func TestLpdDeviceURI(t *testing.T) {
	tests := []struct {
		name      string
		queueName string
		want      string
	}{
		{"blank - most manufacturers", "", "lpd://10.1.1.50/"},
		{"HP's raw default", "raw", "lpd://10.1.1.50/raw"},
		{"Xerox's lp default", "lp", "lpd://10.1.1.50/lp"},
		{"stray leading slash trimmed", "/raw", "lpd://10.1.1.50/raw"},
		{"stray trailing slash trimmed", "raw/", "lpd://10.1.1.50/raw"},
		{"surrounding whitespace trimmed", "  raw  ", "lpd://10.1.1.50/raw"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lpdDeviceURI("10.1.1.50", tt.queueName); got != tt.want {
				t.Errorf("lpdDeviceURI(%q, %q) = %q, want %q", "10.1.1.50", tt.queueName, got, tt.want)
			}
		})
	}
}

func TestSanitizeCUPSQueueName(t *testing.T) {
	tests := []struct{ name, want string }{
		{"Copy Room", "Copy_Room"},
		{"Front-Desk", "Front-Desk"},
		{"A/B Printer", "A_B_Printer"},
		{"Room #3", "Room__3"},
		{"NoSpaces", "NoSpaces"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := sanitizeCUPSQueueName(tt.name); got != tt.want {
			t.Errorf("sanitizeCUPSQueueName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
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
