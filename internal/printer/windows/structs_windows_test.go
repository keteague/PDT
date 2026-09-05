package windows

import (
	"testing"
	"unsafe"
)

// These assert our hand-written Go structs are byte-for-byte the same layout
// as the real Win32 structs, since GetPrinter/SetPrinter/DocumentProperties
// read and write these as raw memory - a field-order or padding mistake here
// would silently corrupt data rather than fail to compile.
func TestDevModeSize(t *testing.T) {
	if got := unsafe.Sizeof(DevMode{}); got != 220 {
		t.Errorf("unsafe.Sizeof(DevMode{}) = %d, want 220 (the well-known base DEVMODE size)", got)
	}
}

func TestPrinterInfo2Size(t *testing.T) {
	// 13 pointers (8 bytes each on amd64/arm64) + 8 DWORDs (4 bytes each) = 136.
	if got := unsafe.Sizeof(PrinterInfo2{}); got != 136 {
		t.Errorf("unsafe.Sizeof(PrinterInfo2{}) = %d, want 136", got)
	}
}

func TestPortData1Size(t *testing.T) {
	// Hand-computed from the documented field layout: 962 bytes of declared
	// fields, plus a 2-byte alignment pad the C compiler (and Go, given the
	// same field order/types) inserts before the DWORD PortNumber field
	// immediately after the odd-length-in-DWORD-terms 540-byte Reserved
	// block, for 964 total.
	if got := unsafe.Sizeof(PortData1{}); got != 964 {
		t.Errorf("unsafe.Sizeof(PortData1{}) = %d, want 964", got)
	}
}

func TestDeletePortData1Size(t *testing.T) {
	// PortName(128) + Reserved0(98) = 226, then a 2-byte pad to 4-align
	// Version, + Version(4) + Reserved1(4) = 236.
	if got := unsafe.Sizeof(DeletePortData1{}); got != 236 {
		t.Errorf("unsafe.Sizeof(DeletePortData1{}) = %d, want 236", got)
	}
}
