package driver

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// mountPointRe pulls the mount-point string out of `hdiutil attach -plist`'s
// XML output. Confirmed against a real vendor image (Kyocera's macOS
// driver .dmg): only the system-entities dict for the actual mountable
// volume carries a <key>mount-point</key>, sibling partition-map/scheme
// entries never do - a plain line-pair regex is enough, no need for a full
// plist parser for this one field.
var mountPointRe = regexp.MustCompile(`(?s)<key>mount-point</key>\s*<string>(.*?)</string>`)

// openDmg is macmount.go's own openDmg seam on darwin: attaches path
// read-only and not in the Finder (-nobrowse) via the real `hdiutil`, and
// returns its mount point plus a detach func that unmounts it - always call
// detach once done, even on a later error, so a failed driver install
// doesn't leave a mounted volume behind. Transparently decompresses a
// ".dmg.gz" path to a temp file first (see decompressGzipToTemp,
// macmount.go - shared with the Windows implementation, pure Go) - the
// mounted volume still needs that decompressed copy to exist on disk for as
// long as it stays mounted, so its own cleanup is folded into detach, not
// run immediately after attaching.
//
// Moved here verbatim from macmount.go's own former mountDmg (GitHub issue
// #3, Phase 1) - zero behavior change, just renamed to match the seam every
// caller (locatePkgWithChainFromRealPath/locateLoosePPDsFromRealPath) now
// goes through, so a Windows-native implementation (macdmgopen_windows.go,
// via the bundled 7z.exe - real .dmg containers need no live mount there,
// just an on-disk extraction) can stand in under the exact same name/
// signature.
func openDmg(path string) (root string, cleanup func() error, err error) {
	attachPath := path
	tmpCleanup := func() {}
	if strings.HasSuffix(strings.ToLower(path), ".dmg.gz") {
		decompressed, dcleanup, derr := decompressGzipToTemp(path)
		if derr != nil {
			return "", nil, fmt.Errorf("decompressing %s: %w", path, derr)
		}
		attachPath = decompressed
		tmpCleanup = dcleanup
	}

	out, err := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-plist", attachPath).Output()
	if err != nil {
		tmpCleanup()
		return "", nil, fmt.Errorf("mounting %s: %w", path, err)
	}
	m := mountPointRe.FindSubmatch(out)
	if m == nil {
		tmpCleanup()
		return "", nil, fmt.Errorf("mounting %s: no mountable volume found in hdiutil output", path)
	}
	mountPoint := string(m[1])
	detach := func() error {
		err := exec.Command("hdiutil", "detach", mountPoint, "-quiet").Run()
		tmpCleanup()
		return err
	}
	return mountPoint, detach, nil
}
