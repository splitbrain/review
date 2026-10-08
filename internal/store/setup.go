package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetupReviewHistory enables review history for the project rooted at rootDir:
// it creates the history directory, writes the note that tells agents what the
// directory holds, and adds the review's own files to the project's .gitignore.
// It returns the paths it created.
//
// Each step here is idempotent.
func SetupReviewHistory(rootDir string) ([]string, error) {
	created := []string{}

	dir := filepath.Join(rootDir, ReviewDir)
	if err := os.Mkdir(dir, 0755); err != nil {
		if !os.IsExist(err) {
			return created, fmt.Errorf("create %s: %w", ReviewDir, err)
		}
	} else {
		created = append(created, dir)
	}

	note := filepath.Join(dir, historyNoteName)
	if _, err := os.Stat(note); os.IsNotExist(err) {
		if err := os.WriteFile(note, []byte(historyNote), 0644); err != nil {
			return created, fmt.Errorf("write %s: %w", historyNoteName, err)
		}
		created = append(created, note)
	}

	added, err := gitignoreReviewFiles(rootDir)
	if err != nil {
		return created, err
	}
	created = append(created, added...)
	return created, nil
}

// historyNoteName is the file in the history directory that explains it to
// agents working in the project.
const historyNoteName = "AGENTS.md"

// historyNote is what that file says. Agents read AGENTS.md files as
// instructions, so a retired review can be mistaken for a current one.
const historyNote = `# Historical reviews

The files in this directory are **not the current code reviews**. They are
historical reviews: earlier REVIEW.md files that were moved aside when a new
review was started.

Please ignore them, unless the user asks you to take inspiration from them.
`

// gitignoreEntries are the paths the review tool keeps out of the project's
// commits. The leading slash anchors them to the project root.
var gitignoreEntries = []string{
	"/" + reviewFileName,
	"/" + ReviewDir + "/",
}

// reviewFileName is the annotation file the tool writes at the root of the
// reviewed directory.
const reviewFileName = "REVIEW.md"

// gitignoreReviewFiles adds the review entries missing from the project's
// .gitignore and returns the .gitignore path when it wrote one.
func gitignoreReviewFiles(rootDir string) ([]string, error) {
	path := filepath.Join(rootDir, ".gitignore")
	present, err := readGitignore(path)
	if err != nil {
		return nil, err
	}

	var missing []string
	for _, entry := range gitignoreEntries {
		if !present[entry] {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}

	if err := appendGitignore(path, missing); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

// readGitignore returns the entries of a .gitignore as a set. A missing file
// reads as an empty one.
func readGitignore(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, fmt.Errorf("read .gitignore: %w", err)
	}
	defer f.Close()

	entries := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		entries[strings.TrimSpace(scanner.Text())] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read .gitignore: %w", err)
	}
	return entries, nil
}

// appendGitignore adds entries to a .gitignore under a heading, after a blank
// line, keeping a missing final newline from running into them.
func appendGitignore(path string, entries []string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read .gitignore: %w", err)
	}

	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 {
		if existing[len(existing)-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteString("\n# Code review tool\n")
	}
	for _, entry := range entries {
		b.WriteString(entry)
		b.WriteByte('\n')
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	return nil
}
