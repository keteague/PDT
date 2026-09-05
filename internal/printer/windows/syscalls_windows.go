package windows

// Raw winspool.drv bindings. golang.org/x/sys/windows has no coverage of the
// print-spooler API at all, so every one of these is hand-bound via
// LazyDLL/LazyProc, the same shape of problem Create-Printers.ps1's embedded
// C# PrinterAttributeHelper class already solved via raw DllImport for the
// PRINTER_ATTRIBUTE_RAW_ONLY bits - same idea, Go syscalls instead of P/Invoke.
//
// This file only declares the procs; printer_windows.go, apf_windows.go, and
// port_windows.go wrap them with Go-idiomatic error handling and struct
// marshaling.

import "golang.org/x/sys/windows"

var (
	modwinspool = windows.NewLazySystemDLL("winspool.drv")

	procOpenPrinterW   = modwinspool.NewProc("OpenPrinterW")
	procClosePrinter   = modwinspool.NewProc("ClosePrinter")
	procGetPrinterW    = modwinspool.NewProc("GetPrinterW")
	procSetPrinterW    = modwinspool.NewProc("SetPrinterW")
	procAddPrinterW    = modwinspool.NewProc("AddPrinterW")
	procDeletePrinter  = modwinspool.NewProc("DeletePrinter")
	procEnumPrintersW  = modwinspool.NewProc("EnumPrintersW")
	procXcvDataW            = modwinspool.NewProc("XcvDataW")
	procEnumPortsW          = modwinspool.NewProc("EnumPortsW")
	procDocumentPropertiesW = modwinspool.NewProc("DocumentPropertiesW")
)
