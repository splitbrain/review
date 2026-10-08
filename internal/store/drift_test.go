package store

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
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

	changed := checkDrift(t, st, "test.go")
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

	changed := checkDrift(t, st, "test.go")
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

	changed := checkDrift(t, st, "test.go")
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

	changed := checkDrift(t, st, "nonexistent.go")
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

	// An annotation without context adopts the current source as reference
	st.data["test.go"] = map[int]*Annotation{
		1: {Comment: "no context"},
	}

	if !checkDrift(t, st, "test.go") {
		t.Error("expected the annotation to adopt the current source")
	}
	ann := st.data["test.go"][1]
	if ann.ContextFrom != 1 {
		t.Errorf("expected ContextFrom=1, got %d", ann.ContextFrom)
	}
	if len(ann.Context) != 2 {
		t.Errorf("expected 2 context lines, got %d", len(ann.Context))
	}
	if ann.Outdated {
		t.Error("expected the annotation to not be outdated")
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

	pos := findContext(fileLines, []string{"b", "c", "d"}, 1)
	if pos != 2 {
		t.Errorf("expected position 2, got %d", pos)
	}

	pos = findContext(fileLines, []string{"x", "y"}, 1)
	if pos != 0 {
		t.Errorf("expected 0 for not found, got %d", pos)
	}

	pos = findContext(fileLines, []string{}, 1)
	if pos != 0 {
		t.Errorf("expected 0 for empty context, got %d", pos)
	}

	// A context longer than the file matches nothing
	pos = findContext([]string{"a"}, []string{"a", "b"}, 1)
	if pos != 0 {
		t.Errorf("expected 0 for a context longer than the file, got %d", pos)
	}
}

// TestFindContextPrefersNearestMatch verifies that a context appearing more than
// once relocates to the copy closest to where it used to be, rather than to
// whichever comes first in the file.
func TestFindContextPrefersNearestMatch(t *testing.T) {
	// The same three lines open a block at 1, 6 and 11
	fileLines := []string{
		"}", "", "func f() {",
		"\tbody", "",
		"}", "", "func f() {",
		"\tbody", "",
		"}", "", "func f() {",
		"\tbody",
	}
	context := []string{"}", "", "func f() {"}

	for near, want := range map[int]int{1: 1, 3: 1, 4: 6, 6: 6, 8: 6, 11: 11, 99: 11} {
		if got := findContext(fileLines, context, near); got != want {
			t.Errorf("searching from %d found %d, want %d", near, got, want)
		}
	}
}

// TestCheckDrift_RelocatesToNearestCopy verifies that inserting a block above an
// annotation moves it down to its own code, not up to an identical block.
func TestCheckDrift_RelocatesToNearestCopy(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "REVIEW.md")
	srcPath := filepath.Join(tmpDir, "test.go")

	block := "func x() {\n\treturn\n}\n"
	if err := os.WriteFile(srcPath, []byte(block+block), 0644); err != nil {
		t.Fatal(err)
	}

	st, err := Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	// Comment on the second copy's body
	if err := st.Set("test.go", 5, 5, "the second one"); err != nil {
		t.Fatal(err)
	}

	// Push everything down by inserting a line at the top
	if err := os.WriteFile(srcPath, []byte("// header\n"+block+block), 0644); err != nil {
		t.Fatal(err)
	}
	if !checkDrift(t, st, "test.go") {
		t.Fatal("expected the annotation to move")
	}

	anns := st.GetFile("test.go")
	if _, ok := anns[6]; !ok {
		t.Errorf("expected the annotation on line 6, got lines %v", slices.Sorted(maps.Keys(anns)))
	}
}

// TestOnChange_NoDeadlock verifies that Set/Delete don't deadlock when an
// OnChange callback reads from the store (as the file watcher does).
func TestOnChange_NoDeadlock(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "test.go")
	os.WriteFile(srcFile, []byte("line1\nline2\nline3\n"), 0644)

	mdPath := filepath.Join(tmpDir, "REVIEW.md")
	st, err := Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Register a callback that reads from the store, mimicking watcher.addAnnotatedFiles.
	st.OnChange(func() {
		_ = st.AnnotatedFiles()
	})

	// These will deadlock (and the test will time out) if notifyChange
	// is called while the write lock is still held.
	if err := st.Set("test.go", 1, 1, "comment"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := st.Delete("test.go", 1); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func keys(m map[int]*Annotation) []int {
	ks := make([]int, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// TestCheckDrift_OutdatedSurvivesReload guards against the stored context
// being replaced by the code that took the annotated line's place, which would
// make the outdated mark disappear on the next read.
func TestCheckDrift_OutdatedSurvivesReload(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "test.go")
	os.WriteFile(srcFile, []byte("l1\nl2\nl3\nTARGET\nl5\nl6\nl7\n"), 0644)

	mdPath := filepath.Join(tmpDir, "REVIEW.md")
	st, err := Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set("test.go", 4, 4, "look at this"); err != nil {
		t.Fatal(err)
	}

	// The annotated code is replaced by something else entirely
	os.WriteFile(srcFile, []byte("l1\nl2\nl3\nREPLACED\nl5\nl6\nl7\n"), 0644)
	if !checkDrift(t, st, "test.go") {
		t.Fatal("expected drift to be detected")
	}
	if !st.data["test.go"][4].Outdated {
		t.Fatal("expected the annotation to be marked outdated")
	}

	// Reading the review back must not silently clear the mark
	reloaded, err := Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.CheckAllDrift(); err != nil {
		t.Fatal(err)
	}
	ann := reloaded.data["test.go"][4]
	if ann == nil {
		t.Fatal("expected annotation on line 4")
	}
	if !ann.Outdated {
		t.Error("outdated mark was lost across a reload")
	}
}

// checkDrift runs drift detection on one file and fails the test if the result
// could not be written out.
func checkDrift(t *testing.T, st *Store, file string) bool {
	t.Helper()
	changed, err := st.CheckDrift(file)
	if err != nil {
		t.Fatalf("writing REVIEW.md after drift check: %v", err)
	}
	return changed
}
