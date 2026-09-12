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

// ppdOption is one PPD-declared option's keyword and its available choices,
// parsed from one line of `lpoptions -l` output.
type ppdOption struct {
	keyword string
	choices []string
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
		opts = append(opts, ppdOption{keyword: m[1], choices: strings.Fields(m[2])})
	}
	return opts, nil
}

// ppdOpenUIRe matches one `*OpenUI *<Keyword>[/Label]: <PickOne|Boolean>`
// line straight out of a raw PPD file - the same declaration `lpoptions -l`
// itself reads to build the line ppdOptionLineRe parses, one level closer to
// the source. Confirmed against the real installed Canon PPD's own
// `*OpenUI *CNDuplex/Print Style: PickOne`.
var ppdOpenUIRe = regexp.MustCompile(`^\*OpenUI \*(\S+?)(?:/[^:]*)?:`)

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
			choice := rest
			if idx := strings.IndexAny(choice, "/:"); idx >= 0 {
				choice = choice[:idx]
			}
			if choice = strings.TrimSpace(choice); choice != "" {
				building.choices = append(building.choices, choice)
			}
		}
	}
	if inBlock {
		result = append(result, building)
	}
	return result
}

// pickChoice returns the first choice (its "*"-default marker stripped)
// whose lowercased text contains none of avoid, and - when want is non-empty -
// also contains at least one of want. An empty want matches any choice not in
// avoid (used to pick "whatever's left once the mono-ish choices are
// excluded" for a color request, where there's no single positive keyword to
// require). The same substring-based approach devmode_windows.go's own
// driver-specific quirks already accept as unavoidable given how
// inconsistently vendors name PPD options and choices; unlike Windows' fixed
// DEVMODE fields, there is no single correct keyword/value pair to hardcode
// here.
func pickChoice(choices []string, want, avoid []string) (string, bool) {
	for _, raw := range choices {
		c := strings.TrimPrefix(raw, "*")
		lower := strings.ToLower(c)
		if containsAny(lower, avoid) {
			continue
		}
		if len(want) == 0 || containsAny(lower, want) {
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
func decidePrintDefaults(opts []ppdOption, oneSided, mono bool, subject string) (toSet, warnings []string) {
	if duplex, ok := findOption(opts, "duplex", "cnduplex"); ok {
		if oneSided {
			if choice, ok := pickChoice(duplex.choices, []string{"none", "simplex"}, nil); ok {
				toSet = append(toSet, duplex.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("%s's PPD has no simplex/None choice for %s; leaving duplex as-is", subject, duplex.keyword))
			}
		} else {
			if choice, ok := pickChoice(duplex.choices, []string{"notumble"}, nil); ok {
				toSet = append(toSet, duplex.keyword+"="+choice)
			} else if choice, ok := pickChoice(duplex.choices, []string{"tumble", "duplex"}, []string{"none"}); ok {
				toSet = append(toSet, duplex.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("%s's PPD has no two-sided choice for %s; leaving duplex as-is", subject, duplex.keyword))
			}
		}
	} else {
		warnings = append(warnings, fmt.Sprintf("%s's PPD declares no Duplex option; leaving duplex as-is", subject))
	}

	if colorModel, ok := findOption(opts, "colormodel", "cncolormode"); ok {
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
// problem). ppdPath == "" (the `-m everywhere`/IPP-Everywhere fallback, no
// local PPD file to read at all) returns no args and no warnings - Deploy's
// own caller falls back to SetPrintDefaults against the live queue instead
// once it exists, the one case this can't handle.
func PrintDefaultsForNewQueue(rowName, ppdPath string, oneSided, mono bool) (toSet, warnings []string) {
	if ppdPath == "" {
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
// "ColorModel", and Canon's own "CNDuplex"/"CNColorMode" - add a new exact
// spelling here if a future vendor's PPD needs one, never widen this back to
// a suffix/substring match.
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
