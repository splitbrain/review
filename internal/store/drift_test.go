package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckDrift_NoChange(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "test.go")
	os.WriteFile(srcFile, []byte("line1\nline2\nline3\nline4\nline5\n"), 0644)

	mdPath := filepath.Join(tmpDir, "REVIEW.md")
	st, err := Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Manually set annotation with context
	st.data["test.go"] = map[int]*Annotation{
		3: {
			Comment:     "test comment",
			Context:     []string{"line2", "line3", "line4"},
			ContextFrom: 2,
		},
	}

	changed := st.CheckDrift("test.go")
	if changed {
		t.Error("expected no change when context matches")
	}
}

func TestCheckDrift_Relocated(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "test.go")
	// Original context was at lines 2-4, now shifted down by 2 (new lines added at top)
	os.WriteFile(srcFile, []byte("new1\nnew2\nline1\nline2\nline3\nline4\nline5\n"), 0644)

	mdPath := filepath.Join(tmpDir, "REVIEW.md")
	st, err := Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	st.data["test.go"] = map[int]*Annotation{
		3: {
			Comment:     "test comment",
			Context:     []string{"line2", "line3", "line4"},
			ContextFrom: 2,
		},
	}

	changed := st.CheckDrift("test.go")
	if !changed {
		t.Fatal("expected change when context moved")
	}

	// Annotation should have moved from line 3 to line 5 (delta of 2)
	if _, ok := st.data["test.go"][5]; !ok {
		t.Errorf("expected annotation relocated to line 5, got keys: %v", keys(st.data["test.go"]))
	}
	if _, ok := st.data["test.go"][3]; ok {
		t.Error("old line 3 annotation should have been removed")
	}
}

func TestCheckDrift_Outdated(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "test.go")
	// Context lines no longer exist in the file
	os.WriteFile(srcFile, []byte("completely\ndifferent\ncontent\n"), 0644)

	mdPath := filepath.Join(tmpDir, "REVIEW.md")
	st, err := Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	st.data["test.go"] = map[int]*Annotation{
		3: {
			Comment:     "test comment",
			Context:     []string{"line2", "line3", "line4"},
			ContextFrom: 2,
		},
	}

	changed := st.CheckDrift("test.go")
	if !changed {
		t.Fatal("expected change when context not found")
	}

	ann := st.data["test.go"][3]
	if ann == nil {
		t.Fatal("annotation should still exist")
	}
	if !ann.Outdated {
		t.Error("annotation should be marked as outdated")
	}
}

func TestCheckDrift_FileDeleted(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "REVIEW.md")
	st, err := Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	st.data["nonexistent.go"] = map[int]*Annotation{
		3: {
			Comment:     "test comment",
			Context:     []string{"line2", "line3", "line4"},
			ContextFrom: 2,
		},
	}

	changed := st.CheckDrift("nonexistent.go")
	if !changed {
		t.Fatal("expected change when file doesn't exist")
	}

	ann := st.data["nonexistent.go"][3]
	if !ann.Outdated {
		t.Error("annotation should be marked as outdated when file is deleted")
	}
}

func TestCheckDrift_NoContext(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "test.go")
	os.WriteFile(srcFile, []byte("line1\nline2\n"), 0644)

	mdPath := filepath.Join(tmpDir, "REVIEW.md")
	st, err := Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Annotation without context should be skipped
	st.data["test.go"] = map[int]*Annotation{
		1: {Comment: "no context"},
	}

	changed := st.CheckDrift("test.go")
	if changed {
		t.Error("expected no change for annotation without context")
	}
}

func TestContextMatchesAt(t *testing.T) {
	fileLines := []string{"a", "b", "c", "d", "e"}

	if !contextMatchesAt(fileLines, []string{"b", "c", "d"}, 2) {
		t.Error("expected match at position 2")
	}
	if contextMatchesAt(fileLines, []string{"b", "c", "d"}, 3) {
		t.Error("expected no match at position 3")
	}
	if contextMatchesAt(fileLines, []string{"b", "c", "d"}, 0) {
		t.Error("expected no match at position 0")
	}
	if contextMatchesAt(fileLines, []string{"d", "e", "f"}, 4) {
		t.Error("expected no match when context extends beyond file")
	}
}

func TestFindContext(t *testing.T) {
	fileLines := []string{"a", "b", "c", "d", "e"}

	pos := findContext(fileLines, []string{"b", "c", "d"})
	if pos != 2 {
		t.Errorf("expected position 2, got %d", pos)
	}

	pos = findContext(fileLines, []string{"x", "y"})
	if pos != 0 {
		t.Errorf("expected 0 for not found, got %d", pos)
	}

	pos = findContext(fileLines, []string{})
	if pos != 0 {
		t.Errorf("expected 0 for empty context, got %d", pos)
	}
}

func keys(m map[int]*Annotation) []int {
	ks := make([]int, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
