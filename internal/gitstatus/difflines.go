package gitstatus

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

// LineChange represents the type of change for a line.
type LineChange string

const (
	LineAdded    LineChange = "added"
	LineModified LineChange = "modified"
)

// hunkRe matches unified diff hunk headers: @@ -old[,count] +new[,count] @@
var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// DiffLines returns a map of line numbers (1-based) to their change type
// for the given file, comparing the working tree against HEAD.
// Returns nil if the file has no changes or is not in a git repo.
func DiffLines(dir, filePath string) map[int]LineChange {
	// First check git status for this file to handle untracked/added files
	status := fileStatus(dir, filePath)
	if status == "" {
		return nil
	}

	// For untracked or newly added files, we can't diff against HEAD.
	// Count lines and mark all as added.
	if status == StatusUntracked || status == StatusAdded {
		return allLinesAdded(dir, filePath)
	}

	// Run git diff HEAD with no context to get precise line ranges
	cmd := exec.Command("git", "diff", "HEAD", "--unified=0", "--no-color", "--", filePath)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	return parseDiffHunks(out)
}

// fileStatus returns the git status for a single file.
func fileStatus(dir, filePath string) Status {
	cmd := exec.Command("git", "status", "--porcelain", "--", filePath)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return StatusNone
	}
	line := string(out)
	if len(line) < 4 {
		return StatusNone
	}
	return classifyStatus(line[0], line[1])
}

// allLinesAdded reads the file and marks every line as "added".
func allLinesAdded(dir, filePath string) map[int]LineChange {
	data, err := os.ReadFile(filepath.Join(dir, filePath))
	if err != nil {
		return nil
	}
	n := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++ // file doesn't end with newline
	}
	if n == 0 {
		return nil
	}
	result := make(map[int]LineChange, n)
	for i := 1; i <= n; i++ {
		result[i] = LineAdded
	}
	return result
}

// parseDiffHunks extracts changed line numbers from unified diff output.
func parseDiffHunks(diffOutput []byte) map[int]LineChange {
	result := make(map[int]LineChange)

	scanner := bufio.NewScanner(bytes.NewReader(diffOutput))
	var inHunk bool
	var newStart, oldCount, newCount int
	var hunkLine int

	for scanner.Scan() {
		line := scanner.Text()

		if m := hunkRe.FindStringSubmatch(line); m != nil {
			// Parse hunk header
			oldCount = 1
			newCount = 1
			newStart, _ = strconv.Atoi(m[3])
			if m[2] != "" {
				oldCount, _ = strconv.Atoi(m[2])
			}
			if m[4] != "" {
				newCount, _ = strconv.Atoi(m[4])
			}

			// Determine change type for this hunk
			changeType := LineModified
			if oldCount == 0 {
				// Pure addition (no old lines removed)
				changeType = LineAdded
			}

			// Mark all new lines in this hunk
			for i := 0; i < newCount; i++ {
				result[newStart+i] = changeType
			}

			inHunk = true
			hunkLine = 0
			continue
		}

		if inHunk {
			if len(line) > 0 && (line[0] == '+' || line[0] == '-' || line[0] == ' ') {
				hunkLine++
			} else {
				inHunk = false
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
