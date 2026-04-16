package filetree

import (
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
