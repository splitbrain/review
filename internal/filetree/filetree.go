package filetree

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Entry represents a file or directory in the tree.
type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	// Children is omitted for files, which make up the bulk of a tree.
	Children []*Entry `json:"children,omitempty"`
}

// reviewFile is the annotation file the tool writes at the root of the
// reviewed directory. It is not part of what is under review.
const reviewFile = "REVIEW.md"

// ignoredDirs are directory names never included in the tree, whether it is
// built from git or by walking the filesystem. Version control metadata
// directories are listed here because their contents are not source code the
// user would review.
var ignoredDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
	".git":         true,
	".svn":         true,
	".hg":          true,
}

var ignoredExts = map[string]bool{
	".exe":   true,
	".bin":   true,
	".so":    true,
	".dylib": true,
	".png":   true,
	".jpg":   true,
	".gif":   true,
	".pdf":   true,
	".zip":   true,
	".tar":   true,
	".gz":    true,
}

// Walk builds a tree of entries rooted at root.
// All paths in the returned entries are relative to root.
// In a git repository, uses git ls-files for performance on large repos.
func Walk(root string) ([]*Entry, error) {
	// Try git ls-files first (much faster on large repos)
	if files, err := gitLsFiles(root); err == nil {
		return buildTreeFromPaths(files), nil
	}

	// Fallback to filesystem walk
	return walkDir(root, "")
}

// gitLsFiles returns all tracked and untracked-but-not-ignored files using git.
// The -z flag emits NUL-separated, unquoted paths, so filenames containing
// non-ASCII (e.g. umlauts) or other special characters are returned verbatim
// instead of git's default C-style quoted form.
func gitLsFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path == "" || path == reviewFile {
			continue
		}
		// Skip binary extensions
		ext := strings.ToLower(filepath.Ext(path))
		if ignoredExts[ext] {
			continue
		}
		if inIgnoredDir(path) {
			continue
		}
		files = append(files, path)
	}
	return files, nil
}

// inIgnoredDir reports whether a file sits below one of the directories that are
// left out of the tree. Paths come from git and are separated by slashes on
// every platform.
func inIgnoredDir(path string) bool {
	parts := strings.Split(path, "/")
	for _, dir := range parts[:len(parts)-1] {
		if ignoredDirs[dir] {
			return true
		}
	}
	return false
}

type dirNode struct {
	entry    *Entry
	children map[string]*dirNode
}

// FromPaths builds a tree from a flat list of repository-relative paths, using
// the same filtering (ignored dirs/extensions, REVIEW.md) as Walk. It lets
// callers feed a virtual tree (e.g. the file list at a historical revision)
// without a filesystem walk.
func FromPaths(paths []string) []*Entry {
	var files []string
	for _, path := range paths {
		if path == "" || path == reviewFile {
			continue
		}
		if ignoredExts[strings.ToLower(filepath.Ext(path))] {
			continue
		}
		if inIgnoredDir(path) {
			continue
		}
		files = append(files, path)
	}
	return buildTreeFromPaths(files)
}

func buildTreeFromPaths(paths []string) []*Entry {
	root := &dirNode{children: make(map[string]*dirNode)}

	for _, path := range paths {
		parts := strings.Split(path, "/")
		current := root

		// Create directory entries for each parent
		for i, part := range parts {
			if i == len(parts)-1 {
				// File entry
				if current.children[part] == nil {
					current.children[part] = &dirNode{
						entry: &Entry{
							Name:  part,
							Path:  path,
							IsDir: false,
						},
					}
				}
			} else {
				// Directory entry
				if current.children[part] == nil {
					dirPath := strings.Join(parts[:i+1], "/")
					current.children[part] = &dirNode{
						entry: &Entry{
							Name:  part,
							Path:  dirPath,
							IsDir: true,
						},
						children: make(map[string]*dirNode),
					}
				}
				current = current.children[part]
			}
		}
	}

	return collectEntries(root)
}

func collectEntries(node *dirNode) []*Entry {
	var result []*Entry
	for _, child := range node.children {
		if child.entry.IsDir {
			child.entry.Children = collectEntries(child)
			if len(child.entry.Children) == 0 {
				continue
			}
		}
		result = append(result, child.entry)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].IsDir != result[j].IsDir {
			return result[i].IsDir
		}
		return result[i].Name < result[j].Name
	})

	return result
}

func walkDir(absDir, relDir string) ([]*Entry, error) {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}

	var result []*Entry
	for _, e := range entries {
		name := e.Name()

		// Skip the review's own file, but only the one at the root
		if relDir == "" && name == reviewFile {
			continue
		}

		rel := name
		if relDir != "" {
			rel = relDir + "/" + name
		}

		if e.IsDir() {
			if ignoredDirs[name] {
				continue
			}
			children, err := walkDir(filepath.Join(absDir, name), rel)
			if err != nil {
				continue // skip unreadable dirs
			}
			if len(children) == 0 {
				continue // skip empty dirs
			}
			result = append(result, &Entry{
				Name:     name,
				Path:     rel,
				IsDir:    true,
				Children: children,
			})
		} else {
			ext := strings.ToLower(filepath.Ext(name))
			if ignoredExts[ext] {
				continue
			}
			result = append(result, &Entry{
				Name:  name,
				Path:  rel,
				IsDir: false,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		// Directories first, then alphabetical
		if result[i].IsDir != result[j].IsDir {
			return result[i].IsDir
		}
		return result[i].Name < result[j].Name
	})

	return result, nil
}
