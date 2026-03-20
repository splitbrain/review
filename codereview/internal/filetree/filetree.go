package filetree

import (
	"os"
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
func Walk(root string) ([]*Entry, error) {
	return walkDir(root, "")
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

		// Skip review.md
		if name == "review.md" {
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
