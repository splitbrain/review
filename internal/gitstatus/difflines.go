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

// DiffDeletion represents a block of lines deleted between two lines in the new file.
type DiffDeletion struct {
	AfterLine int `json:"afterLine"` // deletion sits after this line (0 = top of file)
	Count     int `json:"count"`     // number of lines deleted
	HunkIndex int `json:"hunkIndex"` // index into DiffHunks for tooltip
}

// DiffHunk represents a single diff hunk with its affected line range and raw diff text.
type DiffHunk struct {
	StartLine int    `json:"startLine"` // first new-file line in hunk
	EndLine   int    `json:"endLine"`   // last new-file line in hunk
	Diff      string `json:"diff"`      // raw diff lines (- and + lines)
}

// DiffDeletions returns deletion markers for a file by parsing git diff.
// Each deletion indicates where lines were removed (between which new-file lines).
func DiffDeletions(dir, filePath string, hunks []DiffHunk) []DiffDeletion {
	if len(hunks) == 0 {
		return nil
	}

	status := fileStatus(dir, filePath)
	if status == "" || status == StatusUntracked || status == StatusAdded {
		return nil
	}

	// Parse the unified=0 diff to find pure deletion hunks
	cmd := exec.Command("git", "diff", "HEAD", "--unified=0", "--no-color", "--", filePath)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	var deletions []DiffDeletion
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		m := hunkRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		oldCount := 1
		newCount := 1
		newStart, _ := strconv.Atoi(m[3])
		if m[2] != "" {
			oldCount, _ = strconv.Atoi(m[2])
		}
		if m[4] != "" {
			newCount, _ = strconv.Atoi(m[4])
		}
		// Pure deletion: lines removed, nothing added
		if newCount == 0 && oldCount > 0 {
			// Find the matching hunk index (the hunk whose diff contains these deleted lines)
			// newStart is the line AFTER which the deletion occurred
			hunkIdx := -1
			for i, h := range hunks {
				if h.StartLine == newStart && h.EndLine == newStart-1 {
					hunkIdx = i
					break
				}
			}
			// If no exact match, find by proximity
			if hunkIdx == -1 {
				for i, h := range hunks {
					if h.StartLine <= newStart && h.EndLine >= newStart-1 {
						hunkIdx = i
						break
					}
				}
			}
			deletions = append(deletions, DiffDeletion{
				AfterLine: newStart,
				Count:     oldCount,
				HunkIndex: hunkIdx,
			})
		}
	}

	return deletions
}

// DiffHunksForFile returns parsed diff hunks for the given file.
// Returns nil if the file has no changes or is not in a git repo.
func DiffHunksForFile(dir, filePath string) []DiffHunk {
	status := fileStatus(dir, filePath)
	if status == "" || status == StatusUntracked || status == StatusAdded {
		return nil
	}

	cmd := exec.Command("git", "diff", "HEAD", "--unified=3", "--no-color", "--", filePath)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	return parseHunksWithDiff(out)
}

// parseHunksWithDiff extracts hunks with their raw diff text.
func parseHunksWithDiff(diffOutput []byte) []DiffHunk {
	var hunks []DiffHunk
	scanner := bufio.NewScanner(bytes.NewReader(diffOutput))
	var current *DiffHunk
	var newLine int

	for scanner.Scan() {
		line := scanner.Text()

		if m := hunkRe.FindStringSubmatch(line); m != nil {
			if current != nil {
				hunks = append(hunks, *current)
			}
			newStart, _ := strconv.Atoi(m[3])
			newCount := 1
			if m[4] != "" {
				newCount, _ = strconv.Atoi(m[4])
			}
			current = &DiffHunk{
				StartLine: newStart,
				EndLine:   newStart + newCount - 1,
			}
			newLine = newStart
			continue
		}

		if current == nil {
			continue
		}

		if len(line) == 0 {
			current.Diff += "\n"
			continue
		}

		switch line[0] {
		case '-':
			current.Diff += line + "\n"
		case '+':
			current.Diff += line + "\n"
			newLine++
		case ' ':
			current.Diff += line + "\n"
			newLine++
		default:
			// End of hunk
			if current != nil {
				hunks = append(hunks, *current)
				current = nil
			}
		}
	}

	if current != nil {
		hunks = append(hunks, *current)
	}

	return hunks
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
