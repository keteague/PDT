package driver

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// cpioExtractGlob is macppd.go/mackyocera.go/maccanonselective.go's own
// cpioExtractGlob seam on darwin: the real `cpio -idm`, exactly as every
// call site already used inline before GitHub issue #3 Phase 3 - moved here
// verbatim, zero behavior change, so a Windows-native implementation
// (maccpio_windows.go, via the hand-rolled "newc" reader in maccpionewc.go -
// no system cpio exists on Windows at all) can stand in under the same
// name/signature. Unlike the original inline calls (which were always
// best-effort, `_ = cmd.Run()`), this always returns a real error with
// combined output for diagnosis - callers that were already best-effort
// simply discard it (`_ = cpioExtractGlob(...)`), preserving their own
// existing behavior exactly; callers that already propagated a real error
// (ExtractKyoceraPPD, ExtractCanonDeviceFiles) now get one from here
// directly instead of building it from a subprocess's own CombinedOutput.
func cpioExtractGlob(r io.Reader, destDir string, patterns []string) error {
	cmd := exec.Command("cpio", append([]string{"-idm", "--quiet"}, patterns...)...)
	cmd.Dir = destDir
	cmd.Stdin = r
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cpio: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cpioExtractAll is extractAllFromPayload's own seam on darwin: the real
// `cpio -idm` with no glob patterns at all (extracts everything) - moved
// verbatim from its own former inline exec.Command, zero behavior change.
func cpioExtractAll(r io.Reader, destDir string) error {
	cmd := exec.Command("cpio", "-idm", "--quiet")
	cmd.Dir = destDir
	cmd.Stdin = r
	return cmd.Run()
}

// cpioListEntries is listPayloadEntries's own seam on darwin: the real
// `cpio -it`, moved verbatim from its own former inline exec.Command, zero
// behavior change.
func cpioListEntries(r io.Reader) ([]string, error) {
	cmd := exec.Command("cpio", "-it", "--quiet")
	cmd.Stdin = r
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	entries := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			entries = append(entries, l)
		}
	}
	return entries, nil
}
