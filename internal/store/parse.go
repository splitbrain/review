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
	lineHeaderRe = regexp.MustCompile(`^#### Line (\d+)$`)
)

// parse reads a review.md file and returns the annotation map.
func parse(path string) (map[string]map[int]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]map[int]string), nil
		}
		return nil, err
	}
	defer f.Close()

	data := make(map[string]map[int]string)

	type state int
	const (
		idle state = iota
		inFile
		inComment
		skipFence
	)

	var (
		st          state
		currentFile string
		currentLine int
		commentBuf  strings.Builder
	)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		switch st {
		case idle:
			if m := fileHeaderRe.FindStringSubmatch(line); m != nil {
				currentFile = m[1]
				if data[currentFile] == nil {
					data[currentFile] = make(map[int]string)
				}
				st = inFile
			}

		case inFile:
			if m := lineHeaderRe.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[1])
				currentLine = n
				commentBuf.Reset()
				st = inComment
			} else if strings.TrimSpace(line) == "---" {
				st = idle
			} else if m := fileHeaderRe.FindStringSubmatch(line); m != nil {
				currentFile = m[1]
				if data[currentFile] == nil {
					data[currentFile] = make(map[int]string)
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
				// If we have collected comment text, this is the code fence — skip it
				if commentBuf.Len() > 0 {
					comment := strings.TrimSpace(commentBuf.String())
					data[currentFile][currentLine] = comment
					st = skipFence
				}
				continue
			}
			// Check if we hit a new line header before any fence
			if m := lineHeaderRe.FindStringSubmatch(line); m != nil {
				// Save current comment
				if commentBuf.Len() > 0 {
					comment := strings.TrimSpace(commentBuf.String())
					data[currentFile][currentLine] = comment
				}
				n, _ := strconv.Atoi(m[1])
				currentLine = n
				commentBuf.Reset()
				continue
			}
			// Check if we hit a new file header
			if m := fileHeaderRe.FindStringSubmatch(line); m != nil {
				if commentBuf.Len() > 0 {
					comment := strings.TrimSpace(commentBuf.String())
					data[currentFile][currentLine] = comment
				}
				currentFile = m[1]
				if data[currentFile] == nil {
					data[currentFile] = make(map[int]string)
				}
				st = inFile
				continue
			}
			if trimmed == "---" {
				if commentBuf.Len() > 0 {
					comment := strings.TrimSpace(commentBuf.String())
					data[currentFile][currentLine] = comment
				}
				st = idle
				continue
			}
			if commentBuf.Len() > 0 {
				commentBuf.WriteString("\n")
			}
			commentBuf.WriteString(line)

		case skipFence:
			if strings.TrimSpace(line) == "```" {
				st = inFile
			}
		}
	}

	// Handle trailing comment without a fence
	if st == inComment && commentBuf.Len() > 0 {
		comment := strings.TrimSpace(commentBuf.String())
		data[currentFile][currentLine] = comment
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return data, nil
}
