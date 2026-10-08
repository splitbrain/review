package gitstatus

import (
	"bufio"
	"bytes"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// LineChange represents the type of change for a line.
type LineChange string

const (
	LineAdded    LineChange = "added"
	LineModified LineChange = "modified"
)

// diffContext is the number of unchanged lines git includes around a change.
// They are shown in the hunk tooltip.
const diffContext = 3

// maxDiffLine bounds a single line of diff output, so that a file with very
// long lines is still read completely.
const maxDiffLine = 1 << 20

// hunkRe matches unified diff hunk headers: @@ -old[,count] +new[,count] @@
var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// FileDiffInfo contains all diff information for a single file, computed in one pass.
type FileDiffInfo struct {
	Lines     map[int]LineChange
	Hunks     []DiffHunk
	Deletions []DiffDeletion
}

// DiffDeletion marks a block of lines deleted between two lines of the new
// file. How many were removed is not recorded: the hunk the marker points at
// shows them.
type DiffDeletion struct {
	AfterLine int `json:"afterLine"` // deletion sits after this line (0 = top of file)
	HunkIndex int `json:"hunkIndex"` // index into DiffHunks for tooltip
}

// DiffHunk represents a single diff hunk with its affected line range and raw diff text.
type DiffHunk struct {
	StartLine int    `json:"startLine"` // first new-file line in hunk
	EndLine   int    `json:"endLine"`   // last new-file line in hunk
	Diff      string `json:"diff"`      // raw diff lines (- and + lines)
}

// GetFileDiff returns all diff information for a file. With a base commit set,
// the file is diffed against that commit instead of against HEAD. The file's
// content is used to mark up a file git does not track yet.
//
// The diff and the check for an untracked file run at the same time, because
// which of the two answers is needed only shows once the diff is in.
func GetFileDiff(dir string, base Base, filePath string, content []byte) *FileDiffInfo {
	type result struct {
		out []byte
		err error
	}
	run := func(args ...string) <-chan result {
		ch := make(chan result, 1)
		go func() {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			out, err := cmd.Output()
			ch <- result{out, err}
		}()
		return ch
	}

	diffCh := run("diff", base.rev(), "--unified="+strconv.Itoa(diffContext), "--no-color", "--", filePath)
	untrackedCh := run("ls-files", "--others", "--exclude-standard", "--", filePath)

	diff := <-diffCh
	untracked := <-untrackedCh

	if diff.err == nil && len(diff.out) > 0 {
		return parseDiff(diff.out)
	}
	if untracked.err == nil && len(untracked.out) > 0 {
		// The file is new to git, so all of it is new
		return &FileDiffInfo{Lines: allLinesAdded(content)}
	}
	return &FileDiffInfo{}
}

// GetFileDiffAt diffs a file between two revisions (e.g. a commit and its
// parent). If the file does not exist at fromRev, every line is marked added.
// Unlike GetFileDiff it always uses the revision range, never the working tree.
func GetFileDiffAt(dir, fromRev, toRev, filePath string, content []byte) *FileDiffInfo {
	cmd := exec.Command("git", "diff", fromRev, toRev,
		"--unified="+strconv.Itoa(diffContext), "--no-color", "--", filePath)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		return parseDiff(out)
	}
	// No diff output: added (absent at fromRev) or unchanged.
	if _, e := Show(dir, fromRev, filePath); e != nil {
		return &FileDiffInfo{Lines: allLinesAdded(content)}
	}
	return &FileDiffInfo{}
}

// parseDiff turns unified diff output for a single file into the line markers,
// hunks and deletion markers the frontend draws.
//
// Added and modified lines are told apart by looking at the diff body rather
// than the hunk header: with context around a change, one hunk can hold both.
// A run of removed lines that is not replaced by added ones becomes a deletion
// marker sitting after the last line that survived.
func parseDiff(out []byte) *FileDiffInfo {
	info := &FileDiffInfo{}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), maxDiffLine)

	var (
		current   *DiffHunk       // hunk being read
		body      strings.Builder // raw diff text of the hunk being read
		newLine   int             // next line number in the new file
		removed   int             // removed lines seen since the last unchanged line
		replacing bool            // the run of added lines replaces removed ones
	)

	// keep records a line as part of the hunk being read.
	keep := func(line string) {
		body.WriteString(line)
		body.WriteByte('\n')
	}

	// endRun closes a run of removed lines, recording a deletion marker for
	// those that nothing was put in place of.
	endRun := func() {
		if removed > 0 {
			after := newLine - 1
			if after < 0 {
				after = 0
			}
			info.Deletions = append(info.Deletions, DiffDeletion{
				AfterLine: after,
				HunkIndex: len(info.Hunks),
			})
			removed = 0
		}
		replacing = false
	}

	// endHunk finishes the hunk being read.
	endHunk := func() {
		if current == nil {
			return
		}
		endRun()
		current.Diff = body.String()
		body.Reset()
		info.Hunks = append(info.Hunks, *current)
		current = nil
	}

	mark := func(change LineChange) {
		if info.Lines == nil {
			info.Lines = make(map[int]LineChange)
		}
		info.Lines[newLine] = change
	}

	for scanner.Scan() {
		line := scanner.Text()

		if m := hunkRe.FindStringSubmatch(line); m != nil {
			endHunk()
			newStart, _ := strconv.Atoi(m[3])
			newCount := 1
			if m[4] != "" {
				newCount, _ = strconv.Atoi(m[4])
			}
			current = &DiffHunk{StartLine: newStart, EndLine: newStart + newCount - 1}
			newLine = newStart
			continue
		}

		// Everything before the first hunk header is the file header
		if current == nil {
			continue
		}

		if line == "" {
			// An unchanged empty line is written without its leading space
			endRun()
			keep("")
			newLine++
			continue
		}

		switch line[0] {
		case ' ':
			endRun()
			keep(line)
			newLine++
		case '-':
			if replacing {
				endRun() // a new run of changes starts
			}
			removed++
			keep(line)
		case '+':
			if removed > 0 {
				replacing = true
				removed = 0 // replaced, not deleted
			}
			if replacing {
				mark(LineModified)
			} else {
				mark(LineAdded)
			}
			keep(line)
			newLine++
		case '\\':
			// "\ No newline at end of file"
			keep(line)
		default:
			endHunk()
		}
	}
	endHunk()

	return info
}

// allLinesAdded marks every line of the given content as "added".
func allLinesAdded(content []byte) map[int]LineChange {
	n := bytes.Count(content, []byte{'\n'})
	if len(content) > 0 && content[len(content)-1] != '\n' {
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
