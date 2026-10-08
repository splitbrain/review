package store

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"
)

// startedFormat is the layout of the review's start date in REVIEW.md.
const startedFormat = "2006-01-02"

// maxLineLength bounds a single line when reading source or review files, so
// that a generated or minified file is still read to the end.
const maxLineLength = 1 << 20

// serialize converts the annotation map to a markdown string. The context
// blocks are written from the context stored with each annotation, which is
// the code as it looked when the annotation was last in sync with the source.
func serialize(data map[string]map[int]*Annotation, started string) string {
	var b strings.Builder

	b.WriteString("# Code Review\n\n")
	b.WriteString(fmt.Sprintf("_Started: %s_\n", started))

	// Sort file paths
	paths := make([]string, 0, len(data))
	for p, lines := range data {
		if len(lines) > 0 {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	for _, filePath := range paths {
		lines := data[filePath]
		if len(lines) == 0 {
			continue
		}

		b.WriteString("\n---\n\n")
		b.WriteString(fmt.Sprintf("## `%s`\n", filePath))

		// Sort line numbers
		lineNums := make([]int, 0, len(lines))
		for n := range lines {
			lineNums = append(lineNums, n)
		}
		sort.Ints(lineNums)

		for _, lineNum := range lineNums {
			ann := lines[lineNum]
			b.WriteString("\n#### " + lineHeader(ann, lineNum) + "\n\n")
			b.WriteString(escapeComment(ann.Comment))

			if len(ann.Context) > 0 {
				b.WriteString(fmt.Sprintf("\n```%s\n", contextFenceInfo(filePath)))
				b.WriteString(formatContext(ann.Context, ann.ContextFrom))
				b.WriteString("```\n")
			}
		}
	}

	return b.String()
}

// lineHeader renders the heading text naming the lines an annotation covers,
// without the heading's own hashes. A comment on a range names its first and
// last line; a single-line one keeps the "Line N" form, so a review file
// written before ranges existed still reads the same.
func lineHeader(ann *Annotation, endLine int) string {
	header := fmt.Sprintf("Line %d", endLine)
	if ann.StartLine > 0 && ann.StartLine != endLine {
		header = fmt.Sprintf("Lines %d-%d", ann.StartLine, endLine)
	}
	if ann.Outdated {
		header += " (outdated)"
	}
	return header
}

// escapeComment writes a comment as the reviewer wrote it. Only a line that
// would be read back as one of the document's own headings is prefixed with a
// backslash, which markdown renders as the plain text that was meant. Lines
// inside the comment's own code fences are left alone, as nothing in them is
// read as structure.
func escapeComment(comment string) string {
	var b strings.Builder
	fence := ""
	for _, line := range strings.Split(comment, "\n") {
		if fence != "" {
			if closesFence(line, fence) {
				fence = ""
			}
		} else if marker := fenceMarker(line); marker != "" {
			fence = marker
		} else if looksStructural(line) {
			b.WriteString(`\`)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// contextFenceInfo is the info string of a context block's fence: the file's
// language, so the block is highlighted wherever the review is rendered,
// followed by the marker that tells it from a code sample in a comment.
// Renderers take the language from the first word and ignore the rest.
func contextFenceInfo(filePath string) string {
	if lang := detectLang(filePath); lang != "" {
		return lang + " " + contextMarker
	}
	return contextMarker
}

// formatContext renders stored context lines with their line numbers, the form
// they take inside the fenced context block.
func formatContext(lines []string, from int) string {
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%d: %s\n", from+i, line)
	}
	return b.String()
}

// contextAround returns the lines surrounding lineNum together with the line
// number the block starts at. Line numbers are 1-based.
func contextAround(lines []string, lineNum, radius int) ([]string, int) {
	return contextAroundRange(lines, lineNum, lineNum, radius)
}

// contextAroundRange returns the lines surrounding the range startLine..endLine
// together with the line number the block starts at. A range wider than the
// context it is stored with keeps at least its own lines, so a long comment is
// recognised by the code it covers rather than only its edges. Line numbers are
// 1-based.
func contextAroundRange(lines []string, startLine, endLine, radius int) ([]string, int) {
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}
	if startLine < 1 || startLine > len(lines) {
		return nil, 0
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	start := startLine - radius
	if start < 1 {
		start = 1
	}
	end := endLine + radius
	if end > len(lines) {
		end = len(lines)
	}
	return append([]string(nil), lines[start-1:end]...), start
}

// readFileLines reads all lines from a source file and returns them (0-indexed).
func readFileLines(srcRoot, relPath string) ([]string, error) {
	return readLines(filepath.Join(srcRoot, relPath))
}

// detectLang returns a language identifier for the code fence.
func detectLang(filename string) string {
	lexer := lexers.Match(filename)
	if lexer == nil {
		return ""
	}
	name := strings.ToLower(lexer.Config().Name)
	// Map common names to short fence tags
	switch name {
	case "go":
		return "go"
	case "python", "python 3":
		return "python"
	case "javascript":
		return "javascript"
	case "typescript":
		return "typescript"
	case "java":
		return "java"
	case "c":
		return "c"
	case "c++":
		return "cpp"
	case "c#":
		return "csharp"
	case "ruby":
		return "ruby"
	case "rust":
		return "rust"
	case "shell", "bash":
		return "bash"
	case "sql":
		return "sql"
	case "html":
		return "html"
	case "css":
		return "css"
	case "json":
		return "json"
	case "yaml":
		return "yaml"
	case "markdown":
		return "markdown"
	default:
		return name
	}
}
