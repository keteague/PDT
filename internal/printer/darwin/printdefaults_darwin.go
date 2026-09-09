package darwin

import (
	"context"
	"fmt"
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

// SetPrintDefaults best-effort applies duplex/color defaults to queueName,
// returning warning strings rather than an error - the direct analog of
// deploy_windows.go's own best-effort/[WARN] treatment of duplex/color via
// devmode_windows.go, adapted for PPD option keywords/choices instead of
// fixed DEVMODE fields (see pickChoice's doc comment for why). A PPD with no
// Duplex/ColorModel option at all, or none of whose choices look like what
// was asked for, produces a warning for that one setting and leaves the rest
// alone - never fails the row.
func SetPrintDefaults(ctx context.Context, queueName string, oneSided, mono bool) []string {
	var warnings []string

	opts, err := listPPDOptions(ctx, queueName)
	if err != nil {
		return []string{fmt.Sprintf("could not read PPD options for %q: %v", queueName, err)}
	}

	var toSet []string

	if duplex, ok := findOption(opts, "duplex"); ok {
		if oneSided {
			if choice, ok := pickChoice(duplex.choices, []string{"none", "simplex"}, nil); ok {
				toSet = append(toSet, duplex.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("queue %q's PPD has no simplex/None choice for %s; leaving duplex as-is", queueName, duplex.keyword))
			}
		} else {
			if choice, ok := pickChoice(duplex.choices, []string{"notumble"}, nil); ok {
				toSet = append(toSet, duplex.keyword+"="+choice)
			} else if choice, ok := pickChoice(duplex.choices, []string{"tumble", "duplex"}, []string{"none"}); ok {
				toSet = append(toSet, duplex.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("queue %q's PPD has no two-sided choice for %s; leaving duplex as-is", queueName, duplex.keyword))
			}
		}
	} else {
		warnings = append(warnings, fmt.Sprintf("queue %q's PPD declares no Duplex option; leaving duplex as-is", queueName))
	}

	if colorModel, ok := findOption(opts, "colormodel"); ok {
		if mono {
			if choice, ok := pickChoice(colorModel.choices, []string{"gray", "grey", "mono", "black"}, nil); ok {
				toSet = append(toSet, colorModel.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("queue %q's PPD has no grayscale/mono choice for %s; leaving color mode as-is", queueName, colorModel.keyword))
			}
		} else {
			if choice, ok := pickChoice(colorModel.choices, nil, []string{"gray", "grey", "mono", "black"}); ok {
				toSet = append(toSet, colorModel.keyword+"="+choice)
			} else {
				warnings = append(warnings, fmt.Sprintf("queue %q's PPD has no color choice for %s; leaving color mode as-is", queueName, colorModel.keyword))
			}
		}
	} else {
		warnings = append(warnings, fmt.Sprintf("queue %q's PPD declares no ColorModel option; leaving color mode as-is", queueName))
	}

	if len(toSet) == 0 {
		return warnings
	}

	argv := append([]string{"lpadmin", "-p", queueName}, optionArgs(toSet)...)
	if _, err := runPrivileged(ctx, argv); err != nil {
		warnings = append(warnings, fmt.Sprintf("could not set print defaults for %q: %v", queueName, err))
	}
	return warnings
}

func findOption(opts []ppdOption, keywordLower string) (ppdOption, bool) {
	for _, o := range opts {
		if strings.ToLower(o.keyword) == keywordLower {
			return o, true
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
