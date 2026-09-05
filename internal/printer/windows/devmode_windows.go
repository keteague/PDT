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

	info, err := p.GetInfo2()
	if err != nil {
		return err
	}
	info.Info.DevMode = uintptr(unsafe.Pointer(&validated[0]))
	err = p.SetInfo2(info)
	runtime.KeepAlive(validated)
	return err
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
