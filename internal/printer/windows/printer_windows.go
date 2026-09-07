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
//
// NOTE: per Microsoft's own "Per-User DEVMODE" documentation, SetPrinter
// Level 2's DevMode field is NOT the printer's global default despite
// PRINTER_INFO_2 looking like "the" printer info structure - it sets the
// per-user DEVMODE for whichever account is calling it. Never touch
// b.Info.DevMode here for anything Deploy needs visible to other users; use
// SetGlobalDevMode instead. This method still exists for every other
// PRINTER_INFO_2 field (Comment, ShareName, PortName, ...), which really are
// machine-wide and have nothing to do with per-user DEVMODE.
func (p *OpenedPrinter) SetInfo2(b *PrinterInfo2Buffer) error {
	r1, _, callErr := procSetPrinterW.Call(uintptr(p.Handle), 2, uintptr(unsafe.Pointer(b.Info)), printerControlNone)
	runtime.KeepAlive(b.buf)
	if r1 == 0 {
		return callErr
	}
	return nil
}

// SetPerUserDevMode commits dm as the CALLING USER's own per-user default
// DEVMODE, via SetPrinter Level 2 (PRINTER_INFO_2) - per Microsoft's "Per-
// User DEVMODE" documentation, that's what Level 2's DevMode field actually
// controls despite PRINTER_INFO_2 looking like "the" printer info structure
// (see SetInfo2's own note). It's what the General tab's "Preferences"
// button reads back for whoever has it open. Round-trips through GetInfo2
// first so every other PRINTER_INFO_2 field (Comment, PortName, ...) is
// preserved exactly as-is.
func (p *OpenedPrinter) SetPerUserDevMode(dm []byte) error {
	if len(dm) == 0 {
		return fmt.Errorf("SetPerUserDevMode(%q): empty DEVMODE data", p.Name)
	}
	info, err := p.GetInfo2()
	if err != nil {
		return err
	}
	info.Info.DevMode = uintptr(unsafe.Pointer(&dm[0]))
	err = p.SetInfo2(info)
	runtime.KeepAlive(dm)
	return err
}

// SetGlobalDevMode commits dm as the printer's GLOBAL default DEVMODE, via
// SetPrinter Level 8 (PRINTER_INFO_8) - the "administrator" default an
// end user actually gets on a freshly-deployed machine, visible via the
// Advanced tab's "Printing Defaults" button (and, for most drivers, the
// Device Settings tab's installable options too, which live in the same
// buffer's driver-private dmDriverExtra data). Unlike PRINTER_INFO_2,
// PRINTER_INFO_8 has no other fields to preserve, so there's no GetInfo8
// round-trip needed - just build the one-field struct and set it directly.
func (p *OpenedPrinter) SetGlobalDevMode(dm []byte) error {
	if len(dm) == 0 {
		return fmt.Errorf("SetGlobalDevMode(%q): empty DEVMODE data", p.Name)
	}
	info := PrinterInfo8{DevMode: uintptr(unsafe.Pointer(&dm[0]))}
	r1, _, callErr := procSetPrinterW.Call(uintptr(p.Handle), 8, uintptr(unsafe.Pointer(&info)), printerControlNone)
	runtime.KeepAlive(dm)
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

// LocalPrinterInfo is one locally-installed printer's identifying info, for
// the Import Printers dialog's physical-vs-virtual classification and
// best-effort field enrichment.
type LocalPrinterInfo struct {
	Name       string
	PortName   string
	DriverName string
}

// EnumLocalPrinters lists every locally-installed printer with enough detail
// to guess whether it's a real network printer or a virtual/software one
// (e.g. "Microsoft Print to PDF") - the same EnumPrinters Level-2 call as
// EnumLocalPrinterNames, just reading PortName/DriverName off each entry too
// instead of only PrinterName.
func EnumLocalPrinters() ([]LocalPrinterInfo, error) {
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
	out := make([]LocalPrinterInfo, 0, returned)
	entrySize := unsafe.Sizeof(PrinterInfo2{})
	for i := uint32(0); i < returned; i++ {
		info := (*PrinterInfo2)(unsafe.Pointer(&buf[uintptr(i)*entrySize]))
		if info.PrinterName == nil {
			continue
		}
		lp := LocalPrinterInfo{Name: windows.UTF16PtrToString(info.PrinterName)}
		if info.PortName != nil {
			lp.PortName = windows.UTF16PtrToString(info.PortName)
		}
		if info.DriverName != nil {
			lp.DriverName = windows.UTF16PtrToString(info.DriverName)
		}
		out = append(out, lp)
	}
	return out, nil
}
