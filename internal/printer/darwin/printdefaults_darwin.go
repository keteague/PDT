package darwin

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// ppdOptionLineRe matches one `lpoptions -p <name> -l` line: "Keyword/Label:
// choice1 choice2 *default3 choice4" - confirmed against a real, already-
// deployed Kyocera queue on this machine, e.g.
// "Duplex/Duplexing: None DuplexTumble *DuplexNoTumble" and
// "ColorModel/Color mode: *CMYK Gray".
var ppdOptionLineRe = regexp.MustCompile(`^(\S+?)/[^:]*:\s*(.+)$`)

// ppdChoice is one option's own selectable value, alongside its human-
// readable label when one is available. value is always what actually gets
// sent to `lpadmin -o Key=Value` - never the label. label is populated only
// when parsePPDOpenUIOptions reads a raw PPD file directly (it can see the
// "/Label" part of a `*<Keyword> <Value>/<Label>: ...` line); listPPDOptions
// (`lpoptions -l`, for a queue that already exists) has no way to recover a
// per-choice label at all - CUPS's own `-l` output only ever lists bare
// values - so label stays "" there. Confirmed live (2026-09-13) that this
// split matters: a real Sharp PPD's own ColorModel-equivalent option
// (*ARCMode) declares abbreviated, non-self-describing values ("CMAuto",
// "CMColor", "CMBW") whose own label carries the only human-readable meaning
// ("Automatic", "Color", "Black and White") - every other manufacturer's own
// real PPDs inspected so far (Canon, Kyocera, Ricoh) happened to use
// self-describing values instead (e.g. "DuplexNoTumble", "Gray"), which is
// why this gap went unnoticed until now.
type ppdChoice struct {
	value string
	label string
}

// ppdOption is one PPD-declared option's keyword and its available choices,
// parsed from one line of `lpoptions -l` output.
type ppdOption struct {
	keyword string
	choices []ppdChoice
}

func listPPDOptions(ctx context.Context, queueName string) ([]ppdOption, error) {
	out, err := exec.CommandContext(ctx, "lpoptions", "-p", queueName, "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("listing options for %q: %w", queueName, err)
	}
	var opts []ppdOption
	for _, line := range strings.Split(string(out), "\n") {
		m := ppdOptionLineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		fields := strings.Fields(m[2])
		choices := make([]ppdChoice, len(fields))
		for i, f := range fields {
			choices[i] = ppdChoice{value: f}
		}
		opts = append(opts, ppdOption{keyword: m[1], choices: choices})
	}
	return opts, nil
}

// ppdOpenUIRe matches one `*OpenUI *<Keyword>[/Label]: <PickOne|Boolean>`
// line straight out of a raw PPD file - the same declaration `lpoptions -l`
// itself reads to build the line ppdOptionLineRe parses, one level closer to
// the source. Confirmed against the real installed Canon PPD's own
// `*OpenUI *CNDuplex/Print Style: PickOne`. Matches one-or-more spaces
// between "OpenUI" and the keyword, not exactly one - confirmed live
// (2026-09-13) as a real, previously-undiscovered bug: every single
// `*OpenUI` line in every real Konica Minolta PPD inspected uses a literal
// double space ("*OpenUI  *KMDuplex/Print Type: PickOne", confirmed via a
// raw hex dump, not just eyeballing it) - an exact-one-space regex silently
// matched zero options in any real Konica Minolta PPD at all, meaning
// Duplex/ColorModel would never have been set on a real deploy, no warning
// either (the whole opts slice would just come back empty). The choice-line
// and `*CloseUI:` matching elsewhere in this file both already use a plain,
// single literal space and were confirmed live to need no similar
// widening - only this one declaration line has the real double-space
// quirk.
var ppdOpenUIRe = regexp.MustCompile(`^\*OpenUI\s+\*(\S+?)(?:/[^:]*)?:`)

// readPPDFileBytes reads ppdPath's raw contents, transparently gzip-
// decompressing a ".gz" path - the same convention driver.ReadPPDNickName
// already relies on for every PPD under
// /Library/Printers/PPDs/Contents/Resources.
func readPPDFileBytes(ppdPath string) ([]byte, error) {
	f, err := os.Open(ppdPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(strings.ToLower(ppdPath), ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}
	return io.ReadAll(io.LimitReader(r, 4<<20))
}

// readPPDFileOptions reads ppdPath's own *OpenUI/*CloseUI-declared options
// directly off disk instead of `lpoptions -p <queue> -l` (listPPDOptions'
// own source) - the point being it works before any CUPS queue exists at
// all, since it never touches a live queue. This is what lets Deploy compute
// a brand-new queue's duplex/color -o args *before* creating it, so they can
// ride along on the very same `lpadmin` call that creates the queue instead
// of needing a second, separate elevated call straight after - confirmed
// live as a real, meaningful cost: a queue-create prompt and a following
// defaults-set prompt landing within ~15s of each other still each showed
// their own native password dialog, not covered by one cached grant.
func readPPDFileOptions(ppdPath string) ([]ppdOption, error) {
	data, err := readPPDFileBytes(ppdPath)
	if err != nil {
		return nil, err
	}
	return parsePPDOpenUIOptions(string(data)), nil
}

// parsePPDOpenUIOptions walks a raw PPD's own `*OpenUI *<Keyword>/...:` ...
// `*<Keyword> <Choice>/...: ...` ... `*CloseUI: *<Keyword>` blocks -
// confirmed against the real installed Canon PPD's own
//
//	*OpenUI *CNDuplex/Print Style: PickOne
//	*DefaultCNDuplex: DuplexFront
//	*CNDuplex None/1-sided Printing: "<< >>setpagedevice"
//	*CNDuplex DuplexFront/2-sided Printing: "<< >>setpagedevice"
//	*CNDuplex Booklet/Booklet Printing: "<< >>setpagedevice"
//	*CloseUI: *CNDuplex
//
// - note the `*Default<Keyword>: ...` line in the middle is deliberately
// skipped (it doesn't start with "*<Keyword> ", it starts with
// "*Default<Keyword>"), and pickChoice never needs to know which choice was
// already default anyway, only the full available set.
func parsePPDOpenUIOptions(text string) []ppdOption {
	var result []ppdOption
	var building ppdOption
	inBlock := false
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if m := ppdOpenUIRe.FindStringSubmatch(line); m != nil {
			if inBlock {
				result = append(result, building)
			}
			building = ppdOption{keyword: m[1]}
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		if strings.HasPrefix(line, "*CloseUI:") && strings.Contains(line, "*"+building.keyword) {
			result = append(result, building)
			inBlock = false
			continue
		}
		if rest, ok := strings.CutPrefix(line, "*"+building.keyword+" "); ok {
			value, label := rest, ""
			if slash := strings.Index(value, "/"); slash >= 0 {
				label = value[slash+1:]
				value = value[:slash]
				if colon := strings.Index(label, ":"); colon >= 0 {
					label = label[:colon]
				}
			} else if colon := strings.Index(value, ":"); colon >= 0 {
				value = value[:colon]
			}
			if value = strings.TrimSpace(value); value != "" {
				building.choices = append(building.choices, ppdChoice{value: value, label: strings.TrimSpace(label)})
			}
		}
	}
	if inBlock {
		result = append(result, building)
	}
	return result
}

// pickChoice returns the first choice (its "*"-default marker stripped, own
// value only - never the label) whose searchable text - value plus label
// when one is available (see ppdChoice's own doc comment) - contains none of
// avoid, and, when want is non-empty, also contains at least one of want. An
// empty want matches any choice not in avoid (used to pick "whatever's left
// once the mono-ish choices are excluded" for a color request, where there's
// no single positive keyword to require). The same substring-based approach
// devmode_windows.go's own driver-specific quirks already accept as
// unavoidable given how inconsistently vendors name PPD options and choices;
// unlike Windows' fixed DEVMODE fields, there is no single correct keyword/
// value pair to hardcode here. Matching against the label alongside the
// value (not the value alone) is what lets this recognize a real Sharp
// *ARCMode choice like "CMBW" (value) / "Black and White" (label) as the
// mono choice - the abbreviated value alone contains none of "gray"/"mono"/
// "black" (confirmed live, 2026-09-13, a real bug: Sharp's own color default
// silently never got applied, left at the PPD's own hardcoded "Automatic"),
// while still matching every other manufacturer's self-describing values
// exactly as before (an empty label never changes what a value-only match
// already found).
func pickChoice(choices []ppdChoice, want, avoid []string) (string, bool) {
	for _, raw := range choices {
		c := strings.TrimPrefix(raw.value, "*")
		searchable := strings.ToLower(c)
		if raw.label != "" {
			searchable += " " + strings.ToLower(raw.label)
		}
		if containsAny(searchable, avoid) {
			continue
		}
		if len(want) == 0 || containsAny(searchable, want) {
			return c, true
		}
	}
	return "", false
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// decidePrintDefaults is SetPrintDefaults'/PrintDefaultsForNewQueue's shared
// duplex/color choice logic, fed from opts (either a live queue's `lpoptions
// -l`, or a not-yet-created queue's own PPD file read straight off disk) -
// subject names whatever opts came from in a warning ("queue %q" or "row
// %q") without this function needing to know which. A PPD with no Duplex/
// ColorModel-ish option at all, or none of whose choices look like what was
// asked for, produces a warning for that one setting and leaves the rest
// alone - never a hard failure.
//
// The one-sided/two-sided want-lists ("none"/"simplex"/"single",
// "tumble"/"duplex"/"double") grew "single"/"double" for Konica Minolta's
// own real KMDuplex choices (confirmed live, 2026-09-13:
// "Single"/"Double"/"Booklet" - no NoTumble/Tumble binding-edge split at
// all, unlike every Duplex-spelled manufacturer inspected so far) - without
// this, a one-sided request would have silently matched nothing at all
// against "Single" and left the PPD's own hardcoded default (2-sided) in
// place instead.
func decidePrintDefaults(opts []ppdOption, oneSided, mono bool, subject string) (toSet, warnings []string) {
	if duplex, ok := findOption(opts, "duplex", "cnduplex", "kmduplex"); ok {
		if oneSided {
			if choice, ok := pickChoice(duplex.choices, []string{"none", "simplex", "single"}, nil); ok {
				toSet = append(toSet, duplex.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("%s's PPD has no simplex/None choice for %s; leaving duplex as-is", subject, duplex.keyword))
			}
		} else {
			if choice, ok := pickChoice(duplex.choices, []string{"notumble"}, nil); ok {
				toSet = append(toSet, duplex.keyword+"="+choice)
			} else if choice, ok := pickChoice(duplex.choices, []string{"tumble", "duplex", "double"}, []string{"none"}); ok {
				toSet = append(toSet, duplex.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("%s's PPD has no two-sided choice for %s; leaving duplex as-is", subject, duplex.keyword))
			}
		}
	} else {
		warnings = append(warnings, fmt.Sprintf("%s's PPD declares no Duplex option; leaving duplex as-is", subject))
	}

	if colorModel, ok := findOption(opts, "colormodel", "cncolormode", "arcmode", "xroutputcolor", "colortype", "colormode"); ok {
		if mono {
			if choice, ok := pickChoice(colorModel.choices, []string{"gray", "grey", "mono", "black"}, nil); ok {
				toSet = append(toSet, colorModel.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("%s's PPD has no grayscale/mono choice for %s; leaving color mode as-is", subject, colorModel.keyword))
			}
		} else {
			if choice, ok := pickChoice(colorModel.choices, nil, []string{"gray", "grey", "mono", "black"}); ok {
				toSet = append(toSet, colorModel.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("%s's PPD has no color choice for %s; leaving color mode as-is", subject, colorModel.keyword))
			}
		}
	} else {
		warnings = append(warnings, fmt.Sprintf("%s's PPD declares no ColorModel option; leaving color mode as-is", subject))
	}
	return toSet, warnings
}

// SetPrintDefaults best-effort applies duplex/color defaults to an existing
// queueName via its own separate `lpadmin` call, returning warning strings
// rather than an error - the direct analog of deploy_windows.go's own best-
// effort/[WARN] treatment of duplex/color via devmode_windows.go, adapted
// for PPD option keywords/choices instead of fixed DEVMODE fields (see
// pickChoice's own doc comment for why). Only for a queue Deploy is
// *reusing* (already exists, no queue-create call happening this run to
// piggyback on) - a brand-new queue instead goes through
// PrintDefaultsForNewQueue, folded into the same lpadmin call that creates
// it, so it costs no separate elevated prompt at all.
func SetPrintDefaults(ctx context.Context, queueName string, oneSided, mono bool) []string {
	opts, err := listPPDOptions(ctx, queueName)
	if err != nil {
		return []string{fmt.Sprintf("could not read PPD options for %q: %v", queueName, err)}
	}

	toSet, warnings := decidePrintDefaults(opts, oneSided, mono, fmt.Sprintf("queue %q", queueName))
	if len(toSet) == 0 {
		return warnings
	}

	argv := append([]string{"lpadmin", "-p", queueName}, optionArgs(toSet)...)
	if _, err := runPrivileged(ctx, argv); err != nil {
		warnings = append(warnings, fmt.Sprintf("could not set print defaults for %q: %v", queueName, err))
	}
	return warnings
}

// PrintDefaultsForNewQueue computes rowName's duplex/color "Key=Value"
// settings (unprefixed - EnsureQueue's own optionArgs call adds "-o") from
// ppdPath's own *OpenUI-declared options, read straight off disk
// (readPPDFileOptions) rather than a live queue's `lpoptions -l` - callable
// *before* the queue exists at all. Deploy folds the result directly into
// the same EnsureQueue lpadmin call that creates the queue, so a brand-new
// queue needs only one elevated osascript prompt total for queue-creation-
// plus-defaults, not a second one straight after (see readPPDFileOptions'
// own doc comment for why that second prompt was a real, confirmed-live
// problem). Returns no args and no warnings for either shape with no real
// local PPD file to pre-read at all - ppdPath == "" (the `-m everywhere`/
// IPP-Everywhere fallback) or a `-m drv:///...` reference (Apple's own
// bundled Generic PostScript/PCL - isGenericModelReference,
// driver.GenericDriverModelByLabel) - Deploy's own caller falls back to
// SetPrintDefaults against the live queue instead once it exists, for
// both.
func PrintDefaultsForNewQueue(rowName, ppdPath string, oneSided, mono bool) (toSet, warnings []string) {
	if ppdPath == "" || isGenericModelReference(ppdPath) {
		return nil, nil
	}
	opts, err := readPPDFileOptions(ppdPath)
	if err != nil {
		return nil, []string{fmt.Sprintf("could not read PPD options from %q: %v", ppdPath, err)}
	}
	return decidePrintDefaults(opts, oneSided, mono, fmt.Sprintf("row %q", rowName))
}

// findOption matches a PPD option keyword against an explicit allowlist of
// exact (case-insensitive) known keyword spellings - NOT a suffix or
// substring match. This replaces an earlier suffix-based version, confirmed
// live to be a real bug: a real Canon PPD declares BOTH *CNColorMode (the
// real color/mono switch, choices mono/color) AND *CNProcessColorMode (an
// unrelated boolean, "Print Mixed Color/B&W Documents at High Speed",
// choices False/True) - both end in "ColorMode", so the suffix match picked
// whichever happened to come first in that PPD's own option order
// (CNProcessColorMode, for this exact model), silently applying mono/color
// settings to the wrong option while leaving the real CNColorMode untouched
// - confirmed live: deployed with Mono checked, CUPS still showed color.
// The known real-world spellings so far: the CUPS-standard "Duplex"/
// "ColorModel", Canon's own "CNDuplex"/"CNColorMode", and Sharp's own
// "ARCMode" (confirmed live, 2026-09-13, against a real Sharp PPD's own
// `*OpenUI *ARCMode/Color Mode: PickOne` block - Sharp's own Duplex keyword
// needed no addition, it already spells it the CUPS-standard way), and
// Xerox's own "XROutputColor" (confirmed live, 2026-09-13, against a real
// Xerox PPD's own `*OpenUI *XROutputColor/Xerox Black and White: PickOne`
// block - unlike Sharp's ARCMode, Xerox's own choice values are already
// self-describing, "PrintAsGrayscale"/"PrintAsColor", so no label-matching
// was needed to recognize this one; Xerox's own Duplex keyword also needed
// no addition), and Toshiba's own "ColorType" (confirmed live, 2026-09-13,
// against a real Toshiba PPD's own `*OpenUI *ColorType/Color Type: PickOne`
// block, choices Auto/Color/Mono/Black&Red - "Mono" is already
// self-describing like Xerox's own choices, so again no label-matching gap;
// Toshiba's own Duplex keyword also needed no addition), and Konica
// Minolta's own "KMDuplex" (confirmed live, 2026-09-13, against a real
// Konica Minolta PPD's own `*OpenUI *KMDuplex/Print Type: PickOne` block,
// choices Single/Double/Booklet - unlike every other manufacturer inspected
// so far, this one's own real ColorModel-equivalent option *is* spelled the
// plain CUPS-standard "ColorModel" way, needing no keyword addition at all;
// it's the Duplex side that needed one here, the reverse of every other
// manufacturer's own real gap), and Lexmark's own "ColorMode" (confirmed
// live, 2026-09-16, against the real installed Lexmark Universal Print
// Driver PPD's own `*OpenUI *ColorMode/...: PickOne` block - a real,
// confirmed-live case where the keyword miss alone wasn't the whole story:
// its own choice *values* are non-self-describing ("TrueM"/"FalseM"), the
// same shape Sharp's own ARCMode needed label-matching for, but its own
// labels ("Color"/"Monochrome") already are self-describing, so
// pickChoice's existing label fallback handles it with no further change;
// Lexmark's own Duplex keyword also needed no addition) - add a new exact
// spelling here if a future vendor's PPD needs one, never widen this back
// to a suffix/substring match.
func findOption(opts []ppdOption, exactKeywordsLower ...string) (ppdOption, bool) {
	for _, o := range opts {
		lower := strings.ToLower(o.keyword)
		for _, want := range exactKeywordsLower {
			if lower == want {
				return o, true
			}
		}
	}
	return ppdOption{}, false
}

func optionArgs(settings []string) []string {
	args := make([]string, 0, len(settings)*2)
	for _, s := range settings {
		args = append(args, "-o", s)
	}
	return args
}
