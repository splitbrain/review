package watcher

import (
	"log"
	"path/filepath"
	"sync"
	"time"

	"review/internal/store"

	"github.com/fsnotify/fsnotify"
)

// Event represents a change detected by the watcher.
type Event struct {
	Type string `json:"type"` // "file-changed", "review-deleted", "review-reloaded"
	Path string `json:"path,omitempty"` // relative path for file events
}

// Watcher monitors annotated source files and REVIEW.md for changes.
type Watcher struct {
	store     *store.Store
	fsw       *fsnotify.Watcher
	events    chan Event
	done      chan struct{}
	debounce  map[string]*time.Timer
	debounceMu sync.Mutex
}

// New creates a new file watcher. Call Start() to begin watching.
func New(st *store.Store) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		store:    st,
		fsw:      fsw,
		events:   make(chan Event, 64),
		done:     make(chan struct{}),
		debounce: make(map[string]*time.Timer),
	}

	return w, nil
}

// Events returns the channel of file change events.
func (w *Watcher) Events() <-chan Event {
	return w.events
}

// Start begins watching files and processing events.
func (w *Watcher) Start() {
	// Watch REVIEW.md
	mdPath := w.store.MdPath()
	// Watch the directory containing REVIEW.md (to detect deletion/creation)
	mdDir := filepath.Dir(mdPath)
	w.fsw.Add(mdDir)

	// Watch all annotated source files
	w.addAnnotatedFiles()

	// Re-add watches when annotations change
	w.store.OnChange(func() {
		w.addAnnotatedFiles()
	})

	go w.loop()
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	close(w.done)
	w.fsw.Close()
}

func (w *Watcher) addAnnotatedFiles() {
	srcRoot := w.store.SrcRoot()
	for _, relPath := range w.store.AnnotatedFiles() {
		absPath := filepath.Join(srcRoot, relPath)
		// Watch the directory containing the file
		dir := filepath.Dir(absPath)
		w.fsw.Add(dir)
	}
}

func (w *Watcher) loop() {
	mdPath := w.store.MdPath()
	srcRoot := w.store.SrcRoot()

	for {
		select {
		case <-w.done:
			return

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}

			absPath := ev.Name

			// Is this the REVIEW.md file?
			if absPath == mdPath || absPath == mdPath+".tmp" {
				// Ignore .tmp files (our own atomic writes)
				if absPath == mdPath+".tmp" {
					continue
				}
				if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
					w.emitDebounced(mdPath, Event{Type: "review-deleted"})
				} else if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) {
					w.emitDebounced(mdPath, Event{Type: "review-reloaded"})
				}
				continue
			}

			// Is this a source file we care about?
			if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
				relPath, err := filepath.Rel(srcRoot, absPath)
				if err != nil {
					continue
				}
				// Check if this file has annotations
				anns := w.store.GetFile(relPath)
				if len(anns) == 0 {
					continue
				}
				w.emitDebounced(absPath, Event{Type: "file-changed", Path: relPath})
			}

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

func (w *Watcher) emitDebounced(key string, event Event) {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()

	if t, ok := w.debounce[key]; ok {
		t.Stop()
	}

	w.debounce[key] = time.AfterFunc(500*time.Millisecond, func() {
		// For file-changed events, run drift detection first
		if event.Type == "file-changed" {
			changed := w.store.CheckDrift(event.Path)
			if !changed {
				return // No actual drift — no need to notify frontend
			}
		} else if event.Type == "review-reloaded" {
			if err := w.store.Reload(); err != nil {
				log.Printf("failed to reload REVIEW.md: %v", err)
				return
			}
		}

		select {
		case w.events <- event:
		default:
			// Channel full — drop event
		}

		w.debounceMu.Lock()
		delete(w.debounce, key)
		w.debounceMu.Unlock()
	})
}
