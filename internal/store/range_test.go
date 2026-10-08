package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRange_RoundTrip covers a comment on a run of lines: it is written as a
// "Lines S-E" heading, read back with both ends intact, and keyed by its end
// line.
func TestRange_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9"}
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(strings.Join(src, "\n")+"\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set("a.go", 3, 6, "this whole block needs work"); err != nil {
		t.Fatal(err)
	}

	// Only the end line keys the map, and the context spans the range.
	ann := st.data["a.go"][6]
	if ann == nil {
		t.Fatalf("expected the annotation keyed by its end line, got %v", st.data["a.go"])
	}
	if ann.StartLine != 3 {
		t.Errorf("StartLine: got %d, want 3", ann.StartLine)
	}
	if ann.ContextFrom != 1 || len(ann.Context) != 9 {
		t.Errorf("context should cover the range plus radius, got from=%d lines=%d", ann.ContextFrom, len(ann.Context))
	}

	written := mustRead(t, mdPath)
	if !strings.Contains(written, "#### Lines 3-6\n") {
		t.Errorf("expected a range heading, file is:\n%s", written)
	}

	reloaded, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.data["a.go"][6]
	if got == nil {
		t.Fatalf("annotation lost, file was:\n%s", mustRead(t, mdPath))
	}
	if got.StartLine != 3 || got.Comment != "this whole block needs work" {
		t.Errorf("range did not survive: %+v", got)
	}
}

// TestRange_SingleLineUnchanged checks that a one-line comment keeps the plain
// "Line N" heading, so review files stay readable and compatible.
func TestRange_SingleLineUnchanged(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, _ := Load(mdPath, dir)
	st.Set("a.go", 2, 2, "a note")

	written := mustRead(t, mdPath)
	if !strings.Contains(written, "#### Line 2\n") || strings.Contains(written, "Lines ") {
		t.Errorf("single-line comment should keep the Line heading, file is:\n%s", written)
	}
}

// TestRange_DriftFollowsBothEnds checks that when code moves, a range comment
// follows it with its start line shifted alongside its end.
func TestRange_DriftFollowsBothEnds(t *testing.T) {
	dir := t.TempDir()
	// Two lines inserted at the top of the file; the range moves down by two.
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("new1\nnew2\nl1\nl2\nl3\nl4\nl5\nl6\nl7\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, _ := Load(mdPath, dir)
	// Context as it looked before the insertion, covering lines 1-7 of the
	// original file.
	st.data["a.go"] = map[int]*Annotation{
		6: {
			Comment:     "block",
			StartLine:   3,
			Context:     []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7"},
			ContextFrom: 1,
		},
	}

	changed, err := st.CheckDrift("a.go")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected drift to be detected")
	}

	ann := st.data["a.go"][8]
	if ann == nil {
		t.Fatalf("expected the annotation relocated to end line 8, got %v", st.data["a.go"])
	}
	if ann.StartLine != 5 {
		t.Errorf("start line should shift with the range: got %d, want 5", ann.StartLine)
	}
	if ann.Outdated {
		t.Error("relocated range should not be marked outdated")
	}
}
