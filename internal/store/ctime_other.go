//go:build !linux && !darwin && !windows

package store

import (
	"io/fs"
	"time"
)

// creationTime reports that no creation time is available on these platforms.
func creationTime(fs.FileInfo) time.Time {
	return time.Time{}
}
