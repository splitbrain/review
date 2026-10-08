package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newHistoryStore loads a store over a temporary project with one source file.
func newHistoryStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	st, err := Load(filepath.Join(dir, "REVIEW.md"), dir)
	if err != nil {
		t.Fatal(err)
	}
	return st, dir
}

// TestSetupReviewHistoryCreatesDirAndGitignore verifies that the setup creates
// the history directory, the note explaining it to agents, and the .gitignore
// entries.
func TestSetupReviewHistoryCreatesDirAndGitignore(t *testing.T) {
	dir := t.TempDir()

	created, err := SetupReviewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 3 {
		t.Fatalf("expected the directory, the note and .gitignore to be created, got %v", created)
	}
	if info, err := os.Stat(filepath.Join(dir, ReviewDir)); err != nil || !info.IsDir() {
		t.Fatalf("expected %s to exist as a directory: %v", ReviewDir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ReviewDir, "AGENTS.md")); err != nil {
		t.Errorf("expected the agent note in the history directory: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/REVIEW.md", "/" + ReviewDir + "/"} {
		if !strings.Contains(string(content), want) {
			t.Errorf("expected %q in .gitignore, got:\n%s", want, content)
		}
	}
}

// TestSetupReviewHistoryIsIdempotent verifies that running the setup again
// leaves the project alone.
func TestSetupReviewHistoryIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := SetupReviewHistory(dir); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}

	created, err := SetupReviewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 {
		t.Errorf("expected nothing created the second time, got %v", created)
	}
	after, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf(".gitignore was rewritten unchanged:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestSetupReviewHistoryKeepsExistingEntries verifies that an existing
// .gitignore without a trailing newline keeps its entries and gains the
// review's own on fresh lines.
func TestSetupReviewHistoryKeepsExistingEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("build/\n*.log"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := SetupReviewHistory(dir); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	for _, want := range []string{"build/", "*.log", "/REVIEW.md", "/" + ReviewDir + "/"} {
		found := false
		for _, line := range lines {
			if strings.TrimSpace(line) == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q as an entry of .gitignore, got:\n%s", want, content)
		}
	}
}

// TestArchiveReviewMovesToHistory verifies that, with history enabled, the
// review is moved aside under a name dating it.
func TestArchiveReviewMovesToHistory(t *testing.T) {
	st, dir := newHistoryStore(t)
	if _, err := SetupReviewHistory(dir); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}
	// A known modification time fixes the name the review is archived under.
	stamp := time.Date(2026, 10, 8, 15, 30, 0, 0, time.Local)
	if err := os.Chtimes(st.MdPath(), stamp, stamp); err != nil {
		t.Fatal(err)
	}

	dest, err := st.ArchiveReview()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ReviewDir, "REVIEW-2026-10-08-153000.md")
	if dest != want {
		t.Errorf("archived to %q, want %q", dest, want)
	}

	saved, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "a comment") {
		t.Errorf("archived review lost its comment:\n%s", saved)
	}
	if _, err := os.Stat(st.MdPath()); !os.IsNotExist(err) {
		t.Errorf("expected REVIEW.md to be gone from its old path, stat returned %v", err)
	}
	if got := st.All(); len(got) != 0 {
		t.Errorf("expected an empty review after archiving, got %v", got)
	}
}

// TestArchiveReviewWithoutHistoryDeletes verifies that a review without
// history is deleted and reports no archived path.
func TestArchiveReviewWithoutHistoryDeletes(t *testing.T) {
	st, _ := newHistoryStore(t)
	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}

	dest, err := st.ArchiveReview()
	if err != nil {
		t.Fatal(err)
	}
	if dest != "" {
		t.Errorf("expected a plain deletion, got dest=%q", dest)
	}
	if _, err := os.Stat(st.MdPath()); !os.IsNotExist(err) {
		t.Errorf("expected REVIEW.md to be deleted, stat returned %v", err)
	}
	if got := st.All(); len(got) != 0 {
		t.Errorf("expected an empty review after archiving, got %v", got)
	}
}

// TestArchiveReviewNumbersSameStamp verifies that two reviews retired within
// the same second do not overwrite one another.
func TestArchiveReviewNumbersSameStamp(t *testing.T) {
	st, dir := newHistoryStore(t)
	if _, err := SetupReviewHistory(dir); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 10, 8, 15, 30, 0, 0, time.Local)

	first := filepath.Join(dir, ReviewDir, "REVIEW-2026-10-08-153000.md")
	if err := os.WriteFile(first, []byte("an earlier review"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(st.MdPath(), stamp, stamp); err != nil {
		t.Fatal(err)
	}

	dest, err := st.ArchiveReview()
	if err != nil {
		t.Fatal(err)
	}
	if dest != filepath.Join(dir, ReviewDir, "REVIEW-2026-10-08-153000-1.md") {
		t.Errorf("archived to %q, want the numbered name", dest)
	}
	kept, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "an earlier review" {
		t.Errorf("the earlier review was overwritten: %q", kept)
	}
}

// TestNextHistoryDestMatchesArchive verifies that the path named before a new
// review starts is where the review is moved.
func TestNextHistoryDestMatchesArchive(t *testing.T) {
	st, dir := newHistoryStore(t)
	if _, err := SetupReviewHistory(dir); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 10, 8, 15, 30, 0, 0, time.Local)
	if err := os.Chtimes(st.MdPath(), stamp, stamp); err != nil {
		t.Fatal(err)
	}

	next, ok := st.NextHistoryDest()
	if !ok {
		t.Fatal("expected a destination while a review is in place")
	}
	if next != filepath.Join(ReviewDir, "REVIEW-2026-10-08-153000.md") {
		t.Fatalf("destination %q does not name the archived file", next)
	}

	dest, err := st.ArchiveReview()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.ToSlash(filepath.Join(filepath.Dir(st.MdPath()), next)) != dest {
		t.Errorf("destination %q does not match where the review went, %q", next, dest)
	}
}

// TestNextHistoryDestWithoutReview verifies that no destination is named
// without a review to retire or without history.
func TestNextHistoryDestWithoutReview(t *testing.T) {
	st, dir := newHistoryStore(t)
	if _, err := SetupReviewHistory(dir); err != nil {
		t.Fatal(err)
	}

	if dest, ok := st.NextHistoryDest(); ok {
		t.Errorf("expected no destination without a review, got %q", dest)
	}

	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, ReviewDir)); err != nil {
		t.Fatal(err)
	}
	if dest, ok := st.NextHistoryDest(); ok {
		t.Errorf("expected no destination without history, got %q", dest)
	}
}

// TestHistoryEnabledReflectsDirectory verifies that the flag follows the
// presence of the history directory.
func TestHistoryEnabledReflectsDirectory(t *testing.T) {
	st, dir := newHistoryStore(t)
	if st.HistoryEnabled() {
		t.Error("expected history to be off before setup")
	}
	if _, err := SetupReviewHistory(dir); err != nil {
		t.Fatal(err)
	}
	if !st.HistoryEnabled() {
		t.Error("expected history to be on after setup")
	}
}
