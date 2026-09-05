package windows

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"

	"PDT/internal/driver"
	"PDT/internal/printer"
)

// Deployer implements printer.Deployer against the real Windows print
// spooler, using the low-level winspool.drv/registry bindings verified
// elsewhere in this package. Catalog is built once (BuildCatalog) and shared
// across every row in a run.
type Deployer struct {
	Catalog driver.Catalog
}

func NewDeployer(catalog driver.Catalog) *Deployer {
	return &Deployer{Catalog: catalog}
}

// Deploy ports Create-Printers.ps1's Deploy-PrinterRow, in the same overall
// order (port -> driver -> printer object -> rebind -> print config -> APF),
// adapted for this rewrite's automatic (rather than checkbox-driven) NUL:
// workaround and its Confirm-callback (rather than WinForms MessageBox)
// confirmation flows.
//
// Per spec: failing to create/update the printer object itself is fatal for
// this row (returned as DeployResult.Err, logged [ERR]) - every step up to
// and including that, plus the NUL:-to-real-port rebind, can end the row
// early. Failing to apply a *setting* on an object that does exist (duplex/
// color, APF) is only ever a [WARN] - Deploy always attempts both and always
// finishes the row successfully once the object itself is confirmed to exist.
func (d *Deployer) Deploy(ctx context.Context, req printer.DeployRequest, confirm printer.Confirm) printer.DeployResult {
	log := &printer.Logger{}
	row := req.Row
	fatal := func(err error) printer.DeployResult {
		log.Err("%v", err)
		return printer.DeployResult{RowName: row.Name, Log: log.Lines(), Err: err}
	}

	log.Info("Starting deployment for %q", row.Name)

	ip, isNul, err := printer.NormalizeIP(row.IP)
	if err != nil {
		return fatal(err)
	}

	resolved, err := driver.Resolve(d.Catalog, row.Manufacturer, row.Driver)
	if err != nil {
		return fatal(fmt.Errorf("resolving driver: %w", err))
	}
	if resolved == nil {
		return fatal(fmt.Errorf("no usable driver found locally for manufacturer %q, selection %q", row.Manufacturer, row.Driver))
	}
	log.Info("Resolved driver %q (v%s, %s)", resolved.Name, resolved.Version, resolved.Date.Format("2006-01-02"))

	// --- Port ---
	var realPortName string // stays "" for a permanent NUL: row (isNul); that's the only case with no real port at all.
	if !isNul {
		realPortName, err = d.ensureRealPort(ip, row.UseExistingPort, req.PortNamePrefix, row.SNMP, log)
		if err != nil {
			return fatal(fmt.Errorf("resolving port for %s: %w", ip, err))
		}
	}

	finalTargetPort := printer.NulPortName
	if realPortName != "" {
		finalTargetPort = realPortName
	}

	useNulFirst := isNul || printer.RequiresNulPortWorkaround(row.Manufacturer, resolved.Name)
	createPortName := finalTargetPort
	if useNulFirst {
		createPortName = printer.NulPortName
		if err := EnsureNulPort(); err != nil {
			return fatal(fmt.Errorf("creating NUL: port: %w", err))
		}
	}

	// --- Driver ---
	if err := d.ensureDriverCurrent(ctx, resolved, confirm, log); err != nil {
		return fatal(err)
	}

	// --- Printer object ---
	comment := ""
	if req.SalesChainID != "" {
		comment = "SalesChain: " + req.SalesChainID
	}

	// Tracks whether THIS run actually (re)bound the printer to NUL: - the
	// rebind step below must only fire when that's true, not just because
	// useNulFirst is true, or declining the update-confirmation below for an
	// existing printer would still get silently overridden by an
	// unconditional rebind afterward.
	printerBoundToNul := false

	existingNames, err := EnumLocalPrinterNames()
	if err != nil {
		return fatal(fmt.Errorf("listing existing printers: %w", err))
	}
	exists := false
	for _, n := range existingNames {
		if strings.EqualFold(n, row.Name) {
			exists = true
			break
		}
	}

	if exists {
		p, err := OpenPrinter(row.Name, PrinterAllAccess)
		if err != nil {
			return fatal(fmt.Errorf("opening existing printer %q: %w", row.Name, err))
		}
		info, err := p.GetInfo2()
		if err != nil {
			p.Close()
			return fatal(fmt.Errorf("reading existing printer %q: %w", row.Name, err))
		}

		currentDriver := utf16PtrToStringSafe(info.Info.DriverName)
		currentPort := utf16PtrToStringSafe(info.Info.PortName)
		currentComment := utf16PtrToStringSafe(info.Info.Comment)

		var changes []string
		if currentDriver != resolved.Name {
			changes = append(changes, fmt.Sprintf("Driver: %q -> %q", currentDriver, resolved.Name))
		}
		// Compare against the FINAL target port, not createPortName as it
		// stands right now - in NUL-first mode createPortName is only
		// 'NUL:' temporarily until the rebind step below, so comparing
		// against that directly would falsely show a port change for a row
		// whose real target port isn't actually changing at all.
		if currentPort != finalTargetPort {
			changes = append(changes, fmt.Sprintf("Port: %q -> %q", currentPort, finalTargetPort))
		}
		if currentComment != comment {
			changes = append(changes, fmt.Sprintf("Comment: %q -> %q", currentComment, comment))
		}

		if len(changes) == 0 {
			log.Info("Printer %q already exists; no driver/port/comment changes needed.", row.Name)
			p.Close()
		} else {
			ok, cerr := confirm(ctx, "Confirm printer update",
				fmt.Sprintf("Printer %q already exists. The following would change:\n\n%s\n\nApply these changes?", row.Name, strings.Join(changes, "\n")))
			if cerr != nil {
				p.Close()
				return fatal(fmt.Errorf("confirming update to printer %q: %w", row.Name, cerr))
			}
			if !ok {
				log.Warn("Printer %q already exists; user declined to apply changes - %s. Leaving driver/port/comment as-is.", row.Name, strings.Join(changes, "; "))
				p.Close()
			} else {
				driverPtr, derr := windows.UTF16PtrFromString(resolved.Name)
				portPtr, perr := windows.UTF16PtrFromString(createPortName)
				commentPtr, cmerr := windows.UTF16PtrFromString(comment)
				if derr != nil || perr != nil || cmerr != nil {
					p.Close()
					return fatal(fmt.Errorf("encoding updated printer fields for %q", row.Name))
				}
				info.Info.DriverName = driverPtr
				info.Info.PortName = portPtr
				info.Info.Comment = commentPtr
				if err := p.SetInfo2(info); err != nil {
					p.Close()
					return fatal(fmt.Errorf("applying update to printer %q: %w", row.Name, err))
				}
				log.OK("Printer %q updated - %s.", row.Name, strings.Join(changes, "; "))
				if useNulFirst {
					printerBoundToNul = true
				}
				p.Close()
			}
		}
	} else {
		p, err := CreatePrinter(row.Name, resolved.Name, createPortName, comment)
		if err != nil {
			return fatal(fmt.Errorf("creating printer %q: %w", row.Name, err))
		}
		p.Close()
		log.OK("Created printer %q (driver %q, port %q).", row.Name, resolved.Name, createPortName)
		if useNulFirst {
			printerBoundToNul = true
		}
	}

	// --- Rebind NUL: -> real port ---
	// Part of getting the printer object onto the port it's actually
	// supposed to be on, not a "setting" - treated as fatal like the rest of
	// the object-creation sequence above, matching the original tool.
	if printerBoundToNul && realPortName != "" {
		p, err := OpenPrinter(row.Name, PrinterAllAccess)
		if err != nil {
			return fatal(fmt.Errorf("reopening %q to rebind from NUL:: %w", row.Name, err))
		}
		info, err := p.GetInfo2()
		if err != nil {
			p.Close()
			return fatal(fmt.Errorf("reading %q to rebind from NUL:: %w", row.Name, err))
		}
		portPtr, err := windows.UTF16PtrFromString(realPortName)
		if err != nil {
			p.Close()
			return fatal(fmt.Errorf("encoding port name %q: %w", realPortName, err))
		}
		info.Info.PortName = portPtr
		if err := p.SetInfo2(info); err != nil {
			p.Close()
			return fatal(fmt.Errorf("rebinding %q from NUL: to %q: %w", row.Name, realPortName, err))
		}
		p.Close()
		log.OK("Rebound %q from NUL: to %q.", row.Name, realPortName)
	}

	// --- Print configuration (duplex/color) - warnings only past this point ---
	if err := SetDuplexAndColor(row.Name, row.OneSided, row.Mono); err != nil {
		log.Warn("Could not set duplex/color: %v", err)
	} else if duplex, color, verr := GetActualDuplexColor(row.Name); verr != nil {
		log.Warn("Set duplex/color, but could not verify the driver actually applied it: %v", verr)
	} else {
		wantDuplex := DmDupVertical
		if row.OneSided {
			wantDuplex = DmDupSimplex
		}
		wantColor := DmColorColor
		if row.Mono {
			wantColor = DmColorMonochrome
		}
		switch {
		case duplex != int16(wantDuplex) && color != int16(wantColor):
			log.Warn("Requested duplex/color did not take effect (driver reports duplex=%d, color=%d) - some drivers (observed: Kyocera) are known not to honor every combination.", duplex, color)
		case duplex != int16(wantDuplex):
			log.Warn("Requested duplex mode did not take effect (driver reports duplex=%d) - some drivers are known not to honor every combination.", duplex)
		case color != int16(wantColor):
			log.Warn("Requested color mode did not take effect (driver reports color=%d) - some drivers (observed: Kyocera, switching back to color) are known not to honor every combination.", color)
		default:
			log.OK("Duplex/color confirmed applied (oneSided=%v, mono=%v).", row.OneSided, row.Mono)
		}
	}

	// --- Advanced printing features ---
	if err := SetAdvancedPrintingFeatures(row.Name, row.AdvancedPrintingFeatures); err != nil {
		log.Warn("Could not set advanced printing features: %v", err)
	} else {
		log.OK("Advanced printing features set to %v.", row.AdvancedPrintingFeatures)
	}

	log.OK("Deployment finished for %q.", row.Name)
	return printer.DeployResult{RowName: row.Name, Log: log.Lines(), Err: nil}
}

// ensureRealPort resolves (creating if necessary) the real Standard TCP/IP
// port for ip, honoring UseExistingPort's documented contract: reuse the
// port already configured for this IP if one exists (regardless of the
// checkbox - this also doubles as the fix for ever creating a genuine
// duplicate port for a host that already has one), and fail this row if
// UseExistingPort is checked but no such port exists. Otherwise create a new
// one, named "<prefix><ip>" (or just "<ip>" with no prefix), with a numeric
// suffix appended only if that exact name is already in use by a different
// host.
func (d *Deployer) ensureRealPort(ip string, useExisting bool, prefix string, snmpEnabled bool, log *printer.Logger) (string, error) {
	if existingName, found, err := FindTcpIpPortByHost(ip); err != nil {
		return "", err
	} else if found {
		log.Info("Reusing existing Standard TCP/IP port %q already configured for %s.", existingName, ip)
		return existingName, nil
	} else if useExisting {
		return "", fmt.Errorf("UseExistingPort is checked but no existing Standard TCP/IP port targets %s", ip)
	}

	base := ip
	if prefix != "" {
		base = prefix + ip
	}
	existingNames, err := EnumLocalPortNames()
	if err != nil {
		return "", err
	}
	taken := make(map[string]bool, len(existingNames))
	for _, n := range existingNames {
		taken[strings.ToLower(n)] = true
	}
	name := base
	for i := 2; taken[strings.ToLower(name)]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}

	if err := AddStandardTcpIpPort(name, ip, 9100, snmpEnabled, ""); err != nil {
		return "", err
	}
	log.OK("Created Standard TCP/IP port %q -> %s:9100.", name, ip)
	return name, nil
}

// ensureDriverCurrent ports Deploy-PrinterRow's driver-upgrade section: a
// plain-name selection silently upgrades an older installed driver and never
// downgrades; an explicit version pin (ResolvedDriver.IsExplicitVersion) is
// honored upgrade-or-downgrade but only after confirmation, since Windows
// shares one driver registration per name across every printer already using
// it. Declining an explicit pin is a [WARN], not fatal - deployment proceeds
// using whichever version is already installed.
func (d *Deployer) ensureDriverCurrent(ctx context.Context, resolved *driver.ResolvedDriver, confirm printer.Confirm, log *printer.Logger) error {
	installedDate, installedVersion, found, err := GetInstalledDriverVersion(resolved.Name)
	if err != nil {
		return fmt.Errorf("checking installed version of driver %q: %w", resolved.Name, err)
	}

	needsInstall := true
	if found {
		// Compare numerically (driver.CompareVersions), not by exact string
		// equality - the same real version can come back differently
		// formatted from the two sources being compared here (e.g. the
		// registry's "61.360.1.26819" vs. an INF's "61.360.01.26819" for the
		// same actual HP driver), and a naive string compare would wrongly
		// treat those as different versions.
		isSameVersion := installedDate.Equal(resolved.Date) && driver.CompareVersions(resolved.Version, installedVersion) == 0
		isNewer := resolved.Date.After(installedDate) ||
			(resolved.Date.Equal(installedDate) && driver.CompareVersions(resolved.Version, installedVersion) > 0)

		switch {
		case isSameVersion:
			needsInstall = false
			log.Info("Installed driver %q (v%s, %s) already matches the one selected for this row.", resolved.Name, installedVersion, installedDate.Format("2006-01-02"))
		case resolved.IsExplicitVersion:
			direction := "older"
			if isNewer {
				direction = "newer"
			}
			ok, cerr := confirm(ctx, "Confirm driver version change", fmt.Sprintf(
				"The driver %q is currently installed as v%s (%s).\n\nThis row selects a %s version: v%s (%s).\n\nWindows shares one driver registration per name, so changing it affects every printer already using %q, not just this one.\n\nInstall the selected version?",
				resolved.Name, installedVersion, installedDate.Format("2006-01-02"), direction, resolved.Version, resolved.Date.Format("2006-01-02"), resolved.Name))
			if cerr != nil {
				return fmt.Errorf("confirming driver version change for %q: %w", resolved.Name, cerr)
			}
			if ok {
				log.Info("Confirmed: updating driver %q from v%s to v%s as explicitly selected for this row.", resolved.Name, installedVersion, resolved.Version)
			} else {
				needsInstall = false
				log.Warn("Declined: leaving driver %q at v%s - this row's explicit selection of v%s will not take effect until it's installed.", resolved.Name, installedVersion, resolved.Version)
			}
		default:
			needsInstall = isNewer
			if needsInstall {
				log.Info("Installed driver %q (v%s, %s) is older than the one selected (v%s, %s); updating it.", resolved.Name, installedVersion, installedDate.Format("2006-01-02"), resolved.Version, resolved.Date.Format("2006-01-02"))
			} else {
				log.Info("Installed driver %q (v%s, %s) is already current; leaving it as-is.", resolved.Name, installedVersion, installedDate.Format("2006-01-02"))
			}
		}
	}

	if !needsInstall {
		return nil
	}
	if err := EnsureDriverInstalled(resolved.InfPath, resolved.Name); err != nil {
		return fmt.Errorf("installing driver %q: %w", resolved.Name, err)
	}
	log.OK("Installed driver %q (v%s, %s).", resolved.Name, resolved.Version, resolved.Date.Format("2006-01-02"))
	return nil
}

func utf16PtrToStringSafe(p *uint16) string {
	if p == nil {
		return ""
	}
	return windows.UTF16PtrToString(p)
}
