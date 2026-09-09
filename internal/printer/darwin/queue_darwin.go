package darwin

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// lpstatDeviceLineRe matches one "device for <name>: <uri>" line from
// `lpstat -v` - confirmed against this machine's own real, already-deployed
// queues, e.g. "device for Jenks_Kyocera: lpd://10.58.13.21/".
var lpstatDeviceLineRe = regexp.MustCompile(`(?m)^device for (\S+): (\S+)$`)

// findExistingQueue looks for a CUPS queue already pointed at deviceURI
// (exact match) via `lpstat -v` (unprivileged) - the macOS equivalent of
// portlookup_windows.go's registry-based reuse-existing-port lookup, and used
// the same way: reuse rather than ever create a duplicate queue for a host
// that already has one.
func findExistingQueue(ctx context.Context, deviceURI string) (name string, found bool, err error) {
	out, err := exec.CommandContext(ctx, "lpstat", "-v").Output()
	if err != nil {
		// lpstat -v exits non-zero when there are no queues at all - not a
		// real error, same "nothing found" outcome as an empty match.
		return "", false, nil
	}
	for _, m := range lpstatDeviceLineRe.FindAllStringSubmatch(string(out), -1) {
		if m[2] == deviceURI {
			return m[1], true, nil
		}
	}
	return "", false, nil
}

// QueueInfo is one queue's currently-configured device-uri, plus its cached
// PPD path for display only - what findExistingQueue's caller shows in the
// confirmation prompt before changing an existing queue, mirroring
// deploy_windows.go's own existing-printer diff. Only DeviceURI is
// meaningfully comparable against a row's desired state: PPDInterfacePath is
// CUPS' own cached copy of the PPD (confirmed against a real queue on this
// machine: `lpstat -l -p <name>` reports a path under /private/etc/cups/ppd/,
// never the original source path under /Library/Printers/PPDs/... a fresh
// install would resolve to), so there's no reliable way to diff "is this the
// same PPD" by path equality - deploy_darwin.go decides whether to reinstall
// the driver from the row's own resolved selection, not from this path.
type QueueInfo struct {
	DeviceURI        string
	PPDInterfacePath string
}

var lpstatInterfaceRe = regexp.MustCompile(`(?m)Interface:\s*(\S+\.ppd)`)

// currentQueueInfo reads name's current device-uri (via lpstat -v, same
// parsing as findExistingQueue) and cached PPD interface path (via
// `lpstat -l -p <name>`'s own "Interface: <path>" line - absent for a queue
// created with -m everywhere/no classic PPD).
func currentQueueInfo(ctx context.Context, name string) (QueueInfo, bool) {
	out, err := exec.CommandContext(ctx, "lpstat", "-v", name).Output()
	if err != nil {
		return QueueInfo{}, false
	}
	m := lpstatDeviceLineRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return QueueInfo{}, false
	}
	info := QueueInfo{DeviceURI: m[2]}

	if longOut, err := exec.CommandContext(ctx, "lpstat", "-l", "-p", name).Output(); err == nil {
		if im := lpstatInterfaceRe.FindStringSubmatch(string(longOut)); im != nil {
			info.PPDInterfacePath = im[1]
		}
	}
	return info, true
}

// QueueOptions are the non-driver, non-device settings EnsureQueue applies.
type QueueOptions struct {
	Description string
	Location    string
	Shared      bool
}

// EnsureQueue creates or reconfigures the CUPS queue named name against
// deviceURI. ppdPath is a specific PPD file to use (`-P`); when ppdPath is
// empty, falls back to `-m everywhere` (IPP-Everywhere autoconfiguration) -
// see EnsureDriverInstalled's own doc comment for when that fallback applies.
func EnsureQueue(ctx context.Context, name, deviceURI, ppdPath string, opts QueueOptions) error {
	argv := []string{"lpadmin", "-p", name, "-E", "-v", deviceURI}
	if ppdPath != "" {
		argv = append(argv, "-P", ppdPath)
	} else {
		argv = append(argv, "-m", "everywhere")
	}
	if opts.Description != "" {
		argv = append(argv, "-D", opts.Description)
	}
	if opts.Location != "" {
		argv = append(argv, "-L", opts.Location)
	}
	argv = append(argv, "-o", "printer-is-shared="+strconv.FormatBool(opts.Shared))

	if _, err := runPrivileged(ctx, argv); err != nil {
		return fmt.Errorf("configuring queue %q: %w", name, err)
	}
	return nil
}
