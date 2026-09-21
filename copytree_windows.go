package main

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// setFileModTime sets f's last-write time through its own open handle - see
// copyFileProgress for why that beats re-opening the file by path afterwards.
func setFileModTime(f *os.File, t time.Time) error {
	ft := windows.NsecToFiletime(t.UnixNano())
	return windows.SetFileTime(windows.Handle(f.Fd()), nil, nil, &ft)
}
