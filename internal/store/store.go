package store

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// ContextRadius is the number of source lines kept above and below an
// annotated line to recognise the code again after it moved.
const ContextRadius = 3

// Annotation holds a review comment with its source context.
//
// A comment can cover a range of lines. It is keyed by the last line of that
// range, which is what the line header names, and StartLine holds the first.
// A comment on a single line has StartLine equal to its key, the same shape
// Gerrit's CommentRange takes for a one-line comment.
type Annotation struct {
	Comment     string   `json:"comment"`
	StartLine   int      `json:"startLine"` // first line of the range (1-based)
	Context     []string `json:"-"`         // stored context lines (without line-number prefix)
	ContextFrom int      `json:"-"`         // first line number of context block
	Outdated    bool     `json:"outdated"`  // true if context no longer matches source
}

// StartFor returns the first line an annotation covers, given the end line it
// is keyed by. An annotation written before ranges were recorded carries no
// start line, and covers the end line alone.
func (a *Annotation) StartFor(endLine int) int {
	if a.StartLine >= 1 && a.StartLine <= endLine {
		return a.StartLine
	}
	return endLine
}

// equal reports whether two annotations describe the same comment on the same
// piece of code.
func (a *Annotation) equal(b *Annotation) bool {
	return a.Comment == b.Comment &&
		a.StartLine == b.StartLine &&
		a.Outdated == b.Outdated &&
		a.ContextFrom == b.ContextFrom &&
		slices.Equal(a.Context, b.Context)
}

// Store holds annotations in memory and persists them to REVIEW.md.
type Store struct {
	mdPath   string
	srcRoot  string
	data     map[string]map[int]*Annotation
	started  string // date the review was started, as recorded in REVIEW.md
	mu       sync.RWMutex
	onChange []func()
}

// Load reads the REVIEW.md file (if it exists) and returns a ready Store.
func Load(mdPath, srcRoot string) (*Store, error) {
	abs, err := filepath.Abs(mdPath)
	if err != nil {
		return nil, fmt.Errorf("resolve md path: %w", err)
	}
	srcAbs, err := filepath.Abs(srcRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve src root: %w", err)
	}

	data, started, err := parse(abs)
	if err != nil {
		return nil, fmt.Errorf("parse REVIEW.md: %w", err)
	}

	return &Store{
		mdPath:  abs,
		srcRoot: srcAbs,
		data:    data,
		started: started,
	}, nil
}

// OnChange registers a callback that fires after any mutation (Set/Delete).
func (s *Store) OnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = append(s.onChange, fn)
}

// notifyChange runs the registered callbacks. They are collected under the lock
// but called without it, since a callback is free to read the store.
func (s *Store) notifyChange() {
	s.mu.RLock()
	fns := slices.Clone(s.onChange)
	s.mu.RUnlock()

	for _, fn := range fns {
		fn()
	}
}

// Set adds or updates a comment on a specific file and line range. The
// surrounding source lines are recorded with it so the annotation can be
// followed when the code later moves. The annotation is keyed by endLine, so a
// comment set with the same end line replaces the previous one.
func (s *Store) Set(file string, startLine, endLine int, comment string) error {
	s.mu.Lock()

	if s.data[file] == nil {
		s.data[file] = make(map[int]*Annotation)
	}
	ann := &Annotation{
		Comment:   strings.TrimSpace(comment),
		StartLine: startLine,
	}
	if lines, err := readFileLines(s.srcRoot, file); err == nil {
		ann.Context, ann.ContextFrom = contextAroundRange(lines, startLine, endLine, ContextRadius)
	}
	s.data[file][endLine] = ann
	err := s.flush()
	s.mu.Unlock()

	if err == nil {
		s.notifyChange()
	}
	return err
}

// Delete removes a comment from a specific file and line.
func (s *Store) Delete(file string, line int) error {
	s.mu.Lock()

	if m, ok := s.data[file]; ok {
		delete(m, line)
		if len(m) == 0 {
			delete(s.data, file)
		}
	}
	err := s.flush()
	s.mu.Unlock()

	if err == nil {
		s.notifyChange()
	}
	return err
}

// GetFile returns a copy of annotations for a single file.
func (s *Store) GetFile(file string) map[int]*Annotation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orig := s.data[file]
	if orig == nil {
		return map[int]*Annotation{}
	}
	cp := make(map[int]*Annotation, len(orig))
	for k, v := range orig {
		a := *v
		cp[k] = &a
	}
	return cp
}

// All returns a deep copy of all annotations.
func (s *Store) All() map[string]map[int]*Annotation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := make(map[string]map[int]*Annotation, len(s.data))
	for file, lines := range s.data {
		linesCp := make(map[int]*Annotation, len(lines))
		for k, v := range lines {
			a := *v
			linesCp[k] = &a
		}
		cp[file] = linesCp
	}
	return cp
}

// AnnotatedFiles returns a list of all file paths that have annotations.
func (s *Store) AnnotatedFiles() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files := make([]string, 0, len(s.data))
	for f := range s.data {
		files = append(files, f)
	}
	return files
}

// MdPath returns the absolute path to the REVIEW.md file.
func (s *Store) MdPath() string {
	return s.mdPath
}

// SrcRoot returns the absolute path to the source root directory.
func (s *Store) SrcRoot() string {
	return s.srcRoot
}

// Reload re-reads REVIEW.md from disk and replaces in-memory data. It reports
// whether the file differs from what was already in memory, which tells a
// write made by someone else from the store's own write being observed.
func (s *Store) Reload() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, started, err := parse(s.mdPath)
	if err != nil {
		return false, fmt.Errorf("parse REVIEW.md: %w", err)
	}
	changed := !equalAnnotations(s.data, data)
	s.data = data
	s.started = started
	return changed, nil
}

// equalAnnotations reports whether two annotation maps hold the same review.
func equalAnnotations(a, b map[string]map[int]*Annotation) bool {
	if len(a) != len(b) {
		return false
	}
	for file, aLines := range a {
		bLines, ok := b[file]
		if !ok || len(aLines) != len(bLines) {
			return false
		}
		for line, aAnn := range aLines {
			bAnn, ok := bLines[line]
			if !ok || !aAnn.equal(bAnn) {
				return false
			}
		}
	}
	return true
}

// Flush persists current state to REVIEW.md. Exported for use by drift detection.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush()
}

// flush serialises the map and atomically writes REVIEW.md.
func (s *Store) flush() error {
	if s.started == "" {
		s.started = time.Now().Format(startedFormat)
	}
	content := serialize(s.data, s.started)
	tmp := s.mdPath + ".tmp"

	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, s.mdPath); err != nil {
		return fmt.Errorf("rename to REVIEW.md: %w", err)
	}
	return nil
}
