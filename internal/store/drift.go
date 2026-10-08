package store

import (
	"os"
	"path/filepath"
)

// CheckDrift checks annotations for a single file against the current source.
// It relocates annotations whose context has moved and marks as outdated those
// whose context can no longer be found. Returns whether anything changed, along
// with any error from writing the result out.
func (s *Store) CheckDrift(filePath string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.checkDrift(filePath) {
		return false, nil
	}
	return true, s.flush()
}

// CheckAllDrift runs drift detection on all annotated files, writing the result
// once rather than after every file. Returns the file paths that changed, along
// with any error from writing the result out.
func (s *Store) CheckAllDrift() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := make(map[string]bool)
	for file := range s.data {
		if s.checkDrift(file) {
			changed[file] = true
		}
	}
	if len(changed) == 0 {
		return changed, nil
	}
	return changed, s.flush()
}

// checkDrift brings one file's annotations back in line with its source and
// reports whether that changed anything. The caller holds the lock and persists
// the result.
func (s *Store) checkDrift(filePath string) bool {
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
		start := ann.StartFor(lineNum)
		if len(ann.Context) == 0 {
			// Nothing recorded to compare against, as in files written before
			// the context was stored: adopt the current source as reference.
			if ctx, from := contextAroundRange(fileLines, start, lineNum, ContextRadius); len(ctx) > 0 {
				ann.Context, ann.ContextFrom = ctx, from
				ann.StartLine = start
				changed = true
			}
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
		newFrom := findContext(fileLines, ann.Context, ann.ContextFrom)
		if newFrom > 0 {
			// Found at a new position — relocate
			delta := newFrom - ann.ContextFrom
			newLine := lineNum + delta
			if newLine >= 1 {
				ann.ContextFrom = newFrom
				ann.StartLine += delta
				if ann.StartLine < 1 {
					ann.StartLine = newLine
				}
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

	return changed
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

// findContext searches the file for a block of lines matching context and
// returns its 1-based line number, or 0 when there is none.
//
// The search runs outwards from near, so the match closest to where the context
// used to be wins. Code that repeats itself — a run of closing braces, blank
// lines, the same few lines of boilerplate — would otherwise pull an annotation
// to whichever copy comes first in the file.
func findContext(fileLines []string, context []string, near int) int {
	limit := len(fileLines) - len(context) + 1
	if len(context) == 0 || limit < 1 {
		return 0
	}
	if near < 1 {
		near = 1
	}

	for d := 0; d <= max(near-1, limit-near); d++ {
		if at := near - d; at >= 1 && contextMatchesAt(fileLines, context, at) {
			return at
		}
		if d == 0 {
			continue
		}
		if at := near + d; at <= limit && contextMatchesAt(fileLines, context, at) {
			return at
		}
	}
	return 0
}
