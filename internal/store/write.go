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

// stampFormat is the layout of the review's creation and modification times,
// in UTC so that a review carries a reference frame of its own.
const stampFormat = "2006-01-02T15:04:05Z"

// ReviewInfo records the context a review was made in: the point in history it
// was taken from, and when it was begun and last written.
type ReviewInfo struct {
	Base     string // point in history the review was taken from, empty outside a git repository
	Created  string // UTC time the review was begun
	Modified string // UTC time the review was last written
}

// maxLineLength bounds a single line when reading source or review files, so
// that a generated or minified file is still read to the end.
const maxLineLength = 1 << 20

// serialize converts the annotation map to a markdown string. The context
// blocks are written from the context stored with each annotation, which is
// the code as it looked when the annotation was last in sync with the source.
func serialize(data map[string]map[int]*Annotation, started string, info ReviewInfo) string {
	var b strings.Builder

	b.WriteString("# Code Review\n\n")
	if info.Base != "" {
		b.WriteString(fmt.Sprintf("_Base: %s_\n", info.Base))
	}
	b.WriteString(fmt.Sprintf("_Started: %s_\n", started))
	if info.Created != "" {
		b.WriteString(fmt.Sprintf("_Created: %s_\n", info.Created))
	}
	if info.Modified != "" {
		b.WriteString(fmt.Sprintf("_Modified: %s_\n", info.Modified))
	}

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
			if ann.Outdated {
				b.WriteString(fmt.Sprintf("\n#### Line %d (outdated)\n\n", lineNum))
			} else {
				b.WriteString(fmt.Sprintf("\n#### Line %d\n\n", lineNum))
			}
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
	if lineNum < 1 || lineNum > len(lines) {
		return nil, 0
	}
	start := lineNum - radius
	if start < 1 {
		start = 1
	}
	end := lineNum + radius
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
