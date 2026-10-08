//go:build windows

package store

import (
	"io/fs"
	"syscall"
	"time"
)

// creationTime returns the creation time Windows keeps with a file.
func creationTime(info fs.FileInfo) time.Time {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return time.Time{}
	}
	return time.Unix(0, d.CreationTime.Nanoseconds())
}
