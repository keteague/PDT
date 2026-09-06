package main

import (
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// Ports Create-Printers.ps1's DwmHelper (DwmSetWindowAttribute/
// DWMWA_CAPTION_COLOR) - the titlebar turns yellow for the duration of a
// deploy run and always resets to the OS default when done, including on an
// unexpected error. Unlike that WinForms tool, which already had its Form's
// window handle in hand, Wails doesn't expose the native HWND through its
// public runtime API, so findMainWindow below locates it the same way any
// external tool would: enumerate top-level windows and match this process's
// own PID.
var (
	moduser32 = syscall.NewLazyDLL("user32.dll")
	moddwmapi = syscall.NewLazyDLL("dwmapi.dll")

	procEnumWindows              = moduser32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = moduser32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = moduser32.NewProc("IsWindowVisible")
	procDwmSetWindowAttribute    = moddwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	dwmwaCaptionColor = 35         // DWMWA_CAPTION_COLOR - Windows 11 (build 22000+) only.
	dwmwaColorDefault = 0xFFFFFFFF // Sentinel value: reset to the OS default caption color.

	captionColorYellow = 0x0000FFFF // COLORREF (0x00BBGGRR) for RGB(255,255,0).
)

var (
	mainWindowOnce   sync.Once
	mainWindowHandle syscall.Handle
)

// findMainWindow returns this process's own visible top-level window handle,
// found once and cached (the window's own HWND never changes for the life of
// the process).
func findMainWindow() syscall.Handle {
	mainWindowOnce.Do(func() {
		pid := uint32(os.Getpid())
		cb := syscall.NewCallback(func(hwnd syscall.Handle, _ uintptr) uintptr {
			var windowPid uint32
			procGetWindowThreadProcessId.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&windowPid)))
			if windowPid != pid {
				return 1 // keep enumerating
			}
			visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
			if visible == 0 {
				return 1 // keep enumerating - skip this process's hidden/helper windows
			}
			mainWindowHandle = hwnd
			return 0 // found it, stop enumerating
		})
		procEnumWindows.Call(cb, 0)
	})
	return mainWindowHandle
}

// setCaptionColor is a best-effort call: DWMWA_CAPTION_COLOR is silently
// unsupported (a harmless no-op, not an error) on anything older than
// Windows 11, and a titlebar color is cosmetic enough that a missing window
// handle (findMainWindow failing) isn't worth surfacing as an error either.
func setCaptionColor(colorref uint32) {
	hwnd := findMainWindow()
	if hwnd == 0 {
		return
	}
	procDwmSetWindowAttribute.Call(uintptr(hwnd), dwmwaCaptionColor, uintptr(unsafe.Pointer(&colorref)), unsafe.Sizeof(colorref))
}

func setTitleBarBusy()    { setCaptionColor(captionColorYellow) }
func resetTitleBarColor() { setCaptionColor(dwmwaColorDefault) }
