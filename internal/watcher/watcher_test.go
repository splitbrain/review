package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"review/internal/store"
)

// newTestWatcher sets up a store and a running watcher over a temporary tree.
func newTestWatcher(t *testing.T) (*store.Store, *Watcher, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	st, err := store.Load(filepath.Join(dir, "REVIEW.md"), dir)
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(st)
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	t.Cleanup(w.Stop)
	time.Sleep(100 * time.Millisecond) // let the watch settle

	return st, w, dir
}

// drainEvents consumes the events queued so far, so that a test only sees what
// the change it makes itself produces.
func drainEvents(w *Watcher) {
	for {
		select {
		case <-w.Events():
		case <-time.After(700 * time.Millisecond):
			return
		}
	}
}

// TestOwnWriteIsNotReported verifies that saving a comment does not come back
// as an external change to REVIEW.md.
func TestOwnWriteIsNotReported(t *testing.T) {
	st, w, _ := newTestWatcher(t)

	if err := st.Set("a.go", 2, 2, "a comment"); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-w.Events():
		t.Errorf("own write reported as %q", ev.Type)
	case <-time.After(1500 * time.Millisecond):
	}
}

// TestForeignWriteIsReported verifies that a change made outside the tool is
// still picked up.
func TestForeignWriteIsReported(t *testing.T) {
	_, w, dir := newTestWatcher(t)

	content := "# Code Review\n\n_Started: 2020-01-01_\n\n---\n\n## `a.go`\n\n#### Line 2\n\n> edited by hand\n"
	if err := os.WriteFile(filepath.Join(dir, "REVIEW.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-w.Events():
		if ev.Type != "review-reloaded" {
			t.Errorf("expected review-reloaded, got %q", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Error("external change to REVIEW.md was not reported")
	}
}

// TestOwnDeletionIsNotReported verifies that starting a new review, which
// removes REVIEW.md, does not come back as annotations lost behind our back.
func TestOwnDeletionIsNotReported(t *testing.T) {
	st, w, _ := newTestWatcher(t)

	if err := st.Set("a.go", 2, 2, "a comment"); err != nil {
		t.Fatal(err)
	}
	drainEvents(w)

	// What the "New Review" endpoint does
	if err := os.Remove(st.MdPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Reload(); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-w.Events():
		t.Errorf("own deletion reported as %q", ev.Type)
	case <-time.After(1500 * time.Millisecond):
	}
}

// TestForeignDeletionIsReported verifies that losing REVIEW.md to something
// else is still reported.
func TestForeignDeletionIsReported(t *testing.T) {
	st, w, _ := newTestWatcher(t)

	if err := st.Set("a.go", 2, 2, "a comment"); err != nil {
		t.Fatal(err)
	}
	drainEvents(w)

	if err := os.Remove(st.MdPath()); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-w.Events():
		if ev.Type != "review-deleted" {
			t.Errorf("expected review-deleted, got %q", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Error("deletion of REVIEW.md was not reported")
	}
}

// TestViewedFileIsReportedWithoutDrift verifies that an edit to the file a
// client has open is passed on even when it leaves every annotation in place.
func TestViewedFileIsReportedWithoutDrift(t *testing.T) {
	st, w, dir := newTestWatcher(t)

	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("l1\nl2\nl3\nl4\nl5\nl6\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("a.go", 2, 2, "a comment"); err != nil {
		t.Fatal(err)
	}
	w.WatchFile("a.go")
	drainEvents(w)

	// Change a line far away from the annotation's context
	if err := os.WriteFile(path, []byte("l1\nl2\nl3\nl4\nl5\nedited\n"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-w.Events():
		if ev.Path != "a.go" {
			t.Errorf("expected event for a.go, got %q", ev.Path)
		}
	case <-time.After(3 * time.Second):
		t.Error("edit to the file being viewed was not reported")
	}
}

// TestWatchFileDoesNotAccumulate verifies that browsing files leaves only the
// directories that are still needed under watch.
func TestWatchFileDoesNotAccumulate(t *testing.T) {
	st, w, dir := newTestWatcher(t)

	for _, sub := range []string{"one", "two", "three"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "f.go"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		w.WatchFile(filepath.Join(sub, "f.go"))
	}

	// The review directory and the last viewed file's directory remain
	w.watchedMu.Lock()
	got := len(w.watched)
	w.watchedMu.Unlock()
	if got != 2 {
		t.Errorf("expected 2 watched directories, got %d", got)
	}

	// An annotated file keeps its directory watched even when another is viewed
	if err := st.Set("one/f.go", 1, 1, "note"); err != nil {
		t.Fatal(err)
	}
	w.WatchFile("a.go")
	w.watchedMu.Lock()
	got = len(w.watched)
	w.watchedMu.Unlock()
	if got != 2 {
		t.Errorf("expected 2 watched directories (review root and one/), got %d", got)
	}
}

// TestWatchFileRejectsEscapingPaths verifies that a client cannot make the
// watcher look outside the reviewed directory.
func TestWatchFileRejectsEscapingPaths(t *testing.T) {
	_, w, _ := newTestWatcher(t)

	for _, path := range []string{"../outside.go", "../../etc/passwd", ""} {
		w.WatchFile(path)
		if viewed := w.currentlyViewed(); viewed != "" {
			t.Errorf("path %q was accepted as %q", path, viewed)
		}
	}
}

// TestNewFileIsReported verifies that a file appearing in a watched directory
// tells clients their file tree is out of date.
func TestNewFileIsReported(t *testing.T) {
	_, w, dir := newTestWatcher(t)

	w.WatchFile("a.go") // puts the root under watch
	drainEvents(w)

	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-w.Events():
		if ev.Type != "tree-changed" {
			t.Errorf("expected tree-changed, got %q", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Error("a new file was not reported")
	}
}
