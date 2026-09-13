package driver

import (
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// kyoceraPPDInstallerID is the Kyocera "Web Build" Distribution's own real,
// stable bundle identifier for its baseline PPD-only sub-package - confirmed
// against a real "Kyocera Web build 2026.07.03" download. Far more reliable
// across different Kyocera downloads than a file name would be: Kyocera's
// own web-based driver-builder tool names the outer .dmg/.pkg after the
// build date, but these internal installer bundle identifiers aren't
// user-facing and have no reason to change between builds.
const kyoceraPPDInstallerID = "com.kyocera.ppd"

// kyoceraRedundantPPDIDs are two further choices confirmed to ship the exact
// byte-identical PPD payload as kyoceraPPDInstallerID ("Duplex On" and "Net
// Manager On" in the installer's own choice titles), differing only in a
// default option value their own postinstall script patches in afterward
// (*DefaultDuplex/*KNMSupport respectively). Since PDT sets duplex/color
// defaults itself via lpadmin after queue creation regardless of what a PPD
// ships as its own factory default, neither patched variant is ever useful
// here - installing either wastes real time for a result PDT immediately
// overwrites anyway. Confirmed live both are genuinely slow, not just
// redundant: each relocates all ~460 PPDs through a per-file `sed -i`
// (which itself creates a `.backup` file per invocation on macOS) plus `cp`
// shell loop from a staging location into the real PPD directory, and each
// first re-scans every PPD *already installed* on the machine looking for
// duplicate device IDs to remove - a cost that grows with how many printers
// have already been deployed from that laptop, not just with this package.
var kyoceraRedundantPPDIDs = map[string]bool{
	"com.kyocera.ppd2": true, // "Duplex On"
	"com.kyocera.ppd3": true, // "Net Manager On"
}

// kyoceraSkipInstallIDs are choices that install for real but are known to
// fail (or aren't worth the risk of failing) under PDT's own privileged-
// install mechanism, on top of the redundant PPD-set ones above.
//
// "com.kyocera.printpanel" ("Print Panel App", a GUI status/monitoring
// utility) is the ONLY one of Kyocera's 19 choices whose own PackageInfo
// declares `install-location="/Applications"` - every other real choice
// targets `/Library/...`, `/usr/libexec/cups/filter`, or
// `/Library/PreferencePanes`. Confirmed live: installing it through `do
// shell script ... with administrator privileges` fails with
// `PKInstallErrorDomain Code=120 "An unexpected error occurred while moving
// files to the final destination."`, whose own underlying error is
// `NSPOSIXErrorDomain Code=1 "Operation not permitted"` - PackageKit's own
// sandboxed install can't complete its move into /Applications specifically
// under this elevation mechanism, even though the same osascript-granted
// root authorization installs every other real choice (all /Library/...
// or /usr targets) without issue. Very likely a TCC/SIP-related restriction
// specific to /Applications that a real, GUI-driven Installer.app run
// wouldn't hit (Ken's own earlier manual GUI install of the full Kyocera
// package completed without this failure) - not something `-X`/`--flatten`-
// style workarounds can fix, since the failure is inside `installer`'s own
// internal sandbox-to-destination move, not anything PDT's own script
// controls. Skipped rather than risked: a GUI status app isn't required for
// actual CUPS printing (the real functional pieces - CUPS filters, PDEs,
// the driver Framework - all install to /Library or /usr and succeeded).
var kyoceraSkipInstallIDs = map[string]bool{
	"com.kyocera.printpanel": true, // "Print Panel App" - install-location=/Applications, fails under this elevation mechanism
}

// kyoceraDistributionPkgRefs parses a Kyocera "Web Build" Distribution's own
// top-level `<pkg-ref id="..." ...>#<url-encoded filename>.pkg</pkg-ref>`
// elements - direct children of `<installer-script>`, distinct from the
// empty `<pkg-ref id="..."/>` siblings nested inside each `<choice>` (Go's
// xml package only binds a tagged field to direct children of the type
// being unmarshaled into, which conveniently already excludes those) - into
// an id -> real sub-package directory name map.
func kyoceraDistributionPkgRefs(expandedDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(expandedDir, "Distribution"))
	if err != nil {
		return nil, err
	}
	var dist struct {
		PkgRefs []struct {
			ID      string `xml:"id,attr"`
			Content string `xml:",chardata"`
		} `xml:"pkg-ref"`
	}
	if err := xml.Unmarshal(data, &dist); err != nil {
		return nil, err
	}
	refs := make(map[string]string)
	for _, r := range dist.PkgRefs {
		content := strings.TrimPrefix(strings.TrimSpace(r.Content), "#")
		if content == "" {
			continue
		}
		name, err := url.QueryUnescape(content)
		if err != nil {
			continue
		}
		refs[r.ID] = name
	}
	return refs, nil
}

// KyoceraSelectivePackages locates, within an expanded Kyocera "Web Build"
// Distribution, the baseline PPD-only sub-package (ppdInstallerPkgPath -
// selectively extracted per model, never installed via `installer` at all -
// see ExtractKyoceraPPD) and every *other* sub-package that isn't one of the
// two confirmed-redundant PPD-default-patching variants (otherPkgPaths -
// installed for real, the actual driver framework/CUPS filters/PDEs/Print
// Panel App/etc - Kyocera's own analog of Canon's single Core.pkg, just
// split across more, smaller pieces). ok is false when the Distribution
// doesn't declare the known PPD-installer identifier at all - an unexpected
// shape (a different Kyocera package generation, or a future "Web Build"
// layout change) - the caller falls back to installing the whole
// Distribution the old way rather than guessing further.
func KyoceraSelectivePackages(expandedDir string) (ppdInstallerPkgPath string, otherPkgPaths []string, ok bool) {
	refs, err := kyoceraDistributionPkgRefs(expandedDir)
	if err != nil {
		return "", nil, false
	}
	ppdName, hasPPD := refs[kyoceraPPDInstallerID]
	if !hasPPD {
		return "", nil, false
	}
	ppdInstallerPkgPath = filepath.Join(expandedDir, ppdName)
	if _, err := os.Stat(ppdInstallerPkgPath); err != nil {
		return "", nil, false
	}
	for id, name := range refs {
		if id == kyoceraPPDInstallerID || kyoceraRedundantPPDIDs[id] || kyoceraSkipInstallIDs[id] {
			continue
		}
		p := filepath.Join(expandedDir, name)
		if _, err := os.Stat(p); err == nil {
			otherPkgPaths = append(otherPkgPaths, p)
		}
	}
	sort.Strings(otherPkgPaths)
	return ppdInstallerPkgPath, otherPkgPaths, true
}

// kyoceraRestrictSubPackages is indexFamilyPackage's own restrictor for
// Kyocera (see packagePPDEntriesFiltered) - catalog indexing only walks the
// baseline PPD-installer sub-package's own Payload for *.ppd(.gz) entries,
// never the two confirmed byte-identical redundant variants. Without this,
// BuildMacModelIndex would index every model 3 times over with completely
// duplicate variants (same NickName, same content, different sub-package of
// origin) - confirmed live that all 3 really do contain the identical PPD
// set byte-for-byte.
func kyoceraRestrictSubPackages(expandedDir string) (map[string]bool, bool) {
	ppdPath, _, ok := KyoceraSelectivePackages(expandedDir)
	if !ok {
		return nil, false
	}
	return map[string]bool{filepath.Base(ppdPath): true}, true
}

// ExtractKyoceraPPD selectively cpio-extracts just one model's own PPD out
// of ppdInstallerPkgPath's gzip-compressed Payload into destDir - Kyocera's
// own real PPDs sit as flat files directly at the Payload's own root
// (confirmed live: the BOM's own paths are plain "./Kyocera CS 205c.ppd",
// with no Recipe-bundle-style companion structure the way Canon's Device.pkg
// has), so unlike ExtractCanonDeviceFiles there's no nested destination
// path to preserve and no sibling files to also extract - destDir/<filename>
// is the whole result, ready to copy directly into
// /Library/Printers/PPDs/Contents/Resources.
func ExtractKyoceraPPD(ppdInstallerPkgPath, ppdFilename, destDir string) error {
	payloadPath := filepath.Join(ppdInstallerPkgPath, "Payload")
	f, err := os.Open(payloadPath)
	if err != nil {
		return fmt.Errorf("no Payload under %s: %w", ppdInstallerPkgPath, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s's Payload is not gzip-compressed as expected: %w", ppdInstallerPkgPath, err)
	}
	defer gz.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	cmd := exec.Command("cpio", "-idm", "--quiet", "./"+ppdFilename)
	cmd.Dir = destDir
	cmd.Stdin = gz
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extracting %s from %s: %w: %s", ppdFilename, ppdInstallerPkgPath, err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(filepath.Join(destDir, ppdFilename)); err != nil {
		return fmt.Errorf("cpio extracted nothing for %q from %s - the PPD installer's own layout may have changed", ppdFilename, ppdInstallerPkgPath)
	}
	return nil
}
