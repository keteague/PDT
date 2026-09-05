package windows

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// OpenedPrinter wraps a live HANDLE from OpenPrinter/AddPrinter. Callers must
// Close() it (typically via defer) exactly once.
type OpenedPrinter struct {
	Handle windows.Handle
	Name   string
}

// OpenPrinter ports the PrinterAttributeHelper C# class's OpenPrinter call:
// always requests desiredAccess explicitly (PRINTER_ALL_ACCESS for anything
// that reads or writes) rather than relying on a NULL PRINTER_DEFAULTS, which
// grants a default access level too low for SetPrinter to succeed - the exact
// bug the original tool hit and fixed early on.
func OpenPrinter(name string, desiredAccess uint32) (*OpenedPrinter, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	defaults := PrinterDefaults{DesiredAccess: desiredAccess}
	var h windows.Handle
	r1, _, callErr := procOpenPrinterW.Call(
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&h)),
		uintptr(unsafe.Pointer(&defaults)),
	)
	runtime.KeepAlive(namePtr)
	runtime.KeepAlive(defaults)
	if r1 == 0 {
		return nil, fmt.Errorf("OpenPrinter(%q): %w", name, callErr)
	}
	return &OpenedPrinter{Handle: h, Name: name}, nil
}

func (p *OpenedPrinter) Close() error {
	r1, _, callErr := procClosePrinter.Call(uintptr(p.Handle))
	if r1 == 0 {
		return callErr
	}
	return nil
}

// PrinterInfo2Buffer holds a GetPrinter Level-2 result: the raw buffer, which
// every string pointer in Info points into (keep it alive as long as Info is
// used - it's referenced here so the Go GC won't reclaim it), and a typed
// view cast over that same memory.
type PrinterInfo2Buffer struct {
	buf  []byte
	Info *PrinterInfo2
}

// GetInfo2 ports the read half of Get-Printer/Set-Printer round-tripping:
// fetch the current PRINTER_INFO_2, let the caller mutate specific fields on
// Info, then pass the same buffer to SetInfo2 - every untouched pointer field
// still points at valid data from this same fetch.
func (p *OpenedPrinter) GetInfo2() (*PrinterInfo2Buffer, error) {
	var needed uint32
	procGetPrinterW.Call(uintptr(p.Handle), 2, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return nil, fmt.Errorf("GetPrinter(%q): could not determine buffer size", p.Name)
	}
	buf := make([]byte, needed)
	r1, _, callErr := procGetPrinterW.Call(uintptr(p.Handle), 2, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)))
	if r1 == 0 {
		return nil, fmt.Errorf("GetPrinter(%q): %w", p.Name, callErr)
	}
	return &PrinterInfo2Buffer{buf: buf, Info: (*PrinterInfo2)(unsafe.Pointer(&buf[0]))}, nil
}

// SetInfo2 commits a (possibly-modified) PRINTER_INFO_2 back via SetPrinter
// Level 2. b must have come from this same handle's GetInfo2 (or from
// CreatePrinter's own info) so every pointer field still points at live memory.
func (p *OpenedPrinter) SetInfo2(b *PrinterInfo2Buffer) error {
	r1, _, callErr := procSetPrinterW.Call(uintptr(p.Handle), 2, uintptr(unsafe.Pointer(b.Info)), printerControlNone)
	runtime.KeepAlive(b.buf)
	if r1 == 0 {
		return callErr
	}
	return nil
}

func mustUTF16Ptr(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

// CreatePrinter ports Add-Printer: creates a new local printer object bound
// to portName/driverName, returning it already open.
func CreatePrinter(name, driverName, portName, comment string) (*OpenedPrinter, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	portPtr, err := windows.UTF16PtrFromString(portName)
	if err != nil {
		return nil, err
	}
	driverPtr, err := windows.UTF16PtrFromString(driverName)
	if err != nil {
		return nil, err
	}
	commentPtr, err := windows.UTF16PtrFromString(comment)
	if err != nil {
		return nil, err
	}

	info := PrinterInfo2{
		PrinterName:    namePtr,
		PortName:       portPtr,
		DriverName:     driverPtr,
		Comment:        commentPtr,
		PrintProcessor: mustUTF16Ptr("winprint"),
		Datatype:       mustUTF16Ptr("RAW"),
	}
	r1, _, callErr := procAddPrinterW.Call(0, 2, uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(namePtr)
	runtime.KeepAlive(portPtr)
	runtime.KeepAlive(driverPtr)
	runtime.KeepAlive(commentPtr)
	runtime.KeepAlive(info)
	if r1 == 0 {
		return nil, fmt.Errorf("AddPrinter(%q): %w", name, callErr)
	}
	return &OpenedPrinter{Handle: windows.Handle(r1), Name: name}, nil
}

// DeletePrinterByName opens name with full access and removes it.
func DeletePrinterByName(name string) error {
	p, err := OpenPrinter(name, PrinterAllAccess)
	if err != nil {
		return err
	}
	defer p.Close()
	r1, _, callErr := procDeletePrinter.Call(uintptr(p.Handle))
	if r1 == 0 {
		return callErr
	}
	return nil
}

// EnumLocalPrinterNames lists every locally-installed printer's name, in
// place of the original tool's Get-Printer for name-collision checks.
func EnumLocalPrinterNames() ([]string, error) {
	const printerEnumLocal = 0x00000002
	var needed, returned uint32
	procEnumPrintersW.Call(printerEnumLocal, 0, 2, 0, 0, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		return nil, nil
	}
	buf := make([]byte, needed)
	r1, _, callErr := procEnumPrintersW.Call(printerEnumLocal, 0, 2, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if r1 == 0 {
		return nil, fmt.Errorf("EnumPrinters: %w", callErr)
	}
	names := make([]string, 0, returned)
	entrySize := unsafe.Sizeof(PrinterInfo2{})
	for i := uint32(0); i < returned; i++ {
		info := (*PrinterInfo2)(unsafe.Pointer(&buf[uintptr(i)*entrySize]))
		if info.PrinterName != nil {
			names = append(names, windows.UTF16PtrToString(info.PrinterName))
		}
	}
	return names, nil
}
