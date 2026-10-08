//go:build darwin

package store

import (
	"io/fs"
	"syscall"
	"time"
)

// creationTime returns the file's birth time.
func creationTime(info fs.FileInfo) time.Time {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
}
