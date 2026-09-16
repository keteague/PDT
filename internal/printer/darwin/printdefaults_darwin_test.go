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
		{keyword: "CNColorSyncICC", choices: valueChoices("DefaultFile")},
		{keyword: "CNColorHalftone", choices: valueChoices("pattern6")},
		{keyword: "CNNumberOfColors", choices: valueChoices("FullColor")},
		{keyword: "CNColorToUseWithBlack", choices: valueChoices("Red")},
		{keyword: "CNXColorAdjustment", choices: valueChoices("6")},
		{keyword: "CNYColorAdjustment", choices: valueChoices("6")},
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
		{keyword: "CNProcessColorMode", choices: valueChoices("False", "*True")},
		{keyword: "CNColorMode", choices: valueChoices("mono", "*color")},
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
	if strings.Join(choiceValues(opts[0].choices), ",") != strings.Join(want, ",") {
		t.Errorf("choices = %v, want %v", opts[0].choices, want)
	}
}

func TestParsePPDOpenUIOptions_ParsesRealCanonColorModeBlock(t *testing.T) {
	opts := parsePPDOpenUIOptions(realCanonColorModePPDBlock)
	if len(opts) != 1 || opts[0].keyword != "CNColorMode" {
		t.Fatalf("parsePPDOpenUIOptions = %+v, want one CNColorMode option", opts)
	}
	want := []string{"mono", "color"}
	if strings.Join(choiceValues(opts[0].choices), ",") != strings.Join(want, ",") {
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

// valueChoices builds []ppdChoice from bare values with no label - the
// listPPDOptions (`lpoptions -l`) shape, where a per-choice label is never
// available.
func valueChoices(values ...string) []ppdChoice {
	choices := make([]ppdChoice, len(values))
	for i, v := range values {
		choices[i] = ppdChoice{value: v}
	}
	return choices
}

// choiceValues extracts just the .value from each choice, for tests that
// only care about values (most - .label only matters for the Sharp
// abbreviated-code tests below).
func choiceValues(choices []ppdChoice) []string {
	values := make([]string, len(choices))
	for i, c := range choices {
		values[i] = c.value
	}
	return values
}

func parseOneLine(t *testing.T, line string) ppdOption {
	t.Helper()
	m := ppdOptionLineRe.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("ppdOptionLineRe did not match %q", line)
	}
	return ppdOption{keyword: m[1], choices: valueChoices(strings.Fields(m[2])...)}
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
		if got.choices[i].value != want[i] {
			t.Errorf("choices[%d] = %q, want %q", i, got.choices[i].value, want[i])
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
// realSharpARCModePPDBlock is copied verbatim (2026-09-13) from the real,
// installed Sharp BP-20C20 PPD
// (`gunzip -c "SHARP BP-20C20.PPD.gz"` from the real MX-C55c_2512a_MacPS.dmg
// payload). This is the exact real bug that motivated splitting ppdChoice
// into value+label: Sharp's own choice *values* are abbreviated, non-
// self-describing codes ("CMAuto", "CMColor", "CMBW") - only the label after
// the "/" ("Automatic", "Color", "Black and White") is human-readable. A
// live 2-row deploy against this exact real driver confirmed the resulting
// bug: "row %q's PPD declares no ColorModel option; leaving color mode
// as-is" for a row whose Mono checkbox was checked, leaving the PPD's own
// hardcoded default ("Automatic") in CUPS instead of the requested
// Black & White.
const realSharpARCModePPDBlock = `*OpenUI *ARCMode/Color Mode: PickOne
*OrderDependency: 180 AnySetup *ARCMode
*DefaultARCMode: CMAuto
*ARCMode CMAuto/Automatic: "
	userdict /ARCMode known not {userdict /ARCMode 0 put} if
	0 setcolormode"
*End
*ARCMode CMColor/Color: "
	userdict /ARCMode known not {userdict /ARCMode 1 put} if
	1 setcolormode"
*End
*ARCMode CMBW/Black and White: "
	userdict /ARCMode known not {userdict /ARCMode 2 put} if
	2 setcolormode"
*End
*CloseUI: *ARCMode`

// TestParsePPDOpenUIOptions_CapturesSharpARCModeLabels guards that
// parsePPDOpenUIOptions keeps each choice's own label ("Black and White"),
// not just its abbreviated value ("CMBW") - the label is the only place
// Sharp's real PPD spells out what each abbreviated code actually means.
func TestParsePPDOpenUIOptions_CapturesSharpARCModeLabels(t *testing.T) {
	opts := parsePPDOpenUIOptions(realSharpARCModePPDBlock)
	if len(opts) != 1 || opts[0].keyword != "ARCMode" {
		t.Fatalf("expected exactly one ARCMode option, got %+v", opts)
	}
	want := map[string]string{"CMAuto": "Automatic", "CMColor": "Color", "CMBW": "Black and White"}
	if len(opts[0].choices) != len(want) {
		t.Fatalf("choices = %+v, want %d entries", opts[0].choices, len(want))
	}
	for _, c := range opts[0].choices {
		if want[c.value] != c.label {
			t.Errorf("choice %q has label %q, want %q", c.value, c.label, want[c.value])
		}
	}
}

// TestFindOption_MatchesSharpARCMode guards the other half of the real bug:
// findOption's own exact-keyword allowlist didn't recognize "ARCMode" as a
// ColorModel-equivalent keyword at all, so decidePrintDefaults never even
// reached pickChoice for a real Sharp PPD - confirmed live via the deploy
// log's own "declares no ColorModel option" warning.
func TestFindOption_MatchesSharpARCMode(t *testing.T) {
	opts := parsePPDOpenUIOptions(realSharpARCModePPDBlock)
	got, ok := findOption(opts, "colormodel", "cncolormode", "arcmode")
	if !ok || got.keyword != "ARCMode" {
		t.Errorf("findOption(colormodel, cncolormode, arcmode) = %+v, %v, want ARCMode, true", got, ok)
	}
}

// TestPickChoice_MonoPicksCMBWFromRealSharpARCModeChoicesByLabel is the
// end-to-end regression: pickChoice must resolve mono=true against Sharp's
// real ARCMode choices to "CMBW" (the value lpadmin actually needs), found
// only by matching "black" against the choice's own LABEL ("Black and
// White") - the value "CMBW" alone contains none of "gray"/"grey"/"mono"/
// "black", so a value-only match (the pre-fix behavior) would report no
// match at all here.
func TestPickChoice_MonoPicksCMBWFromRealSharpARCModeChoicesByLabel(t *testing.T) {
	opts := parsePPDOpenUIOptions(realSharpARCModePPDBlock)
	choice, ok := pickChoice(opts[0].choices, []string{"gray", "grey", "mono", "black"}, nil)
	if !ok || choice != "CMBW" {
		t.Errorf("pickChoice (mono) = %q, %v, want \"CMBW\", true", choice, ok)
	}
}

// TestDecidePrintDefaults_SharpARCModeAppliesBlackAndWhiteWithNoWarning is
// the full real-deploy-shaped regression: decidePrintDefaults against the
// real Sharp PPD block, mono requested, must produce "ARCMode=CMBW" with no
// warning - reproducing exactly what a real zCom Two-shaped deploy row
// needs, confirmed live to be broken before both fixes above (findOption's
// "arcmode" keyword, pickChoice's label matching) landed together.
func TestDecidePrintDefaults_SharpARCModeAppliesBlackAndWhiteWithNoWarning(t *testing.T) {
	opts := parsePPDOpenUIOptions(realSharpARCModePPDBlock)
	toSet, warnings := decidePrintDefaults(opts, true, true, `row "zCom Two"`)
	found := false
	for _, arg := range toSet {
		if arg == "ARCMode=CMBW" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ARCMode=CMBW in toSet, got %v", toSet)
	}
	for _, w := range warnings {
		if strings.Contains(w, "ColorModel") || strings.Contains(w, "ARCMode") {
			t.Errorf("did not expect a color-related warning against this real PPD block, got %q", w)
		}
	}
}

// realXeroxOutputColorPPDBlock is copied verbatim (2026-09-13) from the
// real, installed Xerox AltaLink C8230 Color MFP PPD (`gunzip -c
// "Xerox AltaLink C8230 Color MFP.gz"` from the real
// XeroxDrivers_5.19.3_2562.dmg payload). Unlike Sharp's own ARCMode, Xerox's
// own choice values are already self-describing ("PrintAsGrayscale"/
// "PrintAsColor") - this guards findOption recognizing the "XROutputColor"
// keyword at all (it isn't "ColorModel"/"CNColorMode"/"ARCMode"), not a
// label-matching gap.
const realXeroxOutputColorPPDBlock = `*OpenUI *XROutputColor/Xerox Black and White: PickOne
*OrderDependency: 10 AnySetup *XROutputColor
*DefaultXROutputColor: PrintAsColor
*XROutputColor PrintAsColor/Off (Use Document Color): ""
*XROutputColor PrintAsGrayscale/On: ""
*CloseUI: *XROutputColor`

// TestFindOption_MatchesXeroxXROutputColor guards findOption's own
// exact-keyword allowlist recognizing Xerox's real ColorModel-equivalent
// keyword.
func TestFindOption_MatchesXeroxXROutputColor(t *testing.T) {
	opts := parsePPDOpenUIOptions(realXeroxOutputColorPPDBlock)
	got, ok := findOption(opts, "colormodel", "cncolormode", "arcmode", "xroutputcolor")
	if !ok || got.keyword != "XROutputColor" {
		t.Errorf("findOption(..., xroutputcolor) = %+v, %v, want XROutputColor, true", got, ok)
	}
}

// TestDecidePrintDefaults_XeroxOutputColorAppliesGrayscaleAndColorCorrectly
// is the full real-deploy-shaped regression for both directions: mono=true
// must pick "PrintAsGrayscale" (value alone contains "gray", no label
// needed), mono=false must pick "PrintAsColor" (the first choice that
// avoids every mono/gray/black keyword) - both with no warning.
func TestDecidePrintDefaults_XeroxOutputColorAppliesGrayscaleAndColorCorrectly(t *testing.T) {
	opts := parsePPDOpenUIOptions(realXeroxOutputColorPPDBlock)

	toSet, warnings := decidePrintDefaults(opts, true, true, `row "Test"`)
	if !containsSetting(toSet, "XROutputColor=PrintAsGrayscale") {
		t.Errorf("expected XROutputColor=PrintAsGrayscale in toSet, got %v", toSet)
	}
	for _, w := range warnings {
		if strings.Contains(w, "ColorModel") || strings.Contains(w, "XROutputColor") {
			t.Errorf("did not expect a color-related warning (mono), got %q", w)
		}
	}

	toSet, warnings = decidePrintDefaults(opts, true, false, `row "Test"`)
	if !containsSetting(toSet, "XROutputColor=PrintAsColor") {
		t.Errorf("expected XROutputColor=PrintAsColor in toSet, got %v", toSet)
	}
	for _, w := range warnings {
		if strings.Contains(w, "ColorModel") || strings.Contains(w, "XROutputColor") {
			t.Errorf("did not expect a color-related warning (color), got %q", w)
		}
	}
}

// realLexmarkColorModePPDBlock is copied verbatim (2026-09-16) from the
// real, installed Lexmark Universal Print Driver PPD (`gunzip -c
// "Lexmark Universal Color.gz"` from the real
// Lexmark_UC1_PrinterSoftware_04022026.dmg payload). Unlike Xerox's own
// XROutputColor, Lexmark's own choice *values* ("TrueM"/"FalseM") are not
// self-describing at all - this guards both findOption recognizing the
// "ColorMode" keyword (it isn't "ColorModel") AND pickChoice's existing
// label fallback correctly reading "Color"/"Monochrome" off each choice's
// own label, the same shape Sharp's own ARCMode needed.
const realLexmarkColorModePPDBlock = `*OpenUI *ColorMode/Color Mode: PickOne
*OrderDependency: 13 AnySetup *ColorMode
*DefaultColorMode: TrueM
*ColorMode TrueM/Color: ""
*ColorMode FalseM/Monochrome: ""
*CloseUI: *ColorMode`

// TestFindOption_MatchesLexmarkColorMode guards findOption's own
// exact-keyword allowlist recognizing Lexmark's real ColorModel-equivalent
// keyword - a real, confirmed-live gap (2026-09-16): a real deploy warned
// "declares no ColorModel option" against this exact PPD before "colormode"
// was added here.
func TestFindOption_MatchesLexmarkColorMode(t *testing.T) {
	opts := parsePPDOpenUIOptions(realLexmarkColorModePPDBlock)
	got, ok := findOption(opts, "colormodel", "cncolormode", "arcmode", "xroutputcolor", "colortype", "colormode")
	if !ok || got.keyword != "ColorMode" {
		t.Errorf("findOption(..., colormode) = %+v, %v, want ColorMode, true", got, ok)
	}
}

// TestDecidePrintDefaults_LexmarkColorModeAppliesBothDirectionsViaLabel is
// the full real-deploy-shaped regression: mono=true must pick "FalseM" (via
// its own "Monochrome" label, since the bare value has no "mono"/"gray"
// substring at all), mono=false must pick "TrueM" (via its own "Color"
// label) - both with no warning.
func TestDecidePrintDefaults_LexmarkColorModeAppliesBothDirectionsViaLabel(t *testing.T) {
	opts := parsePPDOpenUIOptions(realLexmarkColorModePPDBlock)

	toSet, warnings := decidePrintDefaults(opts, true, true, `row "Test"`)
	if !containsSetting(toSet, "ColorMode=FalseM") {
		t.Errorf("expected ColorMode=FalseM in toSet, got %v", toSet)
	}
	for _, w := range warnings {
		if strings.Contains(w, "ColorModel") || strings.Contains(w, "ColorMode") {
			t.Errorf("did not expect a color-related warning (mono), got %q", w)
		}
	}

	toSet, warnings = decidePrintDefaults(opts, true, false, `row "Test"`)
	if !containsSetting(toSet, "ColorMode=TrueM") {
		t.Errorf("expected ColorMode=TrueM in toSet, got %v", toSet)
	}
	for _, w := range warnings {
		if strings.Contains(w, "ColorModel") || strings.Contains(w, "ColorMode") {
			t.Errorf("did not expect a color-related warning (color), got %q", w)
		}
	}
}

func containsSetting(toSet []string, want string) bool {
	for _, s := range toSet {
		if s == want {
			return true
		}
	}
	return false
}

// realToshibaColorTypePPDBlock is copied verbatim (2026-09-13) from the
// real, installed Toshiba ColorMFP-X7 PPD (`gunzip -c
// "TOSHIBA_ColorMFP_X7.gz"` from the real TOSHIBA_ColorMFP.dmg.gz payload -
// itself a plain gzip-compressed UDIF image, a shape none of Canon/Kyocera/
// Ricoh/Sharp/Xerox's own real downloads have). Like Xerox's own choices,
// "Mono" is already self-describing, so this guards findOption recognizing
// the "ColorType" keyword itself, not a label-matching gap.
const realToshibaColorTypePPDBlock = `*OpenUI *ColorType/Color Type: PickOne
*OrderDependency: 50 AnySetup *ColorType
*DefaultColorType: Color
*ColorType Auto/Auto: "<</TSBPrivate (DSSC PRINT RENDERMODE=AUTO) >> setpagedevice "
*ColorType Color/Color: "<</TSBPrivate (DSSC PRINT RENDERMODE=COLOR) >> setpagedevice "
*ColorType Mono/Mono: "<</ProcessColorModel /DeviceGray >> setpagedevice <</TSBPrivate (DSSC PRINT RENDERMODE=GRAYSCALE) >> setpagedevice "
*ColorType Black&Red/Black and Red: "<</TSBPrivate (DSSC PRINT RENDERMODE=2KR) >> setpagedevice "
*CloseUI: *ColorType`

func TestFindOption_MatchesToshibaColorType(t *testing.T) {
	opts := parsePPDOpenUIOptions(realToshibaColorTypePPDBlock)
	got, ok := findOption(opts, "colormodel", "cncolormode", "arcmode", "xroutputcolor", "colortype")
	if !ok || got.keyword != "ColorType" {
		t.Errorf("findOption(..., colortype) = %+v, %v, want ColorType, true", got, ok)
	}
}

// TestDecidePrintDefaults_ToshibaColorTypeAppliesMonoCorrectly is the
// end-to-end regression: mono=true must pick "Mono" (self-describing value,
// no label needed), with no warning. Deliberately doesn't assert on the
// mono=false pick - "Auto" is the first choice that avoids every mono/gray/
// black keyword (listed before the PPD's own literal "Color" choice), a
// reasonable real answer for "don't force monochrome," same as Xerox's own
// "PrintAsColor"/"Off (Use Document Color)" already being the accepted
// non-mono pick for that manufacturer.
func TestDecidePrintDefaults_ToshibaColorTypeAppliesMonoCorrectly(t *testing.T) {
	opts := parsePPDOpenUIOptions(realToshibaColorTypePPDBlock)
	toSet, warnings := decidePrintDefaults(opts, true, true, `row "Test"`)
	if !containsSetting(toSet, "ColorType=Mono") {
		t.Errorf("expected ColorType=Mono in toSet, got %v", toSet)
	}
	for _, w := range warnings {
		if strings.Contains(w, "ColorModel") || strings.Contains(w, "ColorType") {
			t.Errorf("did not expect a color-related warning (mono), got %q", w)
		}
	}
}

// realKonicaMinoltaKMDuplexPPDBlock is copied verbatim (2026-09-13) from
// the real, installed Konica Minolta C751i PPD (`gunzip -c
// "KONICAMINOLTAC751i.gz"` from the real WW_Letter payload). Unlike every
// other manufacturer inspected so far, Konica Minolta's own real
// ColorModel-equivalent option *is* spelled the plain CUPS-standard
// "ColorModel" way (needing no findOption addition) - it's the Duplex side
// that needed one here: "KMDuplex", with choices Single/Double/Booklet -
// no NoTumble/Tumble binding-edge split at all, and neither "single" nor
// "double" matched any existing want-list vocabulary before this fix.
const realKonicaMinoltaKMDuplexPPDBlock = `*OpenUI  *KMDuplex/Print Type: PickOne
*OrderDependency: 5 AnySetup *KMDuplex
*DefaultKMDuplex: Double
*KMDuplex Single/1-Sided:  "<< /Duplex false >> setpagedevice"
*KMDuplex Double/2-Sided:  "<< /Duplex true >> setpagedevice"
*KMDuplex Booklet/Booklet:  "<< /Duplex true >> setpagedevice"
*CloseUI: *KMDuplex`

func TestFindOption_MatchesKonicaMinoltaKMDuplex(t *testing.T) {
	opts := parsePPDOpenUIOptions(realKonicaMinoltaKMDuplexPPDBlock)
	got, ok := findOption(opts, "duplex", "cnduplex", "kmduplex")
	if !ok || got.keyword != "KMDuplex" {
		t.Errorf("findOption(..., kmduplex) = %+v, %v, want KMDuplex, true", got, ok)
	}
}

// TestDecidePrintDefaults_KonicaMinoltaKMDuplexAppliesBothDirections is the
// full real-deploy-shaped regression: before this fix, a one-sided request
// matched nothing against "Single" (the want-list only had "none"/
// "simplex") and a two-sided request matched nothing against "Double"
// either (only "tumble"/"duplex"), silently leaving the PPD's own hardcoded
// default (2-sided) in place regardless of what was actually requested -
// a real, confirmed-live risk: a one-sided request would have silently
// stayed 2-sided.
func TestDecidePrintDefaults_KonicaMinoltaKMDuplexAppliesBothDirections(t *testing.T) {
	opts := parsePPDOpenUIOptions(realKonicaMinoltaKMDuplexPPDBlock)

	toSet, warnings := decidePrintDefaults(opts, true, false, `row "Test"`)
	if !containsSetting(toSet, "KMDuplex=Single") {
		t.Errorf("expected KMDuplex=Single (one-sided) in toSet, got %v", toSet)
	}
	for _, w := range warnings {
		if strings.Contains(w, "Duplex") {
			t.Errorf("did not expect a duplex-related warning (one-sided), got %q", w)
		}
	}

	toSet, warnings = decidePrintDefaults(opts, false, false, `row "Test"`)
	if !containsSetting(toSet, "KMDuplex=Double") {
		t.Errorf("expected KMDuplex=Double (two-sided) in toSet, got %v", toSet)
	}
	for _, w := range warnings {
		if strings.Contains(w, "Duplex") {
			t.Errorf("did not expect a duplex-related warning (two-sided), got %q", w)
		}
	}
}

func TestPrintDefaultsForNewQueue_GenericModelReferenceSkipsReading(t *testing.T) {
	toSet, warnings := PrintDefaultsForNewQueue("Test Row", "drv:///sample.drv/generic.ppd", true, true)
	if toSet != nil || warnings != nil {
		t.Errorf("expected no args and no warnings for a generic model reference, got toSet=%v warnings=%v", toSet, warnings)
	}
}
