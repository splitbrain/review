package store

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	fileHeaderRe = regexp.MustCompile("^## `(.+)`$")
	lineHeaderRe = regexp.MustCompile(`^#### Line (\d+)(.*)$`)
	contextLineRe = regexp.MustCompile(`^(\d+): (.*)$`)
)

// parse reads a REVIEW.md file and returns the annotation map.
func parse(path string) (map[string]map[int]*Annotation, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]map[int]*Annotation), nil
		}
		return nil, err
	}
	defer f.Close()

	data := make(map[string]map[int]*Annotation)

	type state int
	const (
		idle state = iota
		inFile
		inComment
		inFence
	)

	var (
		st          state
		currentFile string
		currentLine int
		outdated    bool
		commentBuf  strings.Builder
		contextLines []string
		contextFrom  int
	)

	saveAnnotation := func() {
		if commentBuf.Len() > 0 {
			a := &Annotation{
				Comment:     strings.TrimSpace(commentBuf.String()),
				Context:     contextLines,
				ContextFrom: contextFrom,
				Outdated:    outdated,
			}
			data[currentFile][currentLine] = a
		}
		commentBuf.Reset()
		contextLines = nil
		contextFrom = 0
		outdated = false
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		switch st {
		case idle:
			if m := fileHeaderRe.FindStringSubmatch(line); m != nil {
				currentFile = m[1]
				if data[currentFile] == nil {
					data[currentFile] = make(map[int]*Annotation)
				}
				st = inFile
			}

		case inFile:
			if m := lineHeaderRe.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[1])
				currentLine = n
				outdated = strings.Contains(m[2], "outdated")
				commentBuf.Reset()
				contextLines = nil
				contextFrom = 0
				st = inComment
			} else if strings.TrimSpace(line) == "---" {
				st = idle
			} else if m := fileHeaderRe.FindStringSubmatch(line); m != nil {
				currentFile = m[1]
				if data[currentFile] == nil {
					data[currentFile] = make(map[int]*Annotation)
				}
			}

		case inComment:
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				if commentBuf.Len() > 0 {
					commentBuf.WriteString("\n")
				}
				continue
			}
			if strings.HasPrefix(trimmed, "```") {
				// If we have collected comment text, this is the code fence — capture it
				if commentBuf.Len() > 0 {
					st = inFence
				}
				continue
			}
			// Check if we hit a new line header before any fence
			if m := lineHeaderRe.FindStringSubmatch(line); m != nil {
				saveAnnotation()
				n, _ := strconv.Atoi(m[1])
				currentLine = n
				outdated = strings.Contains(m[2], "outdated")
				continue
			}
			// Check if we hit a new file header
			if m := fileHeaderRe.FindStringSubmatch(line); m != nil {
				saveAnnotation()
				currentFile = m[1]
				if data[currentFile] == nil {
					data[currentFile] = make(map[int]*Annotation)
				}
				st = inFile
				continue
			}
			if trimmed == "---" {
				saveAnnotation()
				st = idle
				continue
			}
			if commentBuf.Len() > 0 {
				commentBuf.WriteString("\n")
			}
			commentBuf.WriteString(line)

		case inFence:
			trimmed := strings.TrimSpace(line)
			if trimmed == "```" {
				// End of fence — save annotation with context
				saveAnnotation()
				st = inFile
				continue
			}
			// Parse context line: "N: content"
			if m := contextLineRe.FindStringSubmatch(line); m != nil {
				lineNum, _ := strconv.Atoi(m[1])
				if contextFrom == 0 {
					contextFrom = lineNum
				}
				contextLines = append(contextLines, m[2])
			}
		}
	}

	// Handle trailing comment without a fence
	if (st == inComment || st == inFence) && commentBuf.Len() > 0 {
		saveAnnotation()
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return data, nil
}
