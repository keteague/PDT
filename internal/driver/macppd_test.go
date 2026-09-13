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
