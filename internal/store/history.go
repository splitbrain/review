package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ReviewDir is the directory holding retired copies of REVIEW.md, beside the
// review file at the root of the reviewed directory.
const ReviewDir = ".review-history"

// historyPrefix and historySuffix frame the timestamp in an archived review's
// name.
const (
	historyPrefix = "REVIEW-"
	historySuffix = ".md"
)

// historyTimeFormat is the layout of an archived review's timestamp.
const historyTimeFormat = "2006-01-02-150405"

// HistoryEnabled reports whether the review history directory exists.
func (s *Store) HistoryEnabled() bool {
	info, err := os.Stat(s.historyDir())
	return err == nil && info.IsDir()
}

// historyDir returns the absolute path of the review history directory.
func (s *Store) historyDir() string {
	return filepath.Join(filepath.Dir(s.mdPath), ReviewDir)
}

// ArchiveReview retires the current review so a new one can begin. With
// history enabled REVIEW.md is moved into the history directory, named after
// the time of its last change; otherwise it is deleted.
//
// It returns the absolute path the review was moved to, empty when it was
// deleted, and leaves the store empty.
func (s *Store) ArchiveReview() (string, error) {
	s.mu.Lock()
	dest, retired, err := s.archiveLocked()
	s.mu.Unlock()

	if err == nil && retired {
		// Let the watcher see the review is empty now, not removed behind it.
		s.notifyChange()
	}
	return dest, err
}

// archiveLocked retires the review. The caller holds the lock and notifies the
// watcher. It reports whether there was a review to retire.
func (s *Store) archiveLocked() (dest string, retired bool, err error) {
	info, err := os.Stat(s.mdPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.reset()
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat REVIEW.md: %w", err)
	}

	if s.HistoryEnabled() {
		dest, err = s.archiveTo(info)
	} else {
		err = os.Remove(s.mdPath)
	}
	if err != nil {
		return "", false, err
	}

	s.reset()
	return dest, true, nil
}

// reset empties the store, leaving the review ready for the next comment. The
// base is kept: it belongs to the invocation, not to the review file, so a new
// review is still made against the same point in history.
func (s *Store) reset() {
	s.data = make(map[string]map[int]*Annotation)
	s.started = ""
	s.created = ""
	s.modified = ""
}

// archiveTo moves REVIEW.md into the history directory and returns where it
// landed. info is the file metadata that dates the copy.
func (s *Store) archiveTo(info fs.FileInfo) (string, error) {
	dir := s.historyDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create review history: %w", err)
	}

	dest := filepath.Join(dir, historyName(dir, reviewStamp(info).Format(historyTimeFormat)))
	if err := moveFile(s.mdPath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// NextHistoryDest reports where the current review would be moved when the
// next one starts, relative to the reviewed directory, naming the file in the
// confirmation prompt. It reports false when history is off or there is no
// review to retire.
func (s *Store) NextHistoryDest() (string, bool) {
	info, err := os.Stat(s.mdPath)
	if err != nil || !s.HistoryEnabled() {
		return "", false
	}
	name := historyName(s.historyDir(), reviewStamp(info).Format(historyTimeFormat))
	return filepath.ToSlash(filepath.Join(ReviewDir, name)), true
}

// historyName returns the name an archived review takes in dir, numbered where
// one retired in the same second already holds the name.
func historyName(dir, stamp string) string {
	name := historyPrefix + stamp + historySuffix
	for n := 1; ; n++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s%s-%d%s", historyPrefix, stamp, n, historySuffix)
	}
}

// reviewStamp dates an archived review by its last change, falling back to the
// creation time where the filesystem records no modification time, and to the
// present where it records neither.
func reviewStamp(info fs.FileInfo) time.Time {
	if ts := info.ModTime(); !ts.IsZero() {
		return ts
	}
	if ts := creationTime(info); !ts.IsZero() {
		return ts
	}
	return time.Now()
}

// moveFile moves src to dest, falling back to a copy where the rename cannot
// cross filesystems.
func moveFile(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}

	content, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read REVIEW.md: %w", err)
	}
	if err := os.WriteFile(dest, content, 0644); err != nil {
		return fmt.Errorf("write review history: %w", err)
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove REVIEW.md: %w", err)
	}
	return nil
}
