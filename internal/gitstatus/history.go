package gitstatus

import (
	"os/exec"
	"strings"
)

// Commit is one entry of the linear (first-parent) history.
type Commit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Parent  string `json:"parent"` // first parent, empty for a root commit
	Date    string `json:"date"`
}

// emptyTree is git's canonical hash for the empty tree. It is used as the
// "parent" of a root commit so the first commit can still be diffed.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// History lists commits from base (exclusive) to HEAD, newest first, following
// the first parent so the walk is linear. With no base it lists from the root.
func History(dir string, base Base) ([]Commit, error) {
	rev := "HEAD"
	if base.Commit != "" {
		rev = base.Commit + "..HEAD"
	}
	// %H hash, %h short, %P parents(space separated), %cI date, %s subject.
	cmd := exec.Command("git", "log", "--no-color", "--first-parent",
		"--pretty=format:%H%x1f%h%x1f%P%x1f%cI%x1f%s", rev)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var commits []Commit
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\x1f", 5)
		if len(f) < 5 {
			continue
		}
		parent := ""
		if ps := strings.Fields(f[2]); len(ps) > 0 {
			parent = ps[0]
		}
		commits = append(commits, Commit{
			Hash:    f[0],
			Short:   f[1],
			Parent:  parent,
			Date:    f[3],
			Subject: f[4],
		})
	}
	return commits, nil
}

// ParentRev returns the revision a commit is diffed against: its first parent,
// or the empty tree for a root commit.
func (c Commit) ParentRev() string {
	if c.Parent == "" {
		return emptyTree
	}
	return c.Parent
}

// ParentOf resolves the first parent of a revision server-side, for callers
// that only have the revision itself. Falls back to the empty tree for a root
// commit or an unresolvable revision.
func ParentOf(dir, rev string) string {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", rev+"^")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return emptyTree
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return emptyTree
}

// TreeAt lists the file paths tracked at a revision, newest structure, using
// git ls-tree. It gives the virtual tree at a commit without a checkout.
func TreeAt(dir, rev string) ([]string, error) {
	cmd := exec.Command("git", "ls-tree", "-r", "--name-only", "-z", rev)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// Show returns a file's content at a given revision. An error means the file
// does not exist at that revision (or the revision is unknown).
func Show(dir, rev, filePath string) ([]byte, error) {
	cmd := exec.Command("git", "show", rev+":"+filePath)
	cmd.Dir = dir
	return cmd.Output()
}

// StatusBetween reports how files differ between two revisions, covering the
// files a commit touched. Untracked files are not part of a historical range.
func StatusBetween(dir, fromRev, toRev string) FileStatuses {
	result := make(FileStatuses)

	cmd := exec.Command("git", "-c", "core.quotepath=false", "diff",
		"--name-status", fromRev, toRev)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return result
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || fields[0] == "" {
			continue
		}
		path := fields[len(fields)-1]
		if status := classifyDiffStatus(fields[0][0]); status != StatusNone {
			result[path] = status
		}
	}
	return result
}
