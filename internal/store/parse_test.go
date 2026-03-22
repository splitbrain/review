package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_ContextCapture(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "REVIEW.md")

	content := `# Code Review

_Started: 2026-03-22_

---

## ` + "`test.go`" + `

#### Line 5

This is a comment

` + "```go" + `
3: func main() {
4: 	x := 1
5: 	fmt.Println(x)
6: 	y := 2
7: 	fmt.Println(y)
` + "```" + `
`
	os.WriteFile(mdPath, []byte(content), 0644)

	data, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}

	ann := data["test.go"][5]
	if ann == nil {
		t.Fatal("expected annotation on line 5")
	}
	if ann.Comment != "This is a comment" {
		t.Errorf("unexpected comment: %q", ann.Comment)
	}
	if ann.ContextFrom != 3 {
		t.Errorf("expected ContextFrom=3, got %d", ann.ContextFrom)
	}
	if len(ann.Context) != 5 {
		t.Errorf("expected 5 context lines, got %d", len(ann.Context))
	}
	if ann.Context[0] != "func main() {" {
		t.Errorf("unexpected first context line: %q", ann.Context[0])
	}
}

func TestParse_OutdatedMarker(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "REVIEW.md")

	content := `# Code Review

_Started: 2026-03-22_

---

## ` + "`test.go`" + `

#### Line 5 (outdated)

This is outdated

` + "```go" + `
3: old code
4: old code
5: old code
` + "```" + `
`
	os.WriteFile(mdPath, []byte(content), 0644)

	data, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}

	ann := data["test.go"][5]
	if ann == nil {
		t.Fatal("expected annotation on line 5")
	}
	if !ann.Outdated {
		t.Error("expected annotation to be marked as outdated")
	}
}

func TestParse_NonOutdated(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "REVIEW.md")

	content := `# Code Review

_Started: 2026-03-22_

---

## ` + "`test.go`" + `

#### Line 5

Normal comment
`
	os.WriteFile(mdPath, []byte(content), 0644)

	data, err := parse(mdPath)
	if err != nil {
		t.Fatal(err)
	}

	ann := data["test.go"][5]
	if ann == nil {
		t.Fatal("expected annotation on line 5")
	}
	if ann.Outdated {
		t.Error("expected annotation to not be outdated")
	}
}

func TestParse_NonexistentFile(t *testing.T) {
	data, err := parse("/nonexistent/REVIEW.md")
	if err != nil {
		t.Fatal("expected no error for nonexistent file")
	}
	if len(data) != 0 {
		t.Errorf("expected empty data, got %v", data)
	}
}
