package driver

import "io"

// cpioExtractGlob is macppd.go/mackyocera.go/maccanonselective.go's own
// cpioExtractGlob seam on Windows: no system cpio exists here, so this goes
// through the hand-rolled "odc" reader in maccpioodc.go (GitHub issue #3
// Phase 3 - real macOS Payload archives turned out to use the older "odc"
// portable-ASCII cpio format, not "newc", confirmed live against a real
// Kyocera Payload) instead - cpioMatchAny gives it the same "match any of
// several glob patterns" semantics `cpio -idm pattern1 pattern2 ...` has.
func cpioExtractGlob(r io.Reader, destDir string, patterns []string) error {
	return cpioOdcExtract(r, destDir, cpioMatchAny(patterns))
}

// cpioExtractAll is extractAllFromPayload's own seam on Windows: a nil match
// func extracts everything, the same as darwin's own plain `cpio -idm` with
// no glob patterns.
func cpioExtractAll(r io.Reader, destDir string) error {
	return cpioOdcExtract(r, destDir, nil)
}

// cpioListEntries is listPayloadEntries's own seam on Windows.
func cpioListEntries(r io.Reader) ([]string, error) {
	return cpioOdcList(r)
}
