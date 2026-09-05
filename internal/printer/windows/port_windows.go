package windows

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const NulPortName = "NUL:"

// ServerAccessAdminister is required in PRINTER_DEFAULTS.DesiredAccess for
// any XcvData call that mutates state (AddPort/DeletePort), per the TCPMON
// Xcv Interface documentation - the same Xcv contract every port monitor
// (including "Local Port", used here) implements.
const ServerAccessAdminister = 0x00000001

// openXcvMonitor opens an Xcv data handle to a port monitor by name, the
// mechanism XcvData-based port operations are built on. The original
// AddPortEx approach was tried first and rejected: it's documented as
// "obsolete...for use only with Windows NT 4.0 and previous versions", and
// returned ERROR_INVALID_PARAMETER in practice against the modern Local Port
// monitor - XcvData is the real, current replacement (also what Standard
// TCP/IP port creation will need later).
func openXcvMonitor(monitorName string) (windows.Handle, error) {
	namePtr, err := windows.UTF16PtrFromString(",XcvMonitor " + monitorName)
	if err != nil {
		return 0, err
	}
	defaults := PrinterDefaults{DesiredAccess: ServerAccessAdminister}
	var h windows.Handle
	r1, _, callErr := procOpenPrinterW.Call(
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&h)),
		uintptr(unsafe.Pointer(&defaults)),
	)
	runtime.KeepAlive(namePtr)
	runtime.KeepAlive(defaults)
	if r1 == 0 {
		return 0, fmt.Errorf("OpenPrinter(,XcvMonitor %s): %w", monitorName, callErr)
	}
	return h, nil
}

// xcvDataRaw wraps XcvDataW against an arbitrary input payload (a UTF-16
// string for simple commands like "AddPort" on the Local Port monitor, or a
// fixed-layout struct like PORT_DATA_1/DELETE_PORT_DATA_1 for the Standard
// TCP/IP port monitor's commands). Per Microsoft's documentation, XcvData's
// own BOOL return only indicates whether the request reached the monitor -
// the real per-command result is the status out-parameter, which callers
// must check.
func xcvDataRaw(hXcv windows.Handle, dataName string, input unsafe.Pointer, inputLen uintptr) (status uint32, err error) {
	dataNamePtr, err := windows.UTF16PtrFromString(dataName)
	if err != nil {
		return 0, err
	}

	var needed uint32
	r1, _, callErr := procXcvDataW.Call(
		uintptr(hXcv),
		uintptr(unsafe.Pointer(dataNamePtr)),
		uintptr(input),
		inputLen,
		0, 0,
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&status)),
	)
	runtime.KeepAlive(dataNamePtr)
	if r1 == 0 {
		return 0, fmt.Errorf("XcvData(%s): %w", dataName, callErr)
	}
	return status, nil
}

// xcvDataUTF16 is xcvDataRaw for the common case of a plain UTF-16 string
// input (what most monitors' simple commands, like Local Port's "AddPort",
// expect).
func xcvDataUTF16(hXcv windows.Handle, dataName string, input []uint16) (status uint32, err error) {
	if len(input) == 0 {
		return xcvDataRaw(hXcv, dataName, nil, 0)
	}
	status, err = xcvDataRaw(hXcv, dataName, unsafe.Pointer(&input[0]), uintptr(len(input))*2)
	runtime.KeepAlive(input)
	return status, err
}

// EnsureNulPort creates the local NUL: port if it doesn't already exist, via
// XcvData "AddPort" against the "Local Port" monitor - a silent, no-UI
// equivalent of Add-PrinterPort -Name 'NUL:'. Safe to call repeatedly.
func EnsureNulPort() error {
	h, err := openXcvMonitor("Local Port")
	if err != nil {
		return err
	}
	defer procClosePrinter.Call(uintptr(h))

	portNameUTF16, err := windows.UTF16FromString(NulPortName)
	if err != nil {
		return err
	}

	status, err := xcvDataUTF16(h, "AddPort", portNameUTF16)
	if err != nil {
		return err
	}
	if status != 0 && status != uint32(windows.ERROR_ALREADY_EXISTS) {
		return fmt.Errorf("XcvData(AddPort, %s): %w", NulPortName, syscall.Errno(status))
	}
	return nil
}
