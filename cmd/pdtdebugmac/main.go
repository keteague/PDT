//go:build darwin

// pdtdebugmac is a throwaway CLI for verifying the macOS driver-catalog and
// darwin Deployer bindings (internal/driver's Mac* additions,
// internal/printer/darwin) against real files/state on this machine -
// mirroring how cmd/pdtdebug does the same for the Windows side. It has no
// role in the shipped PDT app; delete it once the Wails UI can exercise the
// same codepaths directly.
//
// installpkg and deployqueue run real privileged commands (installer,
// lpadmin) via osascript's administrator-privileges prompt - the same
// elevation every real Deploy call uses, see internal/printer/darwin's own
// elevate_darwin.go.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"PDT/internal/driver"
	"PDT/internal/printer"
	pdtdarwin "PDT/internal/printer/darwin"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "catalog":
		if len(os.Args) != 3 {
			usage()
			os.Exit(1)
		}
		err = cmdCatalog(os.Args[2])
	case "models":
		if len(os.Args) != 3 {
			usage()
			os.Exit(1)
		}
		err = cmdModels(os.Args[2])
	case "installpkg":
		if len(os.Args) != 3 {
			usage()
			os.Exit(1)
		}
		err = cmdInstallPkg(os.Args[2])
	case "deployqueue":
		if len(os.Args) != 5 {
			usage()
			os.Exit(1)
		}
		err = cmdDeployQueue(os.Args[2], os.Args[3], os.Args[4])
	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`pdtdebugmac <command>

Commands:
  catalog <driversRoot>              scan Drivers/macOS/... and print what
                                      BuildMacCatalog/ResolveMac/OpenPrinting
                                      fallback find for every manufacturer
  models <driversRoot>               build the real BuildMacModelIndex and
                                      print every manufacturer/model/variant
                                      it finds - the Model/Driver dropdown's
                                      actual data source on macOS
  installpkg <path-to-.dmg-or-.pkg>  run EnsureDriverInstalled standalone and
                                      print the resulting PPD diff (real
                                      install - prompts for admin password)
  deployqueue <driversRoot> <manufacturer> <ip>
                                      run the full darwin Deployer.Deploy
                                      sequence end to end against one
                                      throwaway LPD queue ("PDT Debug Test
                                      Queue"), auto-confirming every prompt,
                                      printing every log line (real install +
                                      real lpadmin - prompts for admin
                                      password; does not delete the queue
                                      afterward, unlike pdtdebug's own
                                      deployrow - remove it by hand via
                                      lpadmin -x once you're done inspecting
                                      it)`)
}

func cmdCatalog(driversRoot string) error {
	cat, err := driver.BuildMacCatalog(driversRoot)
	if err != nil {
		return fmt.Errorf("BuildMacCatalog: %w", err)
	}

	for _, mfg := range driver.Manufacturers {
		pkgs := cat.Packages[mfg]
		ppds := cat.OpenPrintingPPDs[mfg]
		if len(pkgs) == 0 && len(ppds) == 0 {
			continue
		}
		fmt.Printf("%s:\n", mfg)
		for _, p := range pkgs {
			fmt.Printf("  package: %s (kind=%v, modtime=%s)\n", p.Path, p.Kind, p.ModTime.Format("2006-01-02 15:04:05"))
		}
		if resolved := driver.ResolveMac(cat, mfg); resolved != nil {
			fmt.Printf("  -> resolved: %s (label=%q)\n", resolved.Path, resolved.Label)
		}
		for _, p := range ppds {
			fmt.Printf("  OpenPrinting PPD: %s\n", p)
		}
	}
	return nil
}

func cmdModels(driversRoot string) error {
	cat, err := driver.BuildMacCatalog(driversRoot)
	if err != nil {
		return fmt.Errorf("BuildMacCatalog: %w", err)
	}
	ppdCacheDir := filepath.Join(os.TempDir(), "pdtdebugmac-ppdcache")
	macRoot := filepath.Join(driversRoot, "macOS")
	start := time.Now()
	index, changes := driver.BuildMacModelIndex(cat, macRoot, ppdCacheDir, true)
	fmt.Printf("BuildMacModelIndex took %s (each manufacturer's own catalog.<mfg>.json under %s - run again to see the cached/skip-reinspection path)\n\n", time.Since(start), macRoot)

	if len(index) == 0 {
		fmt.Println("BuildMacModelIndex returned nothing at all for any manufacturer.")
	}
	for mfg, byModel := range index {
		fmt.Printf("%s: %d model(s)\n", mfg, len(byModel))
		for model, variants := range byModel {
			fmt.Printf("  %q:\n", model)
			for _, v := range variants {
				switch {
				case v.PackagePath != "":
					fmt.Printf("    [%s] label=%q nickname=%q filename=%q package=%s\n", v.Language, v.Label, v.NickName, v.Filename, v.PackagePath)
				default:
					fmt.Printf("    [%s] label=%q nickname=%q filename=%q cached=%s\n", v.Language, v.Label, v.NickName, v.Filename, v.LooseCachedPPDPath)
				}
			}
		}
	}

	fmt.Println("\n--- what App.Models/App.DriverCandidates would actually return for Canon (blank filter) ---")
	fmt.Printf("MacModels: %v\n", driver.MacModels(index, "Canon", ""))
	fmt.Printf("MacModelCandidates: %v\n", driver.MacModelCandidates(index, "Canon", "", ""))

	fmt.Println("\n--- model changes vs. the previous catalog.json, if any ---")
	if len(changes) == 0 {
		fmt.Println("(none - first-ever build, or nothing changed since the last one)")
	}
	for _, c := range changes {
		fmt.Println(c)
	}
	return nil
}

func cmdInstallPkg(path string) error {
	resolved := &driver.ResolvedMacPackage{
		Path:  path,
		Label: driver.PackageLabel(path),
	}
	fmt.Printf("installing %s (label=%q) - this needs your admin password...\n", resolved.Path, resolved.Label)

	newPPDs, err := pdtdarwin.EnsureDriverInstalled(context.Background(), resolved)
	if err != nil {
		return err
	}
	fmt.Printf("OK: install added %d new PPD(s):\n", len(newPPDs))
	for _, p := range newPPDs {
		fmt.Printf("  %s\n", p)
	}
	return nil
}

func cmdDeployQueue(driversRoot, manufacturer, ip string) error {
	cat, err := driver.BuildMacCatalog(driversRoot)
	if err != nil {
		return fmt.Errorf("BuildMacCatalog: %w", err)
	}
	modelIndex, _ := driver.BuildMacModelIndex(cat, filepath.Join(driversRoot, "macOS"), filepath.Join(os.TempDir(), "pdtdebugmac-ppdcache"), true)

	deployer := pdtdarwin.NewDeployer(cat, modelIndex)
	req := printer.DeployRequest{
		Row: printer.PrinterRow{
			Name:         "PDT Debug Test Queue",
			IP:           ip,
			Manufacturer: manufacturer,
			Mono:         true,
			OneSided:     true,
		},
		SalesChainID: "PDT-DEBUG",
	}

	confirm := func(ctx context.Context, title, message string) (bool, error) {
		fmt.Printf("--- CONFIRM (auto-yes): %s ---\n%s\n", title, message)
		return true, nil
	}

	result := deployer.Deploy(context.Background(), req, confirm)
	for _, line := range result.Log {
		fmt.Println(line)
	}
	if result.Err != nil {
		return result.Err
	}
	fmt.Println("OK: deploy finished")
	return nil
}
