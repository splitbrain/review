package gitstatus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSummary_HeadWithWorkingChanges verifies that a review of the working set
// names the commit it was taken from, its subject, and that the tree carries
// changes beyond it.
func TestSummary_HeadWithWorkingChanges(t *testing.T) {
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	summary := Base{}.Summary(root)
	if !strings.Contains(summary, "HEAD") {
		t.Errorf("expected HEAD to be named, got %q", summary)
	}
	if !strings.Contains(summary, "init") {
		t.Errorf("expected the commit's first line, got %q", summary)
	}
	if !strings.HasSuffix(summary, "[dirty changeset]") {
		t.Errorf("expected the working changes to be marked, got %q", summary)
	}
}

// TestSummary_CleanHeadOmitsDirtyMark verifies that a clean tree is not marked
// as having changes.
func TestSummary_CleanHeadOmitsDirtyMark(t *testing.T) {
	root := initRepo(t)

	summary := Base{}.Summary(root)
	if strings.Contains(summary, "[dirty changeset]") {
		t.Errorf("expected no dirty mark on a clean tree, got %q", summary)
	}
}

// TestSummary_BaseRevisionNamed verifies that the revision the user gave is
// recorded next to the commit it resolved to.
func TestSummary_BaseRevisionNamed(t *testing.T) {
	root := initRepo(t)

	base, err := ResolveBase(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	summary := base.Summary(root)
	if !strings.Contains(summary, "HEAD") {
		t.Errorf("expected the given revision, got %q", summary)
	}
	if !strings.Contains(summary, "init") {
		t.Errorf("expected the commit's first line, got %q", summary)
	}
}

// TestSummary_OutsideRepository verifies that no summary is produced where
// there is no history to name.
func TestSummary_OutsideRepository(t *testing.T) {
	if summary := (Base{}).Summary(t.TempDir()); summary != "" {
		t.Errorf("expected no summary outside a repository, got %q", summary)
	}
}
