package windows

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	dmOutBuffer = 0x0002
	dmInBuffer  = 0x0008
)

func getDevModeSize(h windows.Handle, deviceName string) (int32, error) {
	namePtr, err := windows.UTF16PtrFromString(deviceName)
	if err != nil {
		return 0, err
	}
	r1, _, callErr := procDocumentPropertiesW.Call(0, uintptr(h), uintptr(unsafe.Pointer(namePtr)), 0, 0, 0)
	runtime.KeepAlive(namePtr)
	size := int32(r1)
	if size <= 0 {
		return 0, fmt.Errorf("DocumentProperties(size query, %q): %w", deviceName, callErr)
	}
	return size, nil
}

// GetDevMode retrieves the printer's current DEVMODE straight from the
// driver via DocumentProperties - the direct equivalent of the original
// tool's Get-ActualDuplexMode (System.Drawing.Printing.PrinterSettings-based),
// needed because Get-PrintConfiguration was found to just echo requests
// rather than report real state.
func GetDevMode(h windows.Handle, deviceName string) ([]byte, error) {
	size, err := getDevModeSize(h, deviceName)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	namePtr, err := windows.UTF16PtrFromString(deviceName)
	if err != nil {
		return nil, err
	}
	r1, _, callErr := procDocumentPropertiesW.Call(0, uintptr(h), uintptr(unsafe.Pointer(namePtr)), uintptr(unsafe.Pointer(&buf[0])), 0, dmOutBuffer)
	runtime.KeepAlive(namePtr)
	if int32(r1) < 0 {
		return nil, fmt.Errorf("DocumentProperties(get, %q): %w", deviceName, callErr)
	}
	return buf, nil
}

// ValidateDevMode round-trips a modified DEVMODE through the driver's own
// DocumentProperties handler before it's committed - going straight from a
// hand-edited buffer to SetPrinter risks the driver rejecting or silently
// ignoring fields it expects to validate/normalize itself (including any
// driver-private extra data past the base 220-byte structure).
func ValidateDevMode(h windows.Handle, deviceName string, input []byte) ([]byte, error) {
	size, err := getDevModeSize(h, deviceName)
	if err != nil {
		return nil, err
	}
	out := make([]byte, size)
	namePtr, err := windows.UTF16PtrFromString(deviceName)
	if err != nil {
		return nil, err
	}
	r1, _, callErr := procDocumentPropertiesW.Call(
		0, uintptr(h), uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&out[0])), uintptr(unsafe.Pointer(&input[0])),
		dmInBuffer|dmOutBuffer,
	)
	runtime.KeepAlive(namePtr)
	runtime.KeepAlive(input)
	if int32(r1) < 0 {
		return nil, fmt.Errorf("DocumentProperties(set, %q): %w", deviceName, callErr)
	}
	return out, nil
}

func devModeView(buf []byte) *DevMode {
	return (*DevMode)(unsafe.Pointer(&buf[0]))
}

// setDevModeEverywhere commits validated as both the printer's global
// default (SetGlobalDevMode, Level 8 - what "Printing Defaults" on the
// Advanced tab shows, and what any user without their own override
// inherits) and the calling account's own per-user default (SetPerUserDevMode,
// Level 2 - what "Preferences" on the General tab shows for that same
// account). These are two independent stores on Windows (see
// OpenedPrinter.SetPerUserDevMode's own comment) - writing only one leaves
// the other showing stale/factory settings, which is exactly what was
// observed testing against only the global write. The global write is
// authoritative for success/failure, since it's what actually matters for
// whoever eventually prints from this machine; the per-user write is
// best-effort so it never fails deploy over what's ultimately just a
// convenience for whichever account happens to be looking at Preferences
// right after.
func setDevModeEverywhere(p *OpenedPrinter, validated []byte) error {
	if err := p.SetGlobalDevMode(validated); err != nil {
		return err
	}
	_ = p.SetPerUserDevMode(validated)
	return nil
}

// SetDuplexAndColor ports the duplex/color half of Deploy-PrinterRow's print
// configuration step, going straight to DEVMODE instead of the original
// tool's two-tier Set-PrintConfiguration + PrintTicket-XML-fallback dance -
// that fallback only ever existed because the PowerShell cmdlet layer was
// unreliable, and has no equivalent here to be unreliable in the first
// place. oneSided/mono follow the grid's own field naming (oneSided means
// simplex; mono means monochrome).
func SetDuplexAndColor(name string, oneSided, mono bool) error {
	p, err := OpenPrinter(name, PrinterAllAccess)
	if err != nil {
		return err
	}
	defer p.Close()

	current, err := GetDevMode(p.Handle, name)
	if err != nil {
		return err
	}

	dm := devModeView(current)
	dm.Fields |= DmDuplex | DmColor
	if oneSided {
		dm.Duplex = DmDupSimplex
	} else {
		dm.Duplex = DmDupVertical
	}
	if mono {
		dm.Color = DmColorMonochrome
	} else {
		dm.Color = DmColorColor
	}

	validated, err := ValidateDevMode(p.Handle, name, current)
	if err != nil {
		return err
	}

	return setDevModeEverywhere(p, validated)
}

// ApplyCapturedDevMode sets name's DEVMODE to a previously-captured raw
// DEVMODE (from GetDevMode against some other, manually-configured printer -
// see App.CaptureDevModeForPrinter) - the deploy-time counterpart to
// SetDuplexAndColor above, mirroring its exact shape, but replaying a whole
// human-configured DEVMODE instead of only setting the duplex/color fields
// programmatically. Always round-trips data through this printer's own
// driver via ValidateDevMode first - a captured DEVMODE was validated by a
// (possibly different) machine's copy of the same driver, and drivers are
// free to normalize/reject fields on their own.
func ApplyCapturedDevMode(name string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("ApplyCapturedDevMode(%q): empty DEVMODE data", name)
	}
	p, err := OpenPrinter(name, PrinterAllAccess)
	if err != nil {
		return err
	}
	defer p.Close()

	validated, err := ValidateDevMode(p.Handle, name, data)
	if err != nil {
		return err
	}

	return setDevModeEverywhere(p, validated)
}

// GetActualDuplexColor re-queries the driver's current DEVMODE fresh (not
// whatever SetDuplexAndColor last requested) so callers can verify the
// driver actually honored the request rather than silently ignoring it -
// some drivers (observed in the original tool: Kyocera for duplex, Canon/
// Kyocera for color) have been seen to do exactly that.
func GetActualDuplexColor(name string) (duplex, color int16, err error) {
	p, err := OpenPrinter(name, PrinterAccessUse)
	if err != nil {
		return 0, 0, err
	}
	defer p.Close()

	buf, err := GetDevMode(p.Handle, name)
	if err != nil {
		return 0, 0, err
	}
	dm := devModeView(buf)
	return dm.Duplex, dm.Color, nil
}
