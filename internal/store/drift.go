package store

import (
	"os"
	"path/filepath"
)

// CheckDrift checks annotations for a single file against the current source.
// It relocates annotations whose context has moved and marks as outdated those
// whose context can no longer be found. Returns true if any changes were made.
func (s *Store) CheckDrift(filePath string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	annotations := s.data[filePath]
	if len(annotations) == 0 {
		return false
	}

	// Check if the source file still exists
	absPath := filepath.Join(s.srcRoot, filePath)
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		// File deleted — mark all annotations as outdated
		changed := false
		for _, ann := range annotations {
			if !ann.Outdated {
				ann.Outdated = true
				changed = true
			}
		}
		if changed {
			s.flush()
		}
		return changed
	}

	// Read current file lines
	fileLines, err := readFileLines(s.srcRoot, filePath)
	if err != nil {
		return false
	}

	changed := false
	// Collect relocations: we may need to change keys in the map
	type relocation struct {
		oldLine int
		newLine int
		ann     *Annotation
	}
	var relocations []relocation

	for lineNum, ann := range annotations {
		if len(ann.Context) == 0 {
			// No stored context to compare — skip
			continue
		}

		// Check if context still matches at stored position
		if contextMatchesAt(fileLines, ann.Context, ann.ContextFrom) {
			// Context is in the same place — clear outdated if it was set
			if ann.Outdated {
				ann.Outdated = false
				changed = true
			}
			continue
		}

		// Context doesn't match at stored position — try to find it elsewhere
		newFrom := findContext(fileLines, ann.Context)
		if newFrom > 0 {
			// Found at a new position — relocate
			delta := newFrom - ann.ContextFrom
			newLine := lineNum + delta
			if newLine >= 1 {
				ann.ContextFrom = newFrom
				ann.Outdated = false
				if newLine != lineNum {
					relocations = append(relocations, relocation{
						oldLine: lineNum,
						newLine: newLine,
						ann:     ann,
					})
				}
				changed = true
			}
		} else {
			// Context not found anywhere — mark as outdated
			if !ann.Outdated {
				ann.Outdated = true
				changed = true
			}
		}
	}

	// Apply relocations (change map keys)
	for _, r := range relocations {
		delete(annotations, r.oldLine)
		annotations[r.newLine] = r.ann
	}

	if changed {
		s.flush()
	}
	return changed
}

// CheckAllDrift runs drift detection on all annotated files.
// Returns a map of file paths that had changes.
func (s *Store) CheckAllDrift() map[string]bool {
	// Get list of files while holding read lock
	s.mu.RLock()
	files := make([]string, 0, len(s.data))
	for f := range s.data {
		files = append(files, f)
	}
	s.mu.RUnlock()

	result := make(map[string]bool)
	for _, f := range files {
		if s.CheckDrift(f) {
			result[f] = true
		}
	}
	return result
}

// contextMatchesAt checks if the given context lines match the file at the given position.
// fromLine is 1-based.
func contextMatchesAt(fileLines []string, context []string, fromLine int) bool {
	if fromLine < 1 || fromLine-1+len(context) > len(fileLines) {
		return false
	}
	for i, ctx := range context {
		if fileLines[fromLine-1+i] != ctx {
			return false
		}
	}
	return true
}

// findContext searches the entire file for a block of lines matching context.
// Returns the 1-based line number of the first match, or 0 if not found.
func findContext(fileLines []string, context []string) int {
	if len(context) == 0 {
		return 0
	}
	limit := len(fileLines) - len(context) + 1
	for i := 0; i < limit; i++ {
		match := true
		for j, ctx := range context {
			if fileLines[i+j] != ctx {
				match = false
				break
			}
		}
		if match {
			return i + 1 // 1-based
		}
	}
	return 0
}
