package windows

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modsetupapi = windows.NewLazySystemDLL("setupapi.dll")

	procSetupCopyOEMInfW = modsetupapi.NewProc("SetupCopyOEMInfW")
	// Microsoft's docs list InstallPrinterDriverFromPackageW's DLL as
	// Spoolss.dll, but on this (client-edition) machine that export doesn't
	// exist there at runtime - it's exported from winspool.drv instead,
	// matching the documented import library (Winspool.lib) more literally
	// than the documented DLL.
	procInstallPrinterDriverFromPackageW = modwinspool.NewProc("InstallPrinterDriverFromPackageW")
)

const (
	spostPath           = 1 // SPOST_PATH
	ipdfpCopyAllFiles    = 0x00000001
)

// StageInf ports the Win32 equivalent of what pnputil.exe does: copies infPath
// (and every file it references, resolved relative to infPath's own folder,
// since OEMSourceMediaLocation is left nil under SPOST_PATH) into the driver
// store, returning the staged copy's path (e.g. C:\Windows\INF\oemNN.inf).
// This alone does NOT register the driver with the print spooler - that's
// InstallDriverFromPackage's job, using this function's result as input.
// Requires administrator privileges (SetupCopyOEMInfW fails outright without
// them).
func StageInf(infPath string) (destInfPath string, err error) {
	srcPtr, err := windows.UTF16PtrFromString(infPath)
	if err != nil {
		return "", err
	}
	destBuf := make([]uint16, windows.MAX_PATH)
	var required uint32

	r1, _, callErr := procSetupCopyOEMInfW.Call(
		uintptr(unsafe.Pointer(srcPtr)),
		0, // OEMSourceMediaLocation = NULL -> with SPOST_PATH, uses infPath's own folder
		spostPath,
		0, // CopyStyle = 0: force overwrite unconditionally, same as pnputil's default
		uintptr(unsafe.Pointer(&destBuf[0])),
		uintptr(len(destBuf)),
		uintptr(unsafe.Pointer(&required)),
		0, // DestinationInfFileNameComponent - not needed
	)
	runtime.KeepAlive(srcPtr)
	runtime.KeepAlive(destBuf)
	if r1 == 0 {
		return "", fmt.Errorf("SetupCopyOEMInf(%q): %w", infPath, callErr)
	}
	return windows.UTF16ToString(destBuf), nil
}

// InstallDriverFromPackage registers driverName (a friendly name declared in
// the staged INF - the same string driver.ResolvedDriver.Name already gives
// us) with the print spooler, resolving and copying every file the package
// needs itself - the modern, purpose-built replacement for hand-populating
// DRIVER_INFO_3/6's file-list fields via AddPrinterDriverExW, which Microsoft's
// own docs now steer callers away from for exactly this scenario ("Installing
// a printer driver without a driver package is no longer recommended").
// destInfPath must be StageInf's result, not the original vendor INF path -
// this function only accepts a driver-store path. copyAllFiles forces every
// file to be (re)copied regardless of timestamp, matching Add-PrinterDriver's
// own effective behavior on a fresh install (needed the first time a given
// driver name is added; the timestamp-aware default is fine for later updates).
func InstallDriverFromPackage(destInfPath, driverName string, copyAllFiles bool) error {
	infPtr, err := windows.UTF16PtrFromString(destInfPath)
	if err != nil {
		return err
	}
	namePtr, err := windows.UTF16PtrFromString(driverName)
	if err != nil {
		return err
	}

	var flags uintptr
	if copyAllFiles {
		flags = ipdfpCopyAllFiles
	}

	hr, _, callErr := procInstallPrinterDriverFromPackageW.Call(
		0, // pszServer = NULL -> local machine
		uintptr(unsafe.Pointer(infPtr)),
		uintptr(unsafe.Pointer(namePtr)),
		0, // pszEnvironment = NULL -> this machine's native environment
		flags,
	)
	runtime.KeepAlive(infPtr)
	runtime.KeepAlive(namePtr)
	if int32(hr) != 0 { // S_OK == 0
		return fmt.Errorf("InstallPrinterDriverFromPackage(%q, %q): HRESULT 0x%08X: %w", destInfPath, driverName, uint32(hr), callErr)
	}
	return nil
}

// EnsureDriverInstalled stages infPath and registers driverName from it in
// one call - the direct Win32 equivalent of Add-PrinterDriver -InfPath.
func EnsureDriverInstalled(infPath, driverName string) error {
	staged, err := StageInf(infPath)
	if err != nil {
		return fmt.Errorf("staging driver: %w", err)
	}
	if err := InstallDriverFromPackage(staged, driverName, true); err != nil {
		return fmt.Errorf("installing driver: %w", err)
	}
	return nil
}
