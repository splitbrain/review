package filetree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuildTreeFromPaths(t *testing.T) {
	t.Run("flat files", func(t *testing.T) {
		paths := []string{"a.go", "b.go", "c.txt"}
		tree := buildTreeFromPaths(paths)

		if len(tree) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(tree))
		}
		for _, e := range tree {
			if e.IsDir {
				t.Errorf("expected file, got dir: %s", e.Name)
			}
		}
		// Should be sorted alphabetically
		if tree[0].Name != "a.go" || tree[1].Name != "b.go" || tree[2].Name != "c.txt" {
			t.Errorf("unexpected order: %s, %s, %s", tree[0].Name, tree[1].Name, tree[2].Name)
		}
	})

	t.Run("nested directories", func(t *testing.T) {
		paths := []string{
			"src/main.go",
			"src/util/helpers.go",
			"README.md",
		}
		tree := buildTreeFromPaths(paths)

		// Should have: src/ dir, README.md file. Dirs first.
		if len(tree) != 2 {
			t.Fatalf("expected 2 top-level entries, got %d", len(tree))
		}
		if !tree[0].IsDir || tree[0].Name != "src" {
			t.Errorf("expected first entry to be dir 'src', got %s (isDir=%v)", tree[0].Name, tree[0].IsDir)
		}
		if tree[1].IsDir || tree[1].Name != "README.md" {
			t.Errorf("expected second entry to be file 'README.md', got %s", tree[1].Name)
		}

		// Check nested structure
		src := tree[0]
		if len(src.Children) != 2 {
			t.Fatalf("expected 2 children in src/, got %d", len(src.Children))
		}
		// util/ dir first, then main.go
		if !src.Children[0].IsDir || src.Children[0].Name != "util" {
			t.Errorf("expected first child to be dir 'util', got %s", src.Children[0].Name)
		}
		if src.Children[1].IsDir || src.Children[1].Name != "main.go" {
			t.Errorf("expected second child to be file 'main.go', got %s", src.Children[1].Name)
		}
	})

	t.Run("deep nesting", func(t *testing.T) {
		paths := []string{"a/b/c/d.txt"}
		tree := buildTreeFromPaths(paths)

		if len(tree) != 1 || tree[0].Name != "a" {
			t.Fatalf("expected single dir 'a', got %v", tree)
		}
		b := tree[0].Children[0]
		if b.Name != "b" || !b.IsDir {
			t.Fatalf("expected dir 'b', got %s", b.Name)
		}
		c := b.Children[0]
		if c.Name != "c" || !c.IsDir {
			t.Fatalf("expected dir 'c', got %s", c.Name)
		}
		d := c.Children[0]
		if d.Name != "d.txt" || d.IsDir {
			t.Fatalf("expected file 'd.txt', got %s", d.Name)
		}
		if d.Path != "a/b/c/d.txt" {
			t.Errorf("expected path 'a/b/c/d.txt', got %s", d.Path)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		tree := buildTreeFromPaths(nil)
		if len(tree) != 0 {
			t.Errorf("expected empty tree, got %d entries", len(tree))
		}
	})

	t.Run("dirs sorted before files", func(t *testing.T) {
		paths := []string{
			"zebra.txt",
			"alpha/file.go",
			"beta.txt",
		}
		tree := buildTreeFromPaths(paths)

		if len(tree) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(tree))
		}
		// alpha/ dir should come first
		if !tree[0].IsDir || tree[0].Name != "alpha" {
			t.Errorf("expected dir 'alpha' first, got %s (isDir=%v)", tree[0].Name, tree[0].IsDir)
		}
		// Then files alphabetically
		if tree[1].Name != "beta.txt" {
			t.Errorf("expected 'beta.txt' second, got %s", tree[1].Name)
		}
		if tree[2].Name != "zebra.txt" {
			t.Errorf("expected 'zebra.txt' third, got %s", tree[2].Name)
		}
	})

	t.Run("multiple files same directory", func(t *testing.T) {
		paths := []string{
			"pkg/a.go",
			"pkg/b.go",
			"pkg/c.go",
		}
		tree := buildTreeFromPaths(paths)

		if len(tree) != 1 || tree[0].Name != "pkg" {
			t.Fatalf("expected single dir 'pkg', got %v", tree)
		}
		if len(tree[0].Children) != 3 {
			t.Errorf("expected 3 children, got %d", len(tree[0].Children))
		}
	})
}

func TestWalkDirHiddenEntries(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		".env":                     "SECRET=1",
		".github/workflows/ci.yml": "on: push",
		".git/config":              "[core]",
		"node_modules/dep.js":      "module.exports = {}",
		"main.go":                  "package main",
	})

	tree, err := walkDir(root, "")
	if err != nil {
		t.Fatalf("walkDir failed: %v", err)
	}

	names := entryPaths(tree)
	for _, want := range []string{".env", ".github", ".github/workflows/ci.yml", "main.go"} {
		if !names[want] {
			t.Errorf("expected %q in tree, got %v", want, names)
		}
	}
	for _, unwanted := range []string{".git", ".git/config", "node_modules"} {
		if names[unwanted] {
			t.Errorf("expected %q to be excluded, got %v", unwanted, names)
		}
	}
}

func TestWalkGitRepoHiddenEntries(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		".gitignore":  "ignored.txt\n",
		".env":        "SECRET=1",
		"ignored.txt": "nope",
		"main.go":     "package main",
	})
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git init failed: %v: %s", err, out)
	}

	tree, err := Walk(root)
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	names := entryPaths(tree)
	for _, want := range []string{".gitignore", ".env", "main.go"} {
		if !names[want] {
			t.Errorf("expected %q in tree, got %v", want, names)
		}
	}
	for _, unwanted := range []string{".git", "ignored.txt"} {
		if names[unwanted] {
			t.Errorf("expected %q to be excluded, got %v", unwanted, names)
		}
	}
}

// writeFiles creates the given files (relative path to content) below root,
// including any parent directories.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// entryPaths flattens a tree into a set of all entry paths it contains.
func entryPaths(entries []*Entry) map[string]bool {
	paths := make(map[string]bool)
	var walk func([]*Entry)
	walk = func(list []*Entry) {
		for _, e := range list {
			paths[e.Path] = true
			walk(e.Children)
		}
	}
	walk(entries)
	return paths
}

// TestWalkKeepsNestedReviewFile verifies that only the review's own file at
// the root is hidden, not a REVIEW.md that belongs to the project.
func TestWalkKeepsNestedReviewFile(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"REVIEW.md":      "our own annotations",
		"docs/REVIEW.md": "a document of the project",
		"main.go":        "package main",
	})

	tree, err := walkDir(root, "")
	if err != nil {
		t.Fatalf("walkDir failed: %v", err)
	}

	names := entryPaths(tree)
	if names["REVIEW.md"] {
		t.Error("expected the root REVIEW.md to be excluded")
	}
	if !names["docs/REVIEW.md"] {
		t.Errorf("expected docs/REVIEW.md in tree, got %v", names)
	}
}

// TestWalkKeepsNestedHistoryDir verifies that only the history directory at the
// root is hidden, not one that belongs to the project.
func TestWalkKeepsNestedHistoryDir(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		".review-history/REVIEW-2026-10-08-153000.md": "a retired review of ours",
		"docs/.review-history/notes.md":               "a note of the project",
		"main.go":                                     "package main",
	})

	tree, err := walkDir(root, "")
	if err != nil {
		t.Fatalf("walkDir failed: %v", err)
	}

	names := entryPaths(tree)
	if names[".review-history"] {
		t.Error("expected the root history directory to be excluded")
	}
	if !names["docs/.review-history"] {
		t.Errorf("expected docs/.review-history in tree, got %v", names)
	}
}

// TestInIgnoredDir verifies that the directories left out of the tree are
// recognised anywhere along a path, but not in a file's own name.
func TestInIgnoredDir(t *testing.T) {
	ignored := []string{
		"vendor/lib.go",
		"node_modules/pkg/index.js",
		"frontend/vendor/pico.css",
		"a/b/.git/config",
		"dist/app.js",
	}
	for _, path := range ignored {
		if !inIgnoredDir(path) {
			t.Errorf("%q should be ignored", path)
		}
	}

	kept := []string{
		"main.go",
		"src/build.go",
		"vendor.go",
		"pkg/dist.txt",
		"a/vendoring/x.go",
	}
	for _, path := range kept {
		if inIgnoredDir(path) {
			t.Errorf("%q should be kept", path)
		}
	}
}
