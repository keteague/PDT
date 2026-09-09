package main

// setTitleBarBusy/resetTitleBarColor have no macOS equivalent - the yellow-
// during-deploy titlebar tint is a Windows DWM caption-color trick
// (titlebar_windows.go) with nothing analogous in Cocoa's own window chrome
// worth building for a cosmetic-only signal. No-ops, same signature, so
// app.go's Deploy can call them unconditionally on every platform.
func setTitleBarBusy()    {}
func resetTitleBarColor() {}
