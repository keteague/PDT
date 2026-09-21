//go:build !windows

package main

import (
	"errors"
	"os"
	"time"
)

// setFileModTime has no handle-based equivalent worth using off Windows;
// copyFileProgress falls back to a Chtimes by path.
func setFileModTime(f *os.File, t time.Time) error {
	return errors.New("not supported")
}
