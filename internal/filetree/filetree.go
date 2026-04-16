package filetree

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Entry represents a file or directory in the tree.
type Entry struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	IsDir    bool     `json:"isDir"`
	Children []*Entry `json:"children"`
}

var ignoredDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
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
func gitLsFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		path := scanner.Text()
		if path == "" || path == "REVIEW.md" {
			continue
		}
		// Skip hidden files
		if strings.HasPrefix(filepath.Base(path), ".") {
			continue
		}
		// Skip hidden directories anywhere in path
		skip := false
		for _, part := range strings.Split(path, "/") {
			if strings.HasPrefix(part, ".") {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		// Skip binary extensions
		ext := strings.ToLower(filepath.Ext(path))
		if ignoredExts[ext] {
			continue
		}
		files = append(files, path)
	}
	return files, scanner.Err()
}

type dirNode struct {
	entry    *Entry
	children map[string]*dirNode
}

// buildTreeFromPaths builds a tree structure from a flat list of file paths.
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

		// Skip hidden files/dirs
		if strings.HasPrefix(name, ".") {
			continue
		}

		// Skip REVIEW.md
		if name == "REVIEW.md" {
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
