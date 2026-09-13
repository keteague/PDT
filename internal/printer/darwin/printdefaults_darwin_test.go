package darwin

import (
	"strings"
	"testing"
)

// Both fixture lines below are copied verbatim from `lpoptions -p
// Jenks_Kyocera -l` against a real, already-deployed Kyocera queue on this
// machine - not synthetic guesses at PPD option formatting.
const realDuplexLine = `Duplex/Duplexing: None DuplexTumble *DuplexNoTumble`
const realColorModelLine = `ColorModel/Color mode: *CMYK Gray`

// Both fixture lines below are copied verbatim from `lpoptions -p Main_Hall
// -l` against a real, freshly-deployed Canon iR-ADV C5840/5850 (UFR II)
// queue on this machine - confirmed live that Canon's own PPD uses "CN"-
// prefixed keywords instead of the CUPS-standard Duplex/ColorModel a
// Kyocera PPD uses, which SetPrintDefaults used to miss entirely (silently
// logging "declares no Duplex/ColorModel option" and leaving both at
// whatever CUPS' own driver default was).
const realCanonDuplexLine = `CNDuplex/Print Style: None *DuplexFront Booklet`
const realCanonColorModeLine = `CNColorMode/Color Mode: mono *color`

func TestFindOption_MatchesCanonCNPrefixedDuplexExactly(t *testing.T) {
	opts := []ppdOption{parseOneLine(t, realCanonDuplexLine)}
	got, ok := findOption(opts, "duplex", "cnduplex")
	if !ok || got.keyword != "CNDuplex" {
		t.Errorf("findOption(duplex, cnduplex) = %+v, %v, want CNDuplex, true", got, ok)
	}
}

func TestFindOption_MatchesCanonCNColorModeExactly(t *testing.T) {
	opts := []ppdOption{parseOneLine(t, realCanonColorModeLine)}
	got, ok := findOption(opts, "colormodel", "cncolormode")
	if !ok || got.keyword != "CNColorMode" {
		t.Errorf("findOption(colormodel, cncolormode) = %+v, %v, want CNColorMode, true", got, ok)
	}
}

// TestFindOption_DoesNotMatchOtherRealCanonColorOptions guards against a
// looser substring/suffix match instead of the exact match actually used -
// these are all real option keywords the same Canon PPD also declares, none
// of which are the color/mono switch itself (an ICC profile picker, a
// halftone pattern, a color-count picker, and two numeric adjustment
// sliders) - a substring/suffix match would risk grabbing one of these
// instead and setting a nonsense value on it.
func TestFindOption_DoesNotMatchOtherRealCanonColorOptions(t *testing.T) {
	opts := []ppdOption{
		{keyword: "CNColorSyncICC", choices: []string{"DefaultFile"}},
		{keyword: "CNColorHalftone", choices: []string{"pattern6"}},
		{keyword: "CNNumberOfColors", choices: []string{"FullColor"}},
		{keyword: "CNColorToUseWithBlack", choices: []string{"Red"}},
		{keyword: "CNXColorAdjustment", choices: []string{"6"}},
		{keyword: "CNYColorAdjustment", choices: []string{"6"}},
	}
	if got, ok := findOption(opts, "colormodel", "cncolormode"); ok {
		t.Errorf("findOption(colormodel, cncolormode) unexpectedly matched %+v, want no match among unrelated Canon color options", got)
	}
}

// TestFindOption_DoesNotMatchCNProcessColorModeInsteadOfCNColorMode guards
// against the exact real, confirmed-live bug this exact-match version
// replaced a suffix-based one to fix: a real Canon PPD (a different UFR II
// model than realCanonColorModeLine's own fixture) declares BOTH
// *CNProcessColorMode (an unrelated boolean, "Print Mixed Color/B&W
// Documents at High Speed", choices False/True) AND *CNColorMode (the real
// switch) - both end in "ColorMode", so a suffix match picked whichever came
// first in this PPD's own option order (CNProcessColorMode, for this exact
// model) instead of the real one, silently leaving color mode untouched
// while warning about the wrong option's own lack of a mono choice.
// CNProcessColorMode is listed FIRST here specifically to reproduce that
// ordering.
func TestFindOption_DoesNotMatchCNProcessColorModeInsteadOfCNColorMode(t *testing.T) {
	opts := []ppdOption{
		{keyword: "CNProcessColorMode", choices: []string{"False", "*True"}},
		{keyword: "CNColorMode", choices: []string{"mono", "*color"}},
	}
	got, ok := findOption(opts, "colormodel", "cncolormode")
	if !ok || got.keyword != "CNColorMode" {
		t.Errorf("findOption(colormodel, cncolormode) = %+v, %v, want CNColorMode, true", got, ok)
	}
}

func TestPickChoice_TwoSidedPicksDuplexFrontFromRealCanonDuplexChoices(t *testing.T) {
	got := parseOneLine(t, realCanonDuplexLine)
	// No "notumble" choice exists (Canon's own two-sided choice is just
	// "DuplexFront") - falls through to the "tumble"/"duplex" want, "none"
	// avoid tier, same as any other vendor with no NoTumble/Tumble split.
	choice, ok := pickChoice(got.choices, []string{"notumble"}, nil)
	if ok {
		t.Fatalf("pickChoice(notumble) unexpectedly matched %q against Canon's real choices %v", choice, got.choices)
	}
	choice, ok = pickChoice(got.choices, []string{"tumble", "duplex"}, []string{"none"})
	if !ok || choice != "DuplexFront" {
		t.Errorf("pickChoice (two-sided fallback) = %q, %v, want \"DuplexFront\", true", choice, ok)
	}
}

// Both raw PPD blocks below are copied verbatim from the same real,
// installed Canon PPD (`gunzip -c
// /Library/Printers/PPDs/Contents/Resources/CNPZUIRAC5840ZU.ppd.gz`) - the
// literal *OpenUI/*CloseUI declarations parsePPDOpenUIOptions reads directly
// off disk, one level closer to the source than the lpoptions-derived
// fixtures above (which lpoptions itself parses from the same lines).
const realCanonDuplexPPDBlock = `*OpenUI *CNDuplex/Print Style: PickOne
*DefaultCNDuplex: DuplexFront
*CNDuplex None/1-sided Printing: "<< >>setpagedevice"
*CNDuplex DuplexFront/2-sided Printing: "<< >>setpagedevice"
*CNDuplex Booklet/Booklet Printing: "<< >>setpagedevice"
*CloseUI: *CNDuplex`

const realCanonColorModePPDBlock = `*OpenUI *CNColorMode/Color Mode: PickOne
*DefaultCNColorMode: color
*CNColorMode mono/Black and White: "<< >>setpagedevice"
*CNColorMode color/Color: "<< >>setpagedevice"
*CloseUI: *CNColorMode`

func TestParsePPDOpenUIOptions_ParsesRealCanonDuplexBlock(t *testing.T) {
	opts := parsePPDOpenUIOptions(realCanonDuplexPPDBlock)
	if len(opts) != 1 || opts[0].keyword != "CNDuplex" {
		t.Fatalf("parsePPDOpenUIOptions = %+v, want one CNDuplex option", opts)
	}
	want := []string{"None", "DuplexFront", "Booklet"}
	if strings.Join(opts[0].choices, ",") != strings.Join(want, ",") {
		t.Errorf("choices = %v, want %v", opts[0].choices, want)
	}
}

func TestParsePPDOpenUIOptions_ParsesRealCanonColorModeBlock(t *testing.T) {
	opts := parsePPDOpenUIOptions(realCanonColorModePPDBlock)
	if len(opts) != 1 || opts[0].keyword != "CNColorMode" {
		t.Fatalf("parsePPDOpenUIOptions = %+v, want one CNColorMode option", opts)
	}
	want := []string{"mono", "color"}
	if strings.Join(opts[0].choices, ",") != strings.Join(want, ",") {
		t.Errorf("choices = %v, want %v", opts[0].choices, want)
	}
}

// TestParsePPDOpenUIOptions_ParsesBothBlocksTogetherAndIgnoresSurroundingLines
// exercises the real multi-option shape a whole PPD file actually has:
// several *OpenUI/*CloseUI blocks back to back, plus unrelated lines
// (*DefaultCNDuplex, a blank line) in between that must not be mistaken for
// a third option or leak into either block's own choice list.
func TestParsePPDOpenUIOptions_ParsesBothBlocksTogetherAndIgnoresSurroundingLines(t *testing.T) {
	text := realCanonDuplexPPDBlock + "\n\n" + realCanonColorModePPDBlock
	opts := parsePPDOpenUIOptions(text)
	if len(opts) != 2 {
		t.Fatalf("parsePPDOpenUIOptions returned %d options, want 2: %+v", len(opts), opts)
	}
	if opts[0].keyword != "CNDuplex" || opts[1].keyword != "CNColorMode" {
		t.Errorf("keywords = [%q, %q], want [CNDuplex, CNColorMode]", opts[0].keyword, opts[1].keyword)
	}
}

func TestDecidePrintDefaults_MatchesRealCanonPPDFileOptions(t *testing.T) {
	opts := parsePPDOpenUIOptions(realCanonDuplexPPDBlock + "\n" + realCanonColorModePPDBlock)

	toSet, warnings := decidePrintDefaults(opts, false /* two-sided */, true /* mono */, `row "Main Hall"`)
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings for a PPD that declares both options: %v", warnings)
	}
	want := map[string]bool{"CNDuplex=DuplexFront": true, "CNColorMode=mono": true}
	if len(toSet) != 2 || !want[toSet[0]] || !want[toSet[1]] {
		t.Errorf("toSet = %v, want exactly %v (order-insensitive)", toSet, want)
	}
}

func TestPickChoice_MonoAndColorFromRealCanonColorModeChoices(t *testing.T) {
	got := parseOneLine(t, realCanonColorModeLine)
	mono, ok := pickChoice(got.choices, []string{"gray", "grey", "mono", "black"}, nil)
	if !ok || mono != "mono" {
		t.Errorf("pickChoice (mono) = %q, %v, want \"mono\", true", mono, ok)
	}
	color, ok := pickChoice(got.choices, nil, []string{"gray", "grey", "mono", "black"})
	if !ok || color != "color" {
		t.Errorf("pickChoice (color) = %q, %v, want \"color\", true", color, ok)
	}
}

func parseOneLine(t *testing.T, line string) ppdOption {
	t.Helper()
	m := ppdOptionLineRe.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("ppdOptionLineRe did not match %q", line)
	}
	return ppdOption{keyword: m[1], choices: strings.Fields(m[2])}
}

func TestPpdOptionLineRe_ParsesRealDuplexLine(t *testing.T) {
	got := parseOneLine(t, realDuplexLine)
	if got.keyword != "Duplex" {
		t.Errorf("keyword = %q, want %q", got.keyword, "Duplex")
	}
	want := []string{"None", "DuplexTumble", "*DuplexNoTumble"}
	if len(got.choices) != len(want) {
		t.Fatalf("choices = %v, want %v", got.choices, want)
	}
	for i := range want {
		if got.choices[i] != want[i] {
			t.Errorf("choices[%d] = %q, want %q", i, got.choices[i], want[i])
		}
	}
}

func TestPickChoice_OneSidedPicksNoneFromRealDuplexChoices(t *testing.T) {
	got := parseOneLine(t, realDuplexLine)
	choice, ok := pickChoice(got.choices, []string{"none", "simplex"}, nil)
	if !ok || choice != "None" {
		t.Errorf("pickChoice (one-sided) = %q, %v, want \"None\", true", choice, ok)
	}
}

func TestPickChoice_TwoSidedPrefersNoTumbleFromRealDuplexChoices(t *testing.T) {
	got := parseOneLine(t, realDuplexLine)
	choice, ok := pickChoice(got.choices, []string{"notumble"}, nil)
	if !ok || choice != "DuplexNoTumble" {
		t.Errorf("pickChoice (two-sided) = %q, %v, want \"DuplexNoTumble\", true", choice, ok)
	}
}

func TestPickChoice_MonoPicksGrayFromRealColorModelChoices(t *testing.T) {
	got := parseOneLine(t, realColorModelLine)
	choice, ok := pickChoice(got.choices, []string{"gray", "grey", "mono", "black"}, nil)
	if !ok || choice != "Gray" {
		t.Errorf("pickChoice (mono) = %q, %v, want \"Gray\", true", choice, ok)
	}
}

func TestPickChoice_ColorAvoidsGrayFromRealColorModelChoices(t *testing.T) {
	got := parseOneLine(t, realColorModelLine)
	choice, ok := pickChoice(got.choices, nil, []string{"gray", "grey", "mono", "black"})
	if !ok || choice != "CMYK" {
		t.Errorf("pickChoice (color) = %q, %v, want \"CMYK\", true", choice, ok)
	}
}

// realGenericPostScriptDuplexBlock is copied verbatim (2026-09-13) from the
// real PPD `/usr/libexec/cups/daemon/cups-driverd cat
// drv:///sample.drv/generic.ppd` actually generates on this machine -
// confirmed unprivileged, no queue creation needed - the *exact* content
// Apple's own bundled "Generic PostScript Printer" (macgeneric.go,
// driver.GenericPostScriptModel) resolves to. Real, not synthetic: proves
// this codebase's own PPD-option parsing handles Apple's generated PPD
// structure correctly, not just vendor-authored ones. The "Generic PCL
// Laser Printer" PPD (drv:///sample.drv/generpcl.ppd) declares the
// identical *OpenUI *Duplex block - same CUPS PPD compiler, same sample.drv
// source.
const realGenericPostScriptDuplexBlock = `*OpenUI *Duplex/2-Sided Printing: PickOne
*OrderDependency: 10 AnySetup *Duplex
*DefaultDuplex: None
*Duplex None/Off (1-Sided): "<</Duplex false>>setpagedevice"
*Duplex DuplexNoTumble/Long-Edge (Portrait): "<</Duplex true/Tumble false>>setpagedevice"
*Duplex DuplexTumble/Short-Edge (Landscape): "<</Duplex true/Tumble true>>setpagedevice"
*CloseUI: *Duplex`

// TestParsePPDOpenUIOptions_RealGenericPostScriptDuplexBlock guards that
// Apple's own bundled Generic PostScript/PCL drivers (offered as a
// last-resort Driver-dropdown fallback - see macgeneric.go) produce a real
// PPD this codebase's existing Duplex-detection logic already handles
// correctly, with no special-casing needed: standard "Duplex" keyword,
// "None"/"DuplexNoTumble"/"DuplexTumble" choices - the exact shape
// findOption/pickChoice were already built for.
//
// Confirmed live (2026-09-13) that neither generic PPD declares any
// *OpenUI *ColorModel-shaped option at all - each is a static
// *ColorDevice: True/False flag instead (PostScript: True, PCL: False),
// with nothing selectable. A row's own Mono checkbox has no real color
// option to act on for either generic driver - decidePrintDefaults already
// warns "declares no ColorModel option" for exactly this shape (the same
// treatment a genuinely monochrome-only vendor PPD already gets), not a
// bug to fix, just an inherent limitation of Apple's own generic driver
// definitions nothing in PDT can improve.
func TestParsePPDOpenUIOptions_RealGenericPostScriptDuplexBlock(t *testing.T) {
	opts := parsePPDOpenUIOptions(realGenericPostScriptDuplexBlock)
	if len(opts) != 1 || opts[0].keyword != "Duplex" {
		t.Fatalf("expected exactly one Duplex option, got %+v", opts)
	}
	toSet, warnings := decidePrintDefaults(opts, false, false, "row \"Test\"")
	found := false
	for _, arg := range toSet {
		if arg == "Duplex=DuplexNoTumble" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Duplex=DuplexNoTumble (two-sided) in toSet, got %v", toSet)
	}
	for _, w := range warnings {
		if strings.Contains(w, "Duplex") {
			t.Errorf("did not expect a Duplex-related warning against this real PPD block, got %q", w)
		}
	}
}

// TestPrintDefaultsForNewQueue_GenericModelReferenceSkipsReading guards a
// real gap: Apple's own bundled Generic PostScript/PCL drivers (a
// `-m drv:///...` reference, not a real PPD file - see
// isGenericModelReference/driver.GenericDriverModelByLabel) have no PPD
// file on disk to pre-read at all. Without this, readPPDFileOptions would
// try to os.Open a literal "drv:///sample.drv/generic.ppd" string as a file
// path and fail, producing a confusing "could not read PPD options"
// warning instead of the clean no-op the empty-ppdPath (-m everywhere) case
// already gets.
func TestPrintDefaultsForNewQueue_GenericModelReferenceSkipsReading(t *testing.T) {
	toSet, warnings := PrintDefaultsForNewQueue("Test Row", "drv:///sample.drv/generic.ppd", true, true)
	if toSet != nil || warnings != nil {
		t.Errorf("expected no args and no warnings for a generic model reference, got toSet=%v warnings=%v", toSet, warnings)
	}
}
