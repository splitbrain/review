package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRoundTrip_MarkdownInComment ensures comment bodies survive a write/read
// cycle even when they contain markdown that looks like document structure.
func TestRoundTrip_MarkdownInComment(t *testing.T) {
	cases := map[string]string{
		"plain":           "just a comment",
		"code fence":      "use this instead:\n\n```go\nfoo()\n```\n\nsee?",
		"horizontal rule": "first\n\n---\n\nsecond",
		"line heading":    "#### Line 99\n\nnot a real header",
		"file heading":    "## `other.go`\n\nnot a real header",
		"context lines":   "1: foo\n2: bar",
		"blockquote":      "> already quoted\n> twice",
		"indentation":     "look at:\n    indented\n\tand tabbed",
	}

	for name, comment := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\nl4\nl5\n"), 0644)
			mdPath := filepath.Join(dir, "REVIEW.md")

			st, err := Load(mdPath, dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Set("a.go", 3, comment); err != nil {
				t.Fatal(err)
			}

			reloaded, err := Load(mdPath, dir)
			if err != nil {
				t.Fatal(err)
			}
			ann := reloaded.data["a.go"][3]
			if ann == nil {
				t.Fatalf("annotation lost, file was:\n%s", mustRead(t, mdPath))
			}
			if ann.Comment != comment {
				t.Errorf("comment changed:\n got: %q\nwant: %q\nfile was:\n%s",
					ann.Comment, comment, mustRead(t, mdPath))
			}
		})
	}
}

// TestRoundTrip_MultipleAnnotations ensures neighbouring annotations are still
// separated correctly when a comment contains structural markdown.
func TestRoundTrip_MultipleAnnotations(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\nl4\nl5\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("x1\nx2\nx3\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, _ := Load(mdPath, dir)
	st.Set("a.go", 1, "first\n\n```\n#### Line 42\n```")
	st.Set("a.go", 4, "second")
	st.Set("b.go", 2, "third\n---")

	reloaded, _ := Load(mdPath, dir)
	if got := len(reloaded.data); got != 2 {
		t.Fatalf("expected 2 files, got %d: %v", got, reloaded.data)
	}
	if got := len(reloaded.data["a.go"]); got != 2 {
		t.Errorf("expected 2 annotations in a.go, got %d", got)
	}
	if got := reloaded.data["a.go"][4]; got == nil || got.Comment != "second" {
		t.Errorf("second annotation not parsed: %+v", got)
	}
	if got := reloaded.data["b.go"][2]; got == nil || got.Comment != "third\n---" {
		t.Errorf("third annotation not parsed: %+v", got)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestRoundTrip_LongLines ensures files with very long lines, such as minified
// or generated code, can still be annotated and followed.
func TestRoundTrip_LongLines(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 200_000)
	if err := os.WriteFile(filepath.Join(dir, "bundle.js"), []byte("first\n"+long+"\nlast\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set("bundle.js", 2, "this line is huge"); err != nil {
		t.Fatal(err)
	}
	if got := len(st.data["bundle.js"][2].Context); got != 3 {
		t.Errorf("expected 3 context lines, got %d", got)
	}

	reloaded, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	ann := reloaded.data["bundle.js"][2]
	if ann == nil {
		t.Fatal("annotation lost")
	}
	if len(ann.Context) != 3 || ann.Context[1] != long {
		t.Errorf("long context line not read back (%d lines)", len(ann.Context))
	}
}

// TestSerialize_CommentStaysPlainText guards the shape of the review file: a
// comment is written as the reviewer wrote it, so the document reads as prose
// with code samples, not as quoted text.
func TestSerialize_CommentStaysPlainText(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\nl4\nl5\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, err := Load(mdPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	comment := "Prefer a logger:\n\n```go\nlog.Println(\"hi\")\n```\n\nIt keeps output consistent."
	if err := st.Set("a.go", 3, comment); err != nil {
		t.Fatal(err)
	}

	written := mustRead(t, mdPath)
	if !strings.Contains(written, "\n"+comment+"\n") {
		t.Errorf("comment was not written verbatim, file is:\n%s", written)
	}
	if strings.Contains(written, "\n> ") {
		t.Errorf("comment was quoted, file is:\n%s", written)
	}
}

// TestSerialize_EscapesOnlyHeadingLines checks that the escaping needed to keep
// a comment apart from the document is limited to the lines that need it.
func TestSerialize_EscapesOnlyHeadingLines(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, _ := Load(mdPath, dir)
	comment := "See below.\n\n#### Line 99\n\nA sample:\n\n```md\n#### Line 42\n```"
	if err := st.Set("a.go", 2, comment); err != nil {
		t.Fatal(err)
	}

	written := mustRead(t, mdPath)
	if !strings.Contains(written, "\\#### Line 99") {
		t.Errorf("heading line was not escaped, file is:\n%s", written)
	}
	if strings.Contains(written, "\\#### Line 42") {
		t.Errorf("heading inside a code fence should not be escaped, file is:\n%s", written)
	}
	if !strings.Contains(written, "See below.") {
		t.Errorf("ordinary text should be untouched, file is:\n%s", written)
	}

	reloaded, _ := Load(mdPath, dir)
	if got := reloaded.data["a.go"][2]; got == nil || got.Comment != comment {
		t.Errorf("comment did not survive:\n got: %#v\nwant: %q", got, comment)
	}
}

// TestParse_LegacyFileWithoutContext covers a review file written before the
// context was stored, where the comment is plain text and no fence follows.
func TestParse_LegacyFileWithoutContext(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "REVIEW.md")
	content := "# Code Review\n\n_Started: 2020-01-01_\n\n---\n\n## `a.go`\n\n" +
		"#### Line 1\n\nfirst comment\n\n#### Line 5\n\nsecond comment\n\n---\n\n## `b.go`\n\n#### Line 2\n\nthird comment\n"
	os.WriteFile(mdPath, []byte(content), 0644)

	meta, err := parse(mdPath)
	data, started := meta.data, meta.started
	if err != nil {
		t.Fatal(err)
	}
	if started != "2020-01-01" {
		t.Errorf("started: got %q", started)
	}
	want := map[string]map[int]string{
		"a.go": {1: "first comment", 5: "second comment"},
		"b.go": {2: "third comment"},
	}
	for file, lines := range want {
		for line, comment := range lines {
			got := data[file][line]
			if got == nil || got.Comment != comment {
				t.Errorf("%s:%d: got %#v, want %q", file, line, got, comment)
			}
		}
	}
}

// TestSerialize_ContextFenceIsMarked checks that a context block says what it
// is, while keeping the language that has it highlighted when rendered.
func TestSerialize_ContextFenceIsMarked(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\n"), 0644)
	os.WriteFile(filepath.Join(dir, "notes.unknownext"), []byte("x\ny\nz\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	st, _ := Load(mdPath, dir)
	st.Set("a.go", 2, "a note")
	st.Set("notes.unknownext", 2, "another note")

	written := mustRead(t, mdPath)
	if !strings.Contains(written, "```go context\n") {
		t.Errorf("expected a marked go fence, file is:\n%s", written)
	}
	if !strings.Contains(written, "```context\n") {
		t.Errorf("expected a marked fence for the file without a language, file is:\n%s", written)
	}
}

// TestRoundTrip_CommentEndingInNumberedBlock covers the case the marker exists
// for: a comment whose own code sample looks exactly like a context block.
func TestRoundTrip_CommentEndingInNumberedBlock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("l1\nl2\nl3\nl4\nl5\n"), 0644)
	mdPath := filepath.Join(dir, "REVIEW.md")

	comment := "The numbering is off here:\n\n```\n1: first\n2: second\n```"
	st, _ := Load(mdPath, dir)
	if err := st.Set("a.go", 3, comment); err != nil {
		t.Fatal(err)
	}

	reloaded, _ := Load(mdPath, dir)
	ann := reloaded.data["a.go"][3]
	if ann == nil {
		t.Fatalf("annotation lost, file was:\n%s", mustRead(t, mdPath))
	}
	if ann.Comment != comment {
		t.Errorf("comment changed:\n got: %q\nwant: %q", ann.Comment, comment)
	}
	if ann.ContextFrom != 1 || len(ann.Context) != 5 {
		t.Errorf("expected the real context block, got from=%d lines=%v", ann.ContextFrom, ann.Context)
	}
}

// TestParse_UnmarkedContextStillRead keeps review files written before the
// marker readable, so their recorded context is not lost.
func TestParse_UnmarkedContextStillRead(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "REVIEW.md")
	content := "# Code Review\n\n_Started: 2020-01-01_\n\n---\n\n## `a.go`\n\n" +
		"#### Line 5 (outdated)\n\nan older comment\n\n```go\n4: was here\n5: and here\n```\n"
	os.WriteFile(mdPath, []byte(content), 0644)

	meta, err := parse(mdPath)
	data := meta.data
	if err != nil {
		t.Fatal(err)
	}
	ann := data["a.go"][5]
	if ann == nil {
		t.Fatal("expected annotation on line 5")
	}
	if ann.Comment != "an older comment" {
		t.Errorf("comment: got %q", ann.Comment)
	}
	if ann.ContextFrom != 4 || len(ann.Context) != 2 {
		t.Errorf("context: got from=%d lines=%v", ann.ContextFrom, ann.Context)
	}
	if !ann.Outdated {
		t.Error("expected the outdated mark to be kept")
	}
}
