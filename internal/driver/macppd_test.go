package driver

import (
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildTestPayload creates a real gzip-compressed cpio "Payload" file at
// payloadPath containing exactly the given files (name -> content), using
// the system `cpio`/`gzip` tools directly - the same real format
// pkgutil --expand leaves every sub-package's own Payload in, confirmed via
// `file` elsewhere in this codebase. Lets extractPPDsFromExpandedPkgFiltered
// be tested against real archive bytes without needing a real downloaded
// vendor package.
func buildTestPayload(t *testing.T, payloadPath string, files map[string]string) {
	t.Helper()
	if _, err := exec.LookPath("cpio"); err != nil {
		t.Skip("cpio not on PATH (not running on macOS/Linux) - cannot build a real test Payload archive")
	}
	srcDir := t.TempDir()
	var names []string
	for name, content := range files {
		p := filepath.Join(srcDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("creating fixture directory for %s: %v", name, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("writing fixture file %s: %v", name, err)
		}
		names = append(names, name)
	}

	cpioCmd := exec.Command("cpio", "-o", "--quiet")
	cpioCmd.Dir = srcDir
	cpioCmd.Stdin = strings.NewReader(strings.Join(names, "\n") + "\n")
	cpioOut, err := cpioCmd.Output()
	if err != nil {
		t.Fatalf("building test cpio archive: %v", err)
	}

	gzipCmd := exec.Command("gzip", "-c")
	gzipCmd.Stdin = bytes.NewReader(cpioOut)
	gzOut, err := gzipCmd.Output()
	if err != nil {
		t.Fatalf("gzip-compressing test payload: %v", err)
	}
	if err := os.WriteFile(payloadPath, gzOut, 0o644); err != nil {
		t.Fatalf("writing test Payload: %v", err)
	}
}

// TestExtractPPDsFromExpandedPkg_CaseInsensitiveExtensions guards a real bug
// found live: cpio's own glob matching is case-sensitive, and a real
// Kyocera "Web Build" download mixes both extension cases in the same
// sub-package (confirmed live: 124 real PPDs named "*.ppd", 336 named
// "*.PPD" - a lowercase-only cpio pattern silently dropped 73% of Kyocera's
// own real model coverage, only noticed because the indexed model count
// came back suspiciously low against the real BOM's own 460-file count).
func TestExtractPPDsFromExpandedPkg_CaseInsensitiveExtensions(t *testing.T) {
	expandDir := t.TempDir()
	subPkg := filepath.Join(expandDir, "Test.pkg")
	if err := os.MkdirAll(subPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	buildTestPayload(t, filepath.Join(subPkg, "Payload"), map[string]string{
		"lowercase.ppd": "*NickName: \"Lowercase Model\"\n",
		"UPPERCASE.PPD": "*NickName: \"Uppercase Model\"\n",
		"not-a-ppd.txt": "irrelevant\n",
	})

	destDir := t.TempDir()
	subs := extractPPDsFromExpandedPkgFiltered(expandDir, destDir, nil, nil)
	if len(subs) != 1 {
		t.Fatalf("got %d sub-package results, want 1: %+v", len(subs), subs)
	}

	if _, err := os.Stat(filepath.Join(destDir, "Test.pkg", "lowercase.ppd")); err != nil {
		t.Errorf("lowercase.ppd was not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Test.pkg", "UPPERCASE.PPD")); err != nil {
		t.Errorf("UPPERCASE.PPD was not extracted (the real bug this guards against): %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Test.pkg", "not-a-ppd.txt")); err == nil {
		t.Errorf("not-a-ppd.txt was extracted, want it excluded")
	}
}

// gzipString compresses s the same way a real PPD ships inside a legacy
// Apple-distributed Ricoh bundle - see
// TestExtractPPDsFromExpandedPkg_RicohLegacyPathFragmentFallback.
func gzipString(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(s)); err != nil {
		t.Fatalf("gzip-compressing fixture content: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}
	return buf.String()
}

// TestExtractPPDsFromExpandedPkg_RicohNoExtensionFallback guards a real gap
// found live: a real Ricoh "Web Build"-style download's own baseline PPD
// sub-package carries no file extension on any of its real PPDs at all
// ("RICOH IM C3000", not "RICOH IM C3000.ppd") - the fast, extension-based
// cpio glob can never match them. ricohPPDExtractionFallback's own
// install-location signal should still find them, and looksLikeRealPPD
// should still exclude a same-Payload, non-PPD file even in the fallback
// path (never trust "this sub-package is Ricoh's own ppds.pkg" as license to
// keep everything it contains, only what really is a PPD).
func TestExtractPPDsFromExpandedPkg_RicohNoExtensionFallback(t *testing.T) {
	expandDir := t.TempDir()
	subPkg := filepath.Join(expandDir, "ppds.pkg")
	if err := os.MkdirAll(subPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	packageInfo := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<pkg-info identifier="com.RICOH.print.IM_C300_C400.ppds.pkg" version="1.0" install-location="/Library/Printers/PPDs/Contents/Resources/"/>`
	if err := os.WriteFile(filepath.Join(subPkg, "PackageInfo"), []byte(packageInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	buildTestPayload(t, filepath.Join(subPkg, "Payload"), map[string]string{
		"RICOH IM C300": "*PPD-Adobe: \"4.3\"\n*NickName: \"RICOH IM C300 PS\"\n",
		"RICOH IM C400": "*PPD-Adobe: \"4.3\"\n*NickName: \"RICOH IM C400 PS\"\n",
		"AutoSetupTool": "not a PPD at all, just happens to share this Payload\n",
	})

	destDir := t.TempDir()
	subs := extractPPDsFromExpandedPkgFiltered(expandDir, destDir, nil, macSubPackagePPDFallback("Ricoh"))
	if len(subs) != 1 {
		t.Fatalf("got %d sub-package results, want 1: %+v", len(subs), subs)
	}
	if _, err := os.Stat(filepath.Join(destDir, "ppds.pkg", "RICOH IM C300")); err != nil {
		t.Errorf("RICOH IM C300 was not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "ppds.pkg", "RICOH IM C400")); err != nil {
		t.Errorf("RICOH IM C400 was not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "ppds.pkg", "AutoSetupTool")); err == nil {
		t.Errorf("AutoSetupTool was extracted, want it excluded (not a real PPD by content)")
	}
}

// TestExtractPPDsFromExpandedPkg_RicohLegacyPathFragmentFallback guards the
// other real shape found live: a legacy Apple-distributed Ricoh bundle
// ("RicohPrinterDrivers.pkg") declares no install-location of its own at
// all - its real PPDs (named "<model>.gz", still no ".ppd" anywhere) sit
// alongside hundreds of unrelated driver-framework/PDE-plugin files in the
// very same Payload, distinguishable only by their own real
// ".../PPDs/Contents/Resources/..." path. The fallback must extract only
// the matched path, never the whole Payload.
func TestExtractPPDsFromExpandedPkg_RicohLegacyPathFragmentFallback(t *testing.T) {
	expandDir := t.TempDir()
	subPkg := filepath.Join(expandDir, "RicohPrinterDrivers.pkg")
	if err := os.MkdirAll(subPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	packageInfo := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<pkg-info identifier="com.apple.pkg.RicohPrinterDrivers" version="10.6.0"/>`
	if err := os.WriteFile(filepath.Join(subPkg, "PackageInfo"), []byte(packageInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	realPPD := gzipString(t, "*PPD-Adobe: \"4.3\"\n*NickName: \"RICOH Aficio 3224C PS\"\n")
	buildTestPayload(t, filepath.Join(subPkg, "Payload"), map[string]string{
		"Library/Printers/PPDs/Contents/Resources/RICOH Aficio 3224C.gz":     realPPD,
		"Library/Printers/RICOH/PDEs/BalanceadjustmentRV1.plugin/Info.plist": "<plist>not a ppd</plist>\n",
	})

	destDir := t.TempDir()
	subs := extractPPDsFromExpandedPkgFiltered(expandDir, destDir, nil, macSubPackagePPDFallback("Ricoh"))
	if len(subs) != 1 {
		t.Fatalf("got %d sub-package results, want 1: %+v", len(subs), subs)
	}
	ppdPath := filepath.Join(destDir, "RicohPrinterDrivers.pkg", "Library/Printers/PPDs/Contents/Resources/RICOH Aficio 3224C.gz")
	if _, err := os.Stat(ppdPath); err != nil {
		t.Errorf("the real PPD was not extracted: %v", err)
	}
	pluginPath := filepath.Join(destDir, "RicohPrinterDrivers.pkg", "Library/Printers/RICOH/PDEs/BalanceadjustmentRV1.plugin/Info.plist")
	if _, err := os.Stat(pluginPath); err == nil {
		t.Errorf("an unrelated plugin file was extracted, want only the real PPD pulled from this Payload")
	}
}

// TestExtractDriverFootprint_MultiSubPackageRicohShape guards issue #12's
// own fallback extraction against the real gap that motivated it: a real
// Ricoh package (2026-09-16, ppds.pkg install-location
// "/Library/Printers/PPDs/Contents/Resources/", CupsFilter.pkg
// install-location "/Library/Printers/RICOH/Filters/") splits the PPD and
// its own supporting filter binary across two separate sub-packages -
// pathFragmentPPDExtractionFallback's own PPD-only extraction (used for
// cataloging/defaults-reading) never touches the second one at all.
// extractDriverFootprint must pull files from both qualifying sub-packages
// into the same destDir, while leaving a third, non-driver-relevant
// sub-package (UserAuthentication.pkg, a real Ricoh install-location
// confirmed live, "/Applications/") untouched.
func TestExtractDriverFootprint_MultiSubPackageRicohShape(t *testing.T) {
	expandDir := t.TempDir()

	ppdsPkg := filepath.Join(expandDir, "ppds.pkg")
	if err := os.MkdirAll(ppdsPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ppdsPkg, "PackageInfo"), []byte(
		`<?xml version="1.0" encoding="UTF-8"?><pkg-info identifier="com.RICOH.print.IM_C3000.ppds.pkg" version="1.13.0" install-location="/Library/Printers/PPDs/Contents/Resources/"/>`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	buildTestPayload(t, filepath.Join(ppdsPkg, "Payload"), map[string]string{
		"RICOH IM C3000": "*PPD-Adobe: \"4.3\"\n*NickName: \"RICOH IM C3000 PS\"\n*cupsFilter: \"application/vnd.cups-postscript 0 /Library/Printers/RICOH/Filters/pstopsRV2.app/Contents/MacOS/pstopsRV2\"\n",
	})

	cupsFilterPkg := filepath.Join(expandDir, "CupsFilter.pkg")
	if err := os.MkdirAll(cupsFilterPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cupsFilterPkg, "PackageInfo"), []byte(
		`<?xml version="1.0" encoding="UTF-8"?><pkg-info identifier="ricoh.cupsfilter.pkg" version="3.0.0" install-location="/Library/Printers/RICOH/Filters/"/>`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	buildTestPayload(t, filepath.Join(cupsFilterPkg, "Payload"), map[string]string{
		"pstopsRV2.app/Contents/MacOS/pstopsRV2": "#!/bin/sh\necho fake filter binary\n",
	})

	userAuthPkg := filepath.Join(expandDir, "UserAuthentication.pkg")
	if err := os.MkdirAll(userAuthPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userAuthPkg, "PackageInfo"), []byte(
		`<?xml version="1.0" encoding="UTF-8"?><pkg-info identifier="ricoh.userauthentication.pkg" version="1.0.0" install-location="/Applications/"/>`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	buildTestPayload(t, filepath.Join(userAuthPkg, "Payload"), map[string]string{
		"RicohUserAuthApp.app/Contents/MacOS/RicohUserAuthApp": "not driver/print-path-relevant\n",
	})

	destDir := t.TempDir()
	if !extractDriverFootprint(expandDir, destDir) {
		t.Fatal("extractDriverFootprint returned false, want true - two qualifying sub-packages should have yielded files")
	}

	if _, err := os.Stat(filepath.Join(destDir, "Library", "Printers", "PPDs", "Contents", "Resources", "RICOH IM C3000")); err != nil {
		t.Errorf("the real PPD (ppds.pkg) was not extracted to its own real install-location-relative destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Library", "Printers", "RICOH", "Filters", "pstopsRV2.app", "Contents", "MacOS", "pstopsRV2")); err != nil {
		t.Errorf("the filter binary (CupsFilter.pkg, a *separate* sub-package from the PPD) was not extracted - this is the real gap issue #12's fallback exists to close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Applications", "RicohUserAuthApp.app")); err == nil {
		t.Error("UserAuthentication.pkg's own content was extracted, want it left alone (install-location /Applications/ is not driver-relevant)")
	}
}

// TestExtractDriverFootprint_WholeDriverInstallLocation guards the other
// real shape confirmed live (2026-09-16, both Xerox and Sharp): a single
// sub-package declaring install-location "/" itself, whose own payload
// entries already carry their real absolute destination path baked in -
// unlike the Ricoh shape above. printersPathFragment must still pick out
// only the driver-relevant subset (Xerox's own real package also ships a
// LaunchDaemon plist and an unrelated analytics framework in the very same
// Payload, confirmed live) rather than extracting everything.
func TestExtractDriverFootprint_WholeDriverInstallLocation(t *testing.T) {
	expandDir := t.TempDir()
	subPkg := filepath.Join(expandDir, "xerox_driver.pkg")
	if err := os.MkdirAll(subPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subPkg, "PackageInfo"), []byte(
		`<?xml version="1.0" encoding="UTF-8"?><pkg-info identifier="com.xerox.drivers.pkg" version="5.19.3" install-location="/"/>`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	buildTestPayload(t, filepath.Join(subPkg, "Payload"), map[string]string{
		"Library/Printers/PPDs/Contents/Resources/Xerox C300 Color Printer.gz": "*PPD-Adobe: \"4.3\"\n",
		"Library/Printers/Xerox/Filters/pstoxrps.app/Contents/MacOS/pstoxrps":  "#!/bin/sh\necho fake filter\n",
		"Library/LaunchDaemons/com.xerox.AnalyticsAgent.plist":                 "<plist>not driver/print-path-relevant</plist>\n",
		"Library/Frameworks/XeroxAnalytics.framework/XeroxAnalytics":           "not driver/print-path-relevant either\n",
	})

	destDir := t.TempDir()
	if !extractDriverFootprint(expandDir, destDir) {
		t.Fatal("extractDriverFootprint returned false, want true")
	}
	if _, err := os.Stat(filepath.Join(destDir, "Library", "Printers", "PPDs", "Contents", "Resources", "Xerox C300 Color Printer.gz")); err != nil {
		t.Errorf("the real PPD was not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Library", "Printers", "Xerox", "Filters", "pstoxrps.app", "Contents", "MacOS", "pstoxrps")); err != nil {
		t.Errorf("the filter binary was not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Library", "LaunchDaemons")); err == nil {
		t.Error("the unrelated LaunchDaemons entry was extracted, want it excluded (not under the Printers path fragment)")
	}
	if _, err := os.Stat(filepath.Join(destDir, "Library", "Frameworks")); err == nil {
		t.Error("the unrelated Frameworks entry was extracted, want it excluded (not under the Printers path fragment)")
	}
}

// TestExtractDriverFootprint_NothingQualifies guards the "nothing sensible
// to fall back to" case DriverFootprintForVersionGateFallback's own doc
// comment relies on: a sub-package that isn't driver-relevant at all should
// never cause extractDriverFootprint to report success.
func TestExtractDriverFootprint_NothingQualifies(t *testing.T) {
	expandDir := t.TempDir()
	subPkg := filepath.Join(expandDir, "Unrelated.pkg")
	if err := os.MkdirAll(subPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subPkg, "PackageInfo"), []byte(
		`<?xml version="1.0" encoding="UTF-8"?><pkg-info identifier="com.example.unrelated" version="1.0" install-location="/Applications/"/>`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	buildTestPayload(t, filepath.Join(subPkg, "Payload"), map[string]string{
		"SomeApp.app/Contents/MacOS/SomeApp": "irrelevant\n",
	})

	destDir := t.TempDir()
	if extractDriverFootprint(expandDir, destDir) {
		t.Error("extractDriverFootprint returned true, want false - no sub-package here declares a driver-relevant install-location")
	}
}

// TestPackagePPDEntriesFilteredFallback_ExpandRunsWhileFilesStillExist
// guards a real, confirmed-live bug (2026-09-13): an earlier version of
// this function's own caller (indexFamilyPackage) ran its expand hook
// *after* packagePPDEntriesFilteredFallback had already returned - by which
// point this function's own `defer os.RemoveAll(tmpDir)` had already fired,
// so every entry's own Path pointed at an already-deleted file.
// toshibaExpandProductEntries' own os.Open silently failed every time as a
// result, always falling back to the unexpanded generic entry - Toshiba's
// real model count came back as 8 (the generic PDL-variant count) instead
// of the real ~136 e-STUDIO model numbers, discovered only by comparing
// against the real *Product line count found live in each PPD. Uses the
// same real (if tiny) UFRII_test_fixture.pkg already checked in for the
// family tests - skips on non-macOS, same reasoning as
// macresolve_test.go's own pkgutil-dependent tests.
func TestPackagePPDEntriesFilteredFallback_ExpandRunsWhileFilesStillExist(t *testing.T) {
	if _, err := exec.LookPath("pkgutil"); err != nil {
		t.Skip("pkgutil not on PATH (not running on macOS)")
	}
	pkgPath := filepath.Join("testdata_mac_family", "macOS", "Canon", "26-Tahoe", "UFRII_test_fixture.pkg")

	var sawEntry bool
	expand := func(entries []ppdEntry) []ppdEntry {
		for _, e := range entries {
			sawEntry = true
			if _, err := os.Stat(e.Path); err != nil {
				t.Errorf("expand hook ran against a Path that no longer exists on disk (%q): %v - the temp dir was cleaned up too early", e.Path, err)
			}
		}
		return entries
	}

	if _, _, err := packagePPDEntriesFilteredFallback(pkgPath, nil, nil, expand); err != nil {
		t.Fatalf("packagePPDEntriesFilteredFallback: %v", err)
	}
	if !sawEntry {
		t.Fatal("expand hook was never called with any entries - fixture produced nothing to check")
	}
}
