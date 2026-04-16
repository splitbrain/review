package gitstatus

import (
	"bufio"
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
)

// Status represents the git status of a file.
type Status string

const (
	StatusNone      Status = ""
	StatusModified  Status = "modified"  // changed in working tree
	StatusStaged    Status = "staged"    // staged for commit
	StatusUntracked Status = "untracked" // not tracked by git
	StatusAdded     Status = "added"     // new file staged
	StatusDeleted   Status = "deleted"   // deleted
	StatusConflict  Status = "conflict"  // merge conflict
)

// FileStatuses maps relative file paths to their git status.
type FileStatuses map[string]Status

// Get returns the git status for all files in the given directory.
// Returns nil if the directory is not a git repository.
func Get(dir string) FileStatuses {
	// Check if this is a git repo
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return nil
	}

	// Get porcelain status
	cmd = exec.Command("git", "status", "--porcelain", "-unormal")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	result := make(FileStatuses)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 4 {
			continue
		}

		x := line[0] // index (staging area) status
		y := line[1] // working tree status
		path := strings.TrimSpace(line[3:])

		// Handle renames: "R  old -> new"
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}

		// Make path relative and clean
		path = filepath.Clean(path)

		status := classifyStatus(x, y)
		if status != StatusNone {
			result[path] = status
		}
	}

	return result
}

func classifyStatus(x, y byte) Status {
	// Untracked
	if x == '?' && y == '?' {
		return StatusUntracked
	}
	// Conflicts
	if x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D') {
		return StatusConflict
	}
	// Staged changes take priority display
	if x == 'A' {
		return StatusAdded
	}
	if x == 'D' {
		return StatusDeleted
	}
	if x == 'M' || x == 'R' || x == 'C' {
		// If also modified in working tree, show as modified (more urgent)
		if y == 'M' || y == 'D' {
			return StatusModified
		}
		return StatusStaged
	}
	// Working tree changes
	if y == 'M' {
		return StatusModified
	}
	if y == 'D' {
		return StatusDeleted
	}
	return StatusNone
}

// DirStatus computes an aggregate status for a directory path.
// It returns the "most important" status of any file under that directory.
func (fs FileStatuses) DirStatus(dirPath string) Status {
	if fs == nil {
		return StatusNone
	}
	prefix := dirPath + "/"
	best := StatusNone
	for path, status := range fs {
		if strings.HasPrefix(path, prefix) || path == dirPath {
			if statusPriority(status) > statusPriority(best) {
				best = status
			}
		}
	}
	return best
}

func statusPriority(s Status) int {
	switch s {
	case StatusConflict:
		return 5
	case StatusModified:
		return 4
	case StatusUntracked:
		return 3
	case StatusAdded:
		return 2
	case StatusStaged:
		return 1
	default:
		return 0
	}
}
