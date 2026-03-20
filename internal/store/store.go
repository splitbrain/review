package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store holds annotations in memory and persists them to REVIEW.md.
type Store struct {
	mdPath  string
	srcRoot string
	data    map[string]map[int]string
	mu      sync.RWMutex
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

// Set adds or updates a comment on a specific file and line.
func (s *Store) Set(file string, line int, comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data[file] == nil {
		s.data[file] = make(map[int]string)
	}
	s.data[file][line] = comment
	return s.flush()
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
	return s.flush()
}

// GetFile returns a copy of annotations for a single file.
func (s *Store) GetFile(file string) map[int]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orig := s.data[file]
	if orig == nil {
		return map[int]string{}
	}
	cp := make(map[int]string, len(orig))
	for k, v := range orig {
		cp[k] = v
	}
	return cp
}

// All returns a deep copy of all annotations.
func (s *Store) All() map[string]map[int]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := make(map[string]map[int]string, len(s.data))
	for file, lines := range s.data {
		linesCp := make(map[int]string, len(lines))
		for k, v := range lines {
			linesCp[k] = v
		}
		cp[file] = linesCp
	}
	return cp
}

// MdPath returns the absolute path to the REVIEW.md file.
func (s *Store) MdPath() string {
	return s.mdPath
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
