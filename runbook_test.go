package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunbookPortType(t *testing.T) {
	cases := []struct {
		name string
		p    RunbookPrinter
		want string
	}{
		{"standard tcp/ip", RunbookPrinter{IP: "10.1.58.32"}, "Standard TCP/IP on 10.1.58.32"},
		{"lpd queue wins over plain ip", RunbookPrinter{IP: "10.1.58.32", LPDQueueName: "raw"}, "LPR on 10.1.58.32 (queue: raw)"},
		{"use existing port wins over everything", RunbookPrinter{IP: "10.1.58.32", LPDQueueName: "raw", UseExistingPort: true}, "Use existing port"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runbookPortType(c.p); got != c.want {
				t.Errorf("runbookPortType(%+v) = %q, want %q", c.p, got, c.want)
			}
		})
	}
}

func TestRunbookDownloadURL(t *testing.T) {
	urls := map[string]map[string]map[string]string{
		"Canon": {
			"Windows": {"UFR II": "https://downloads.canon.com/Generic_Plus_UFRII_v3.50.zip"},
		},
	}
	if got := runbookDownloadURL(urls, "Canon", "Windows", ""); got != "" {
		t.Errorf("expected blank for a blank driver name, got %q", got)
	}
	if got := runbookDownloadURL(urls, "Canon", "Windows", "Canon Generic Plus UFR II"); got != "https://downloads.canon.com/Generic_Plus_UFRII_v3.50.zip" {
		t.Errorf("expected the configured URL, got %q", got)
	}
	if got := runbookDownloadURL(urls, "Canon", "macOS", "Canon Generic Plus UFR II"); got != "" {
		t.Errorf("expected blank when nothing is configured for that platform, got %q", got)
	}
	if got := runbookDownloadURL(urls, "HP", "Windows", "HP Universal Printing PCL 6"); got != "" {
		t.Errorf("expected blank for a manufacturer with no Direct Downloads entries at all, got %q", got)
	}
}

// TestBuildRunbookText_SingleMFDHasNoIDLine is the direct regression test
// for GitHub issue #15's own explicit shape: a single-printer survey never
// shows an "ID:" line at all, matching the simpler template.
func TestBuildRunbookText_SingleMFDHasNoIDLine(t *testing.T) {
	printers := []RunbookPrinter{
		{Name: "Workroom South", IP: "10.1.58.32", Manufacturer: "Canon", Driver: "Canon Generic Plus UFR II", WindowsEnabled: true},
	}
	text := buildRunbookText(printers, nil, nil)

	if strings.Contains(text, "* ID:") {
		t.Errorf("expected no ID line for a single printer, got:\n%s", text)
	}
	if !strings.Contains(text, "* Print Object Name: Workroom South") {
		t.Errorf("expected the printer's own name, got:\n%s", text)
	}
	if !strings.Contains(text, "Standard TCP/IP on 10.1.58.32") {
		t.Errorf("expected the derived port type, got:\n%s", text)
	}
	if !strings.Contains(text, "Windows: Canon Generic Plus UFR II") {
		t.Errorf("expected the Windows driver line, got:\n%s", text)
	}
	if !strings.Contains(text, "macOS: \n") {
		t.Errorf("expected a blank macOS driver line (MacEnabled is false), got:\n%s", text)
	}
	if !strings.Contains(text, "*** PRINT DRIVER INSTALLATION") || !strings.Contains(text, "Number of endpoints:") {
		t.Errorf("expected the fixed header/footer to both be present, got:\n%s", text)
	}
}

// TestBuildRunbookText_MultipleMFDsShowIDLineEvenWhenBlank is the direct
// regression test for Ken's own explicit answer: once there's more than one
// printer, every entry gets an "ID:" line, even one left blank.
func TestBuildRunbookText_MultipleMFDsShowIDLineEvenWhenBlank(t *testing.T) {
	printers := []RunbookPrinter{
		{ID: "1-1", Name: "Workroom South", IP: "10.1.58.32", Manufacturer: "Canon", Driver: "Canon Generic Plus UFR II", WindowsEnabled: true},
		{ID: "", Name: "Workroom East", IP: "10.1.58.33", Manufacturer: "Canon", Driver: "Canon Generic Plus UFR II", WindowsEnabled: true},
	}
	text := buildRunbookText(printers, nil, nil)

	if !strings.Contains(text, "* ID: 1-1\n") {
		t.Errorf("expected the first printer's own ID, got:\n%s", text)
	}
	if !strings.Contains(text, "* ID: \n") {
		t.Errorf("expected a blank (but present) ID line for the second printer, got:\n%s", text)
	}
	if !strings.Contains(text, "Workroom South") || !strings.Contains(text, "Workroom East") {
		t.Errorf("expected both printers' own names, got:\n%s", text)
	}
}

// TestBuildRunbookText_ExplicitMacDriverPinWinsOverResolver proves a row
// carrying its own explicit macOS Driver commitment (GitHub issue #16 - the
// new modal, auto-filled but overridable) is shown and matched against
// Direct Downloads as-is, never re-derived from a fresh catalog guess even
// when one is available.
func TestBuildRunbookText_ExplicitMacDriverPinWinsOverResolver(t *testing.T) {
	resolver := func(mfg, model string) (string, bool) {
		t.Errorf("resolver should never be called when MacDriver is already pinned")
		return "", false
	}
	urls := map[string]map[string]map[string]string{
		"Canon": {"macOS": {"PS": "https://downloads.canon.com/PS_v4.17.24_mac.zip"}},
	}
	printers := []RunbookPrinter{
		{
			Name: "Front Desk", IP: "10.1.58.26", Manufacturer: "Canon", Model: "imageFORCE C331F",
			MacDriver: "imageFORCE C331F (PostScript)", MacEnabled: true,
		},
	}
	text := buildRunbookText(printers, resolver, urls)

	if !strings.Contains(text, "macOS: imageFORCE C331F (PostScript)\n") {
		t.Errorf("expected the pinned macOS driver shown exactly as chosen, got:\n%s", text)
	}
	if !strings.Contains(text, "* macOS driver download link: https://downloads.canon.com/PS_v4.17.24_mac.zip") {
		t.Errorf("expected the pinned driver's own download link, got:\n%s", text)
	}
	if !strings.Contains(text, "Windows: \n") {
		t.Errorf("expected a blank Windows line (WindowsEnabled is false), got:\n%s", text)
	}
}

// TestBuildRunbookText_FallsBackToResolverWhenMacEnabledButNotYetPinned
// covers the defensive catch-all path: macOS is checked but MacDriver is
// still blank (shouldn't normally happen once the modal auto-fills it, but
// must degrade gracefully rather than silently showing nothing useful).
func TestBuildRunbookText_FallsBackToResolverWhenMacEnabledButNotYetPinned(t *testing.T) {
	resolver := func(mfg, model string) (string, bool) {
		if mfg == "Canon" && model == "imageFORCE C331F" {
			return "imageFORCE C331F (UFR II)", true
		}
		return "", false
	}
	urls := map[string]map[string]map[string]string{
		"Canon": {"macOS": {"UFR II": "https://downloads.canon.com/UFRII_v10.19.25_mac.zip"}},
	}
	printers := []RunbookPrinter{
		{Name: "Front Desk", IP: "10.1.58.26", Manufacturer: "Canon", Model: "imageFORCE C331F", MacEnabled: true},
	}
	text := buildRunbookText(printers, resolver, urls)

	if !strings.Contains(text, "macOS: Canon imageFORCE C331F\n") {
		t.Errorf("expected the clean \"Manufacturer Model\" display text, not the raw family label, got:\n%s", text)
	}
	if !strings.Contains(text, "* macOS driver download link: https://downloads.canon.com/UFRII_v10.19.25_mac.zip") {
		t.Errorf("expected the resolved macOS download link (matched via the raw family label), got:\n%s", text)
	}
}

// TestBuildRunbookText_UncheckedPlatformAlwaysBlank proves WindowsEnabled/
// MacEnabled are respected even when the underlying Driver/MacDriver field
// happens to have a stale value sitting in it (e.g. the checkbox was
// unchecked after a value was once typed) - an unchecked platform must never
// show a driver or attempt a download-link lookup.
func TestBuildRunbookText_UncheckedPlatformAlwaysBlank(t *testing.T) {
	printers := []RunbookPrinter{
		{
			Name: "Front Desk", IP: "10.1.58.26", Manufacturer: "Canon", Model: "imageFORCE C331F",
			Driver: "Canon Generic Plus UFR II", WindowsEnabled: false,
			MacDriver: "imageFORCE C331F (UFR II)", MacEnabled: false,
		},
	}
	text := buildRunbookText(printers, nil, nil)

	if !strings.Contains(text, "Windows: \n") {
		t.Errorf("expected a blank Windows line despite a stale Driver value, got:\n%s", text)
	}
	if !strings.Contains(text, "macOS: \n") {
		t.Errorf("expected a blank macOS line despite a stale MacDriver value, got:\n%s", text)
	}
}

func TestWriteRunbookFile(t *testing.T) {
	dir := t.TempDir()
	path, err := writeRunbookFile(dir, "18455-1", "some runbook text")
	if err != nil {
		t.Fatalf("writeRunbookFile: %v", err)
	}
	if filepath.Base(path) != "18455-1-runbook.txt" {
		t.Errorf("path = %q, want a file named \"18455-1-runbook.txt\"", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back %s: %v", path, err)
	}
	if string(data) != "some runbook text" {
		t.Errorf("file content = %q, want %q", data, "some runbook text")
	}
}
