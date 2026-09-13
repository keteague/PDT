package darwin

import (
	"strings"
	"testing"
)

func TestBuildEnsureQueueArgv_RealPPDPathUsesDashPCapital(t *testing.T) {
	argv := buildEnsureQueueArgv("q1", "lpd://10.1.1.1/", "/Library/Printers/PPDs/Contents/Resources/foo.ppd", QueueOptions{})
	if !containsSeq(argv, "-P", "/Library/Printers/PPDs/Contents/Resources/foo.ppd") {
		t.Errorf("expected -P with the real PPD path, got %v", argv)
	}
	if containsArg(argv, "-m") {
		t.Errorf("did not expect -m for a real PPD path, got %v", argv)
	}
}

func TestBuildEnsureQueueArgv_EmptyPathFallsBackToEverywhere(t *testing.T) {
	argv := buildEnsureQueueArgv("q1", "lpd://10.1.1.1/", "", QueueOptions{})
	if !containsSeq(argv, "-m", "everywhere") {
		t.Errorf("expected -m everywhere for an empty ppdPath, got %v", argv)
	}
}

// TestBuildEnsureQueueArgv_GenericModelReferenceUsesDashM guards the real
// mechanism Apple's own bundled Generic PostScript/PCL drivers need -
// confirmed live via `lpinfo -m` (2026-09-13) that these are real CUPS
// model strings (`drv:///sample.drv/generic.ppd`,
// `drv:///sample.drv/generpcl.ppd`), passed to `lpadmin -m` directly, never
// `-P` (which expects a real file path on disk - there isn't one for
// either, CUPS generates the real PPD on demand as part of creating the
// queue).
func TestBuildEnsureQueueArgv_GenericModelReferenceUsesDashM(t *testing.T) {
	argv := buildEnsureQueueArgv("q1", "lpd://10.1.1.1/", "drv:///sample.drv/generic.ppd", QueueOptions{})
	if !containsSeq(argv, "-m", "drv:///sample.drv/generic.ppd") {
		t.Errorf("expected -m drv:///sample.drv/generic.ppd, got %v", argv)
	}
	if containsArg(argv, "-P") {
		t.Errorf("did not expect -P for a drv:/// model reference, got %v", argv)
	}
}

func TestIsGenericModelReference(t *testing.T) {
	tests := []struct {
		ppdPath string
		want    bool
	}{
		{"drv:///sample.drv/generic.ppd", true},
		{"drv:///sample.drv/generpcl.ppd", true},
		{"/Library/Printers/PPDs/Contents/Resources/foo.ppd", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isGenericModelReference(tt.ppdPath); got != tt.want {
			t.Errorf("isGenericModelReference(%q) = %v, want %v", tt.ppdPath, got, tt.want)
		}
	}
}

func containsArg(argv []string, s string) bool {
	for _, a := range argv {
		if a == s {
			return true
		}
	}
	return false
}

func containsSeq(argv []string, a, b string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == a && argv[i+1] == b {
			return true
		}
	}
	return false
}

func TestBuildEnsureQueueArgv_NeverMentionsBothPFlags(t *testing.T) {
	// Belt-and-suspenders: whichever mode wins, the other driver-selection
	// flag must never also appear - lpadmin would reject a command
	// specifying both -P and -m.
	for _, ppdPath := range []string{"", "drv:///sample.drv/generic.ppd", "/real/path.ppd"} {
		argv := buildEnsureQueueArgv("q1", "lpd://10.1.1.1/", ppdPath, QueueOptions{})
		joined := strings.Join(argv, " ")
		hasP := strings.Contains(joined, " -P ")
		hasM := strings.Contains(joined, " -m ")
		if hasP == hasM {
			t.Errorf("ppdPath=%q: expected exactly one of -P/-m, got argv=%v", ppdPath, argv)
		}
	}
}
