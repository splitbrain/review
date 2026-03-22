package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Annotation holds a review comment with its source context.
type Annotation struct {
	Comment     string   `json:"comment"`
	Context     []string `json:"-"`          // stored context lines (without line-number prefix)
	ContextFrom int      `json:"-"`          // first line number of context block
	Outdated    bool     `json:"outdated"`   // true if context no longer matches source
}

// Store holds annotations in memory and persists them to REVIEW.md.
type Store struct {
	mdPath    string
	srcRoot   string
	data      map[string]map[int]*Annotation
	mu        sync.RWMutex
	onChange  []func()
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

	data, err := parse(abs)
	if err != nil {
		return nil, fmt.Errorf("parse REVIEW.md: %w", err)
	}

	return &Store{
		mdPath:  abs,
		srcRoot: srcAbs,
		data:    data,
	}, nil
}

// OnChange registers a callback that fires after any mutation (Set/Delete).
func (s *Store) OnChange(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = append(s.onChange, fn)
}

func (s *Store) notifyChange() {
	for _, fn := range s.onChange {
		fn()
	}
}

// Set adds or updates a comment on a specific file and line.
func (s *Store) Set(file string, line int, comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data[file] == nil {
		s.data[file] = make(map[int]*Annotation)
	}
	s.data[file][line] = &Annotation{Comment: comment}
	err := s.flush()
	if err == nil {
		s.notifyChange()
	}
	return err
}

// Delete removes a comment from a specific file and line.
func (s *Store) Delete(file string, line int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m, ok := s.data[file]; ok {
		delete(m, line)
		if len(m) == 0 {
			delete(s.data, file)
		}
	}
	err := s.flush()
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

// Reload re-reads REVIEW.md from disk and replaces in-memory data.
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := parse(s.mdPath)
	if err != nil {
		return fmt.Errorf("parse REVIEW.md: %w", err)
	}
	s.data = data
	return nil
}

// Flush persists current state to REVIEW.md. Exported for use by drift detection.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush()
}

// flush serialises the map and atomically writes REVIEW.md.
func (s *Store) flush() error {
	content := serialize(s.data, s.srcRoot)
	tmp := s.mdPath + ".tmp"

	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, s.mdPath); err != nil {
		return fmt.Errorf("rename to REVIEW.md: %w", err)
	}
	return nil
}
