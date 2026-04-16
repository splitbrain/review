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

// FileDiffInfo contains all diff information for a single file, computed in one pass.
type FileDiffInfo struct {
	Lines     map[int]LineChange
	Hunks     []DiffHunk
	Deletions []DiffDeletion
}

// GetFileDiff returns all diff information for a file in a single call,
// minimizing the number of git subprocess spawns.
func GetFileDiff(dir, filePath string) *FileDiffInfo {
	status := fileStatus(dir, filePath)
	if status == "" {
		return &FileDiffInfo{}
	}

	if status == StatusUntracked || status == StatusAdded {
		return &FileDiffInfo{Lines: allLinesAdded(dir, filePath)}
	}

	// Run both diffs concurrently
	type diffResult struct {
		out []byte
		err error
	}
	ch0 := make(chan diffResult, 1)
	ch3 := make(chan diffResult, 1)

	go func() {
		cmd := exec.Command("git", "diff", "HEAD", "--unified=0", "--no-color", "--", filePath)
		cmd.Dir = dir
		out, err := cmd.Output()
		ch0 <- diffResult{out, err}
	}()
	go func() {
		cmd := exec.Command("git", "diff", "HEAD", "--unified=3", "--no-color", "--", filePath)
		cmd.Dir = dir
		out, err := cmd.Output()
		ch3 <- diffResult{out, err}
	}()

	r0 := <-ch0
	r3 := <-ch3

	info := &FileDiffInfo{}

	if r0.err == nil && len(r0.out) > 0 {
		info.Lines = parseDiffHunks(r0.out)
	}

	if r3.err == nil && len(r3.out) > 0 {
		info.Hunks = parseHunksWithDiff(r3.out)
	}

	if len(info.Hunks) > 0 && r0.err == nil && len(r0.out) > 0 {
		info.Deletions = parseDeletionsFromDiff(r0.out, info.Hunks)
	}

	return info
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

// parseDeletionsFromDiff extracts deletion markers from unified=0 diff output.
func parseDeletionsFromDiff(diffOutput []byte, hunks []DiffHunk) []DiffDeletion {
	var deletions []DiffDeletion
	scanner := bufio.NewScanner(bytes.NewReader(diffOutput))
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
			hunkIdx := -1
			for i, h := range hunks {
				if h.StartLine == newStart && h.EndLine == newStart-1 {
					hunkIdx = i
					break
				}
			}
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
