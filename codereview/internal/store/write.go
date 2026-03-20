package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2/lexers"
)

// serialize converts the annotation map to a markdown string.
func serialize(data map[string]map[int]string, srcRoot string) string {
	var b strings.Builder

	b.WriteString("# Code Review\n\n")
	b.WriteString(fmt.Sprintf("_Started: %s_\n", time.Now().Format("2006-01-02")))

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
			comment := lines[lineNum]
			b.WriteString(fmt.Sprintf("\n#### Line %d\n\n", lineNum))
			b.WriteString(comment)
			b.WriteString("\n")

			// Try to read context lines from source
			snippet := readContext(srcRoot, filePath, lineNum, 3)
			if snippet != "" {
				lang := detectLang(filePath)
				b.WriteString(fmt.Sprintf("\n```%s\n", lang))
				b.WriteString(snippet)
				b.WriteString("```\n")
			}
		}
	}

	return b.String()
}

// readContext reads ±context lines around lineNum from the source file.
func readContext(srcRoot, relPath string, lineNum, context int) string {
	absPath := filepath.Join(srcRoot, relPath)
	f, err := os.Open(absPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	start := lineNum - context
	if start < 1 {
		start = 1
	}
	end := lineNum + context

	scanner := bufio.NewScanner(f)
	var b strings.Builder
	current := 0
	for scanner.Scan() {
		current++
		if current < start {
			continue
		}
		if current > end {
			break
		}
		b.WriteString(fmt.Sprintf("%d: %s\n", current, scanner.Text()))
	}

	return b.String()
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
