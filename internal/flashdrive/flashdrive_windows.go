// Package flashdrive enumerates and formats removable (USB flash) drives -
// the write-target side of "install PDT on the technician's laptop, then
// stamp out portable copies onto flash drives" (see the main package's
// WritePortablePDT/FormatDrives, which layer the actual file-copy and
// per-drive result reporting on top of this).
package flashdrive

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
)

// Drive is one currently-mounted removable drive.
type Drive struct {
	Letter     string // e.g. "E:" - no trailing backslash
	Label      string
	TotalBytes uint64
	FreeBytes  uint64
}

// EnumRemovableDrives lists every currently-mounted removable drive -
// GetDriveType's DRIVE_REMOVABLE, which is what a real USB flash drive
// reports (as opposed to DRIVE_FIXED for internal/external hard disks and
// most USB SSDs, DRIVE_REMOTE for network shares, or DRIVE_CDROM). A drive
// with an unreadable volume label (no media, or one mid-format) is still
// listed - Label just comes back empty.
func EnumRemovableDrives() ([]Drive, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, err
	}

	// []Drive{}, not nil - a nil slice marshals to JSON `null` across the
	// Wails/JS bridge (see driver.ManufacturersWithDrivers' own comment for
	// the exact class of bug this avoids); the frontend already guards this
	// specific call with `result.drives || []`, but there's no reason to
	// rely on every future caller remembering to.
	drives := []Drive{}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + ":"
		rootPtr, err := windows.UTF16PtrFromString(letter + `\`)
		if err != nil {
			continue
		}
		if windows.GetDriveType(rootPtr) != windows.DRIVE_REMOVABLE {
			continue
		}

		d := Drive{Letter: letter}
		volName := make([]uint16, 256)
		if err := windows.GetVolumeInformation(rootPtr, &volName[0], uint32(len(volName)), nil, nil, nil, nil, 0); err == nil {
			d.Label = windows.UTF16ToString(volName)
		}
		var free, total, totalFree uint64
		if err := windows.GetDiskFreeSpaceEx(rootPtr, &free, &total, &totalFree); err == nil {
			d.TotalBytes = total
			d.FreeBytes = free
		}
		drives = append(drives, d)
	}
	return drives, nil
}

// FormatExFAT quick-formats letter (e.g. "E:") as exFAT, labeled "PDT".
// Shells out to PowerShell's Format-Volume (Storage module, built into
// every supported Windows version) rather than legacy format.com - the same
// "exec.Command(\"powershell.exe\", ...)" pattern already used elsewhere in
// this codebase for scripted operations with no simple direct Win32 call,
// and avoids format.com's own interactive console-prompt quirks entirely.
// Quick (non-Full) format, since this is about laying down a fresh
// filesystem before writing a portable PDT copy, not verifying the drive's
// physical media.
func FormatExFAT(letter string) error {
	driveLetter := strings.TrimSuffix(letter, ":")
	script := fmt.Sprintf(
		`Format-Volume -DriveLetter %s -FileSystem exFAT -NewFileSystemLabel 'PDT' -Confirm:$false -Force`,
		driveLetter,
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("formatting %s: %w: %s", letter, err, strings.TrimSpace(string(out)))
	}
	return nil
}
