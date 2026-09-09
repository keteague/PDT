// Package flashdrive enumerates and formats removable (USB flash) drives -
// the write-target side of "install PDT on the technician's laptop, then
// stamp out portable copies onto flash drives" (see the main package's
// WritePortablePDT/FormatDrives, which layer the actual file-copy and
// per-drive result reporting on top of this). This file is macOS's own
// implementation of the exact same API flashdrive_windows.go provides,
// built on diskutil/syscall.Statfs instead of the Win32 GetDriveType/
// GetLogicalDrives/GetDiskFreeSpaceEx family.
package flashdrive

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// diskutilInfo is the subset of `diskutil info -plist <path>`'s own output
// this package actually reads, decoded via `plutil -convert json` (there is
// no plist package in Go's standard library, and diskutil's own plist output
// is otherwise painful to parse reliably) - confirmed live against this
// machine's own internal disk: RemovableMediaOrExternalDevice is literally
// named for exactly the "should this be offered as a flash-drive target"
// question this package needs answered, and reports false for the internal
// disk, true for a genuine external/USB volume.
type diskutilInfo struct {
	RemovableMediaOrExternalDevice bool
	VolumeName                     string
	MountPoint                     string
}

func getDiskutilInfo(path string) (diskutilInfo, bool) {
	out, err := exec.Command("diskutil", "info", "-plist", path).Output()
	if err != nil {
		return diskutilInfo{}, false
	}
	jsonOut, err := runPlutilToJSON(out)
	if err != nil {
		return diskutilInfo{}, false
	}
	var info diskutilInfo
	if err := json.Unmarshal(jsonOut, &info); err != nil {
		return diskutilInfo{}, false
	}
	return info, true
}

// runPlutilToJSON pipes plistData through `plutil -convert json -o - -`
// (stdin, print to stdout) - the standard macOS tool for exactly this
// conversion, avoiding a hand-rolled plist parser for a structure with far
// more fields than internal/driver/macmount.go's own single-field regex
// extraction would be safe to generalize to.
func runPlutilToJSON(plistData []byte) ([]byte, error) {
	cmd := exec.Command("plutil", "-convert", "json", "-o", "-", "-")
	cmd.Stdin = strings.NewReader(string(plistData))
	return cmd.Output()
}

// IsRemovableDrive reports whether path sits on a removable/external drive -
// PDT's own running location, when Write to Flash Drive needs to refuse to
// overwrite the exact file it's currently executing from. Unlike Windows
// (where this is a hard OS refusal), macOS actually allows overwriting a
// running executable's file - but stamping a running portable copy out onto
// more drives still makes no sense as a workflow, so the same refusal
// applies here for the same reason, not because the OS forces it.
func IsRemovableDrive(path string) bool {
	info, ok := getDiskutilInfo(path)
	return ok && info.RemovableMediaOrExternalDevice
}

// Drive is one currently-mounted removable drive. Letter holds the mount
// point path (e.g. "/Volumes/MYDRIVE") on macOS - there are no drive
// letters - kept as the same field name as the Windows Drive struct since
// every caller (app.go's DriveInfo, the frontend) already treats it as an
// opaque per-drive identifier string, never parses it as an actual letter.
type Drive struct {
	Letter     string
	Label      string
	TotalBytes uint64
	FreeBytes  uint64
}

// EnumRemovableDrives lists every currently-mounted removable/external
// volume under /Volumes - diskutil's own RemovableMediaOrExternalDevice
// flag per volume (see diskutilInfo's own doc comment), which is what a
// real USB flash drive reports true for (as opposed to the internal disk,
// or a network share, which isn't listed under /Volumes as a distinct mount
// on macOS in the first place). A volume with an unreadable diskutil info
// entry is skipped rather than erroring the whole scan.
func EnumRemovableDrives() ([]Drive, error) {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return nil, err
	}

	// []Drive{}, not nil - a nil slice marshals to JSON `null` across the
	// Wails/JS bridge (see driver.ManufacturersWithDrivers' own comment for
	// the exact class of bug this avoids).
	drives := []Drive{}
	for _, e := range entries {
		mountPoint := filepath.Join("/Volumes", e.Name())
		info, ok := getDiskutilInfo(mountPoint)
		if !ok || !info.RemovableMediaOrExternalDevice {
			continue
		}

		d := Drive{Letter: mountPoint, Label: info.VolumeName}
		var st syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &st); err == nil {
			d.TotalBytes = uint64(st.Bsize) * st.Blocks
			d.FreeBytes = uint64(st.Bsize) * st.Bavail
		}
		drives = append(drives, d)
	}
	return drives, nil
}

// FormatExFAT quick-formats mountPoint (e.g. "/Volumes/MYDRIVE") as exFAT,
// labeled "PDT". `diskutil eraseVolume` reformats the one volume in place
// (not `eraseDisk`, which would also repartition the whole physical device -
// more destructive than this needs, and not the Windows FormatExFAT's own
// scope of "reformat this one drive letter" either).
func FormatExFAT(mountPoint string) error {
	cmd := exec.Command("diskutil", "eraseVolume", "ExFAT", "PDT", mountPoint)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("formatting %s: %w: %s", mountPoint, err, strings.TrimSpace(string(out)))
	}
	return nil
}
