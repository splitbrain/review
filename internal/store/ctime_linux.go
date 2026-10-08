//go:build linux

package store

import (
	"io/fs"
	"syscall"
	"time"
)

// creationTime returns the inode change time, the closest a Linux filesystem
// comes to recording when a file appeared.
func creationTime(info fs.FileInfo) time.Time {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}
	}
	return time.Unix(st.Ctim.Sec, st.Ctim.Nsec)
}
