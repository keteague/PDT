package windows

import (
	"errors"
	"runtime"
	"time"

	"golang.org/x/sys/windows/registry"
)

// environmentKeyName maps this process's architecture to the print
// subsystem's own environment name, confirmed against a real machine:
// HKLM\SYSTEM\CurrentControlSet\Control\Print\Environments lists exactly
// "Windows 4.0", "Windows ARM64", "Windows IA64", "Windows NT x86" and
// "Windows x64" - IA64 has no Go GOARCH equivalent and is omitted.
func environmentKeyName() string {
	switch runtime.GOARCH {
	case "arm64":
		return "Windows ARM64"
	case "386":
		return "Windows NT x86"
	default:
		return "Windows x64"
	}
}

// GetInstalledDriverVersion reads an already-installed driver's real
// date/version straight from the registry key the print spooler itself
// maintains per driver name:
// HKLM\SYSTEM\CurrentControlSet\Control\Print\Environments\<env>\Drivers\Version-3\<driverName>,
// values "DriverDate" (MM/DD/YYYY) and "DriverVersion" (dot-separated, e.g.
// "8.6.1022.0"). Confirmed against a real machine to hold the same version
// string a vendor INF's own DriverVer= line declares - unlike Get-PrinterDriver's
// CIM-derived Date/DriverVersion properties, which the original PowerShell
// tool found to come back unusable (garbage DriverVersion, blank Date) no
// matter how the driver was installed. found is false if driverName isn't
// currently installed (no error in that case, matching
// Get-PrinterDriver -ErrorAction SilentlyContinue's $null-means-not-found
// convention).
func GetInstalledDriverVersion(driverName string) (date time.Time, version string, found bool, err error) {
	path := `SYSTEM\CurrentControlSet\Control\Print\Environments\` + environmentKeyName() + `\Drivers\Version-3\` + driverName
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return time.Time{}, "", false, nil
		}
		return time.Time{}, "", false, err
	}
	defer k.Close()

	versionStr, _, err := k.GetStringValue("DriverVersion")
	if err != nil {
		return time.Time{}, "", false, err
	}
	dateStr, _, _ := k.GetStringValue("DriverDate")
	parsedDate, _ := time.Parse("01/02/2006", dateStr)
	return parsedDate, versionStr, true, nil
}
