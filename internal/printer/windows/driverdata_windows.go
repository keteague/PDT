package windows

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PrinterDataValue is one opaque name/type/data entry from a printer's
// driver-private registry key (see EnumPrinterDataEx) - Type is whatever raw
// REG_* value the driver used (REG_DWORD, REG_BINARY, REG_SZ, ...), never
// interpreted here. This package has no driver-specific knowledge of what
// any of these values mean; copying them verbatim from a manually-configured
// reference printer to another machine's copy of the same printer object is
// enough to reproduce the same Device Settings tab state, the same way a
// captured DEVMODE reproduces Printing Defaults/Preferences.
type PrinterDataValue struct {
	Name string
	Type uint32
	Data []byte
}

// EnumPrinterDataEx reads every value under a printer's keyName registry key
// (PrinterDriverDataKeyName, in practice - see CaptureDriverData) as an
// opaque list, suitable for replaying verbatim via SetPrinterDataEx. Returns
// an empty (nil) slice, no error, if the key exists but has no values.
func EnumPrinterDataEx(h windows.Handle, keyName string) ([]PrinterDataValue, error) {
	keyPtr, err := windows.UTF16PtrFromString(keyName)
	if err != nil {
		return nil, err
	}
	var needed, count uint32
	r1, _, _ := procEnumPrinterDataExW.Call(uintptr(h), uintptr(unsafe.Pointer(keyPtr)), 0, 0, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)))
	runtime.KeepAlive(keyPtr)
	if needed == 0 {
		if r1 != 0 && syscall.Errno(r1) != syscall.ERROR_MORE_DATA {
			return nil, fmt.Errorf("EnumPrinterDataEx(size query, %q): %w", keyName, syscall.Errno(r1))
		}
		return nil, nil
	}

	buf := make([]byte, needed)
	r1, _, _ = procEnumPrinterDataExW.Call(uintptr(h), uintptr(unsafe.Pointer(keyPtr)), uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)))
	runtime.KeepAlive(keyPtr)
	if r1 != 0 {
		return nil, fmt.Errorf("EnumPrinterDataEx(%q): %w", keyName, syscall.Errno(r1))
	}

	entrySize := unsafe.Sizeof(PrinterEnumValues{})
	out := make([]PrinterDataValue, 0, count)
	for i := uint32(0); i < count; i++ {
		e := (*PrinterEnumValues)(unsafe.Pointer(&buf[uintptr(i)*entrySize]))
		name := ""
		if e.ValueName != nil {
			name = windows.UTF16PtrToString(e.ValueName)
		}
		var data []byte
		if e.DataSz > 0 && e.Data != nil {
			data = make([]byte, e.DataSz)
			copy(data, unsafe.Slice(e.Data, e.DataSz))
		}
		out = append(out, PrinterDataValue{Name: name, Type: e.Type, Data: data})
	}
	runtime.KeepAlive(buf)
	return out, nil
}

// SetPrinterDataEx writes value under keyName, verbatim - one call per
// PrinterDataValue, since SetPrinterDataExW only ever sets one value at a
// time (there's no bulk-set counterpart to EnumPrinterDataEx's bulk read).
func SetPrinterDataEx(h windows.Handle, keyName string, value PrinterDataValue) error {
	keyPtr, err := windows.UTF16PtrFromString(keyName)
	if err != nil {
		return err
	}
	namePtr, err := windows.UTF16PtrFromString(value.Name)
	if err != nil {
		return err
	}
	var dataPtr *byte
	if len(value.Data) > 0 {
		dataPtr = &value.Data[0]
	}
	r1, _, _ := procSetPrinterDataExW.Call(
		uintptr(h), uintptr(unsafe.Pointer(keyPtr)), uintptr(unsafe.Pointer(namePtr)),
		uintptr(value.Type), uintptr(unsafe.Pointer(dataPtr)), uintptr(len(value.Data)),
	)
	runtime.KeepAlive(keyPtr)
	runtime.KeepAlive(namePtr)
	runtime.KeepAlive(value.Data)
	if r1 != 0 {
		return fmt.Errorf("SetPrinterDataEx(%q, %q): %w", keyName, value.Name, syscall.Errno(r1))
	}
	return nil
}

// CaptureDriverData reads name's entire PrinterDriverData registry key - the
// Device Settings tab's usual backing store (installable options, form-to-
// tray assignment), which most drivers keep completely separate from
// DEVMODE. Companion to GetDevMode: called against the same manually-
// configured reference printer, in the same capture step (see
// App.CaptureDevModeForPrinter).
func CaptureDriverData(name string) ([]PrinterDataValue, error) {
	p, err := OpenPrinter(name, PrinterAccessUse)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	return EnumPrinterDataEx(p.Handle, PrinterDriverDataKeyName)
}

// ApplyDriverData replays a previously-captured PrinterDriverData registry
// key onto name, value by value. Best-effort per value (some values may be
// read-only, driver-version-specific, or simply not exist on this machine's
// copy of the driver) - one rejected value shouldn't block every other one
// from applying, so this collects and returns a joined error rather than
// stopping at the first failure.
func ApplyDriverData(name string, values []PrinterDataValue) error {
	if len(values) == 0 {
		return nil
	}
	p, err := OpenPrinter(name, PrinterAllAccess)
	if err != nil {
		return err
	}
	defer p.Close()

	var errs []error
	for _, v := range values {
		if err := SetPrinterDataEx(p.Handle, PrinterDriverDataKeyName, v); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d of %d driver data value(s) failed to apply: %w", len(errs), len(values), errs[0])
	}
	return nil
}
