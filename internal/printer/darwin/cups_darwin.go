package darwin

import (
	"context"
	"os/exec"
	"strings"
)

// WebInterfaceEnabled reports whether CUPS' own web admin UI
// (http://localhost:631) is currently turned on - a plain, unprivileged
// `cupsctl` query (no arguments prints every current server setting, one per
// line, e.g. "WebInterface=No"). Unlike SetWebInterfaceEnabled below,
// reading this needs no elevation. A line for WebInterface missing entirely
// from the output (shouldn't happen on a real cupsd, but cheap to handle) is
// treated as off, matching CUPS' own documented default.
func WebInterfaceEnabled(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, "cupsctl").Output()
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || !strings.EqualFold(key, "WebInterface") {
			continue
		}
		return strings.EqualFold(strings.TrimSpace(value), "yes"), nil
	}
	return false, nil
}

// SetWebInterfaceEnabled turns CUPS' own web admin UI on or off -
// `cupsctl WebInterface=yes`/`=no`, the exact command a technician would run
// by hand. Needs administrator privileges the same way every other CUPS
// server-setting change in this codebase already does (lpadmin, install -
// see runPrivileged's own doc comment): a plain unprivileged
// `cupsctl WebInterface=...` fails with "Unauthorized" on a real machine.
func SetWebInterfaceEnabled(ctx context.Context, enabled bool) error {
	value := "no"
	if enabled {
		value = "yes"
	}
	_, err := runPrivileged(ctx, []string{"cupsctl", "WebInterface=" + value})
	return err
}
