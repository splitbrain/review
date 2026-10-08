package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReviewInfoWrittenAndRead verifies that the base, creation and
// modification times are written to REVIEW.md and read back.
func TestReviewInfoWrittenAndRead(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.SetBase("abc1234 (main) — first line of the commit [dirty changeset]")
	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}

	meta, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.base != "abc1234 (main) — first line of the commit [dirty changeset]" {
		t.Errorf("base: got %q", meta.base)
	}
	if meta.created == "" || meta.modified == "" {
		t.Fatalf("expected creation and modification times, got created=%q modified=%q", meta.created, meta.modified)
	}
	if _, err := time.Parse(stampFormat, meta.created); err != nil {
		t.Errorf("creation time %q is not in the recorded layout: %v", meta.created, err)
	}
	if _, err := time.Parse(stampFormat, meta.modified); err != nil {
		t.Errorf("modification time %q is not in the recorded layout: %v", meta.modified, err)
	}
}

// TestReviewCreatedTimePreserved verifies that the creation time is recorded
// once and kept across later changes, while the modification time moves.
func TestReviewCreatedTimePreserved(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set("a.go", 2, "first"); err != nil {
		t.Fatal(err)
	}
	first, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1100 * time.Millisecond)
	if err := st.Set("a.go", 3, "second"); err != nil {
		t.Fatal(err)
	}
	second, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}

	if second.created != first.created {
		t.Errorf("creation time moved from %q to %q", first.created, second.created)
	}
	if second.modified == first.modified {
		t.Errorf("modification time did not move from %q", first.modified)
	}
}

// TestReviewBaseUpdatedWhenChanged verifies that the recorded base follows a
// caller that reports a different one.
func TestReviewBaseUpdatedWhenChanged(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.SetBase("old base")
	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}

	// Open the same review against a different revision: the base of the
	// invocation replaces the recorded one on the next write.
	reloaded, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.SetBase("new base")
	if err := reloaded.Set("a.go", 3, "another comment"); err != nil {
		t.Fatal(err)
	}

	meta, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.base != "new base" {
		t.Errorf("base: got %q, want %q", meta.base, "new base")
	}
}

// TestReviewBaseKeptAcrossReload verifies that a base set for this invocation
// survives reading the file back, so the review is not relabelled with the
// base recorded in it.
func TestReviewBaseKeptAcrossReload(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.SetBase("recorded base")
	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}

	// A fresh invocation naming a different base keeps its own.
	other, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	other.SetBase("invocation base")
	if _, err := other.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := other.Set("a.go", 3, "another comment"); err != nil {
		t.Fatal(err)
	}

	meta, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.base != "invocation base" {
		t.Errorf("base: got %q, want %q", meta.base, "invocation base")
	}
}

// TestReviewBaseKeptAcrossNewReview verifies that starting a new review keeps
// the base, so the review that follows is made against the same point in
// history rather than losing its label.
func TestReviewBaseKeptAcrossNewReview(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.SetBase("a base")
	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ArchiveReview(); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("a.go", 3, "after the new review"); err != nil {
		t.Fatal(err)
	}

	meta, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.base != "a base" {
		t.Errorf("base: got %q, want %q", meta.base, "a base")
	}
}

// TestReviewInfoAbsentLeftOut verifies that a store with no base and no times
// does not write empty header lines, so a plain review file stays plain.
func TestReviewInfoAbsentLeftOut(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	// A write always stamps the times, so they are present; the base is not set.
	if err := st.Set("a.go", 2, "a comment"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "_Base:") {
		t.Errorf("expected no base line without a base, file is:\n%s", content)
	}
	if !strings.Contains(string(content), "_Created:") || !strings.Contains(string(content), "_Modified:") {
		t.Errorf("expected the times in the header, file is:\n%s", content)
	}
}

// TestParse_LegacyFileWithoutReviewInfo keeps a review file written before the
// base and times were recorded readable.
func TestParse_LegacyFileWithoutReviewInfo(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "REVIEW.md")
	content := "# Code Review\n\n_Started: 2020-01-01_\n\n---\n\n## `a.go`\n\n#### Line 1\n\n> old comment\n"
	os.WriteFile(mdPath, []byte(content), 0644)

	meta, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.base != "" || meta.created != "" || meta.modified != "" {
		t.Errorf("expected no review info, got base=%q created=%q modified=%q", meta.base, meta.created, meta.modified)
	}
	if meta.started != "2020-01-01" {
		t.Errorf("start date: got %q", meta.started)
	}
}
