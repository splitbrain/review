# Plan: Source File Change Detection & Live Updates

## Overview

Three interconnected features:
1. **Context drift detection** — detect when source files change relative to stored annotations, relocate annotations when possible, mark as outdated when not
2. **File watching** — use fsnotify to monitor annotated source files and REVIEW.md for changes
3. **WebSocket** — push change notifications to the frontend in real-time

---

## Part 1: Store Context Lines & Detect Drift

### 1a. Parse and store context from REVIEW.md

Currently `parse.go` skips the code fence content (the `skipFence` state just discards lines). We need to capture it.

**Changes to `internal/store/parse.go`:**
- In the `skipFence` state, collect the fenced lines into a context buffer instead of discarding them
- Extract the context lines as a `[]string` of raw source text (strip the `N: ` prefix during parsing)
- Also extract the line number range from the `N:` prefixes

**New type in `internal/store/store.go`:**
```go
type Annotation struct {
    Comment     string
    Context     []string  // stored context lines from REVIEW.md (without line-number prefix)
    ContextFrom int       // first line number of context (drives the "N: ..." prefixes in REVIEW.md)
    Outdated    bool      // true if context no longer matches source
}
// Note: when drift detection relocates an annotation, ContextFrom is updated
// to the new position. On flush(), serialize() calls readContext() which re-reads
// the source file at the new line range, so the "N: content" prefixes in the
// REVIEW.md code fence are regenerated with the correct (relocated) line numbers.
```

Change `data` from `map[string]map[int]string` to `map[string]map[int]*Annotation`. This is the biggest refactor — all call sites (store methods, handlers, serialization) must be updated.

### 1b. Drift detection & relocation logic

**New file `internal/store/drift.go`:**

Provide a method `(s *Store) CheckDrift(filePath string) (changed bool)` that:

1. For each annotation on `filePath`, reads the current source file
2. Compares stored `Context` lines against the lines at `ContextFrom..ContextFrom+len(Context)-1` in the current file
3. If they match — no change needed
4. If they don't match — attempt relocation:
   - Search the entire file for the stored context block (exact substring match of all context lines)
   - If found at a new offset, compute the delta: `delta = newContextFrom - oldContextFrom`. Update:
     - The annotation's key in the map: move from `oldLine` to `oldLine + delta`
     - `ContextFrom`: set to `newContextFrom`
     - `Context` lines stay the same (content unchanged, just moved)
   - Set `changed = true`.
   - If not found, mark the annotation as `Outdated = true`. Set `changed = true`.
5. If `changed`, call `flush()` to rewrite REVIEW.md with corrected line numbers. Since `serialize()` calls `readContext()` which re-reads the source file and formats `N: content` lines using the (now-corrected) line number, the context block in REVIEW.md will automatically get the updated line-number prefixes.

Also provide `(s *Store) CheckAllDrift() map[string]bool` that runs drift check on all annotated files and returns which files had changes.

### 1c. Serialize outdated annotations

**Changes to `internal/store/write.go`:**
- When writing an annotation that is `Outdated`, add a marker in the REVIEW.md, e.g. `#### Line N (outdated)`
- Update `parse.go` to recognize the `(outdated)` suffix on line headers

### 1d. Expose outdated status in API

**Changes to `internal/server/handlers.go`:**
- `GET /api/annotations` and `GET /api/annotations?path=...` should return objects that include the `outdated` flag:
  ```json
  { "42": { "comment": "...", "outdated": false } }
  ```
  This changes the shape of the annotation response from `{line: comment}` to `{line: {comment, outdated}}`.

**Changes to `frontend/app.js`:**
- Update all annotation handling to use the new `{comment, outdated}` shape
- Render outdated annotations with a visual indicator (e.g. amber/yellow background, strikethrough on line number, warning icon)
- In the comment sidebar, show outdated annotations with a warning badge

**Changes to `frontend/style.css`:**
- Add `.outdated` styles for annotations

---

## Part 2: File Watching with fsnotify

### 2a. Add fsnotify dependency

```
go get github.com/fsnotify/fsnotify
```

### 2b. New watcher component

**New file `internal/watcher/watcher.go`:**

```go
type Event struct {
    Type string // "file-changed", "file-deleted", "review-deleted", "review-changed"
    Path string // relative path for file events
}

type Watcher struct {
    fsWatcher *fsnotify.Watcher
    events    chan Event
    store     *store.Store
    srcRoot   string
}
```

Responsibilities:
- Watch all source files that have annotations (from `store.All()`)
- Watch the REVIEW.md file itself
- On source file change: run `store.CheckDrift(path)`, emit `file-changed` event if drift was detected
- On source file delete: mark all annotations for that file as outdated, emit `file-changed`
- On REVIEW.md delete: emit `review-deleted` event (frontend should show a notification)
- On REVIEW.md external change: reload the store from disk, emit `review-changed`
- When annotations are added/removed (new files get annotated), dynamically add/remove watched paths
- Debounce events (editors may trigger multiple writes) — 500ms window per file

### 2c. Integrate watcher with store

**Changes to `internal/store/store.go`:**
- Add a method `(s *Store) Reload() error` that re-parses REVIEW.md from disk and replaces in-memory data
- Add a callback/channel mechanism so the store can notify the watcher when new files get annotated: `(s *Store) OnChange(func(file string))` — called after Set/Delete so the watcher can update its watch list
- Add a method `(s *Store) WatchedFiles() []string` that returns all file paths with annotations

### 2d. Wire up in main.go

- Create watcher after store is loaded
- Pass watcher's event channel to the server (for WebSocket broadcasting)
- Start watcher in a goroutine
- Ensure clean shutdown (stop watcher on SIGINT/SIGTERM)

---

## Part 3: WebSocket for Live Updates

### 3a. Add gorilla/websocket dependency

```
go get github.com/gorilla/websocket
```

### 3b. WebSocket hub

**New file `internal/server/ws.go`:**

```go
type Hub struct {
    clients    map[*Client]bool
    broadcast  chan []byte
    register   chan *Client
    unregister chan *Client
}

type Client struct {
    conn *websocket.Conn
    send chan []byte
}
```

Standard hub pattern: clients register/unregister, hub broadcasts messages to all connected clients.

Message format (JSON):
```json
{"type": "file-changed", "path": "src/main.go", "annotations": {...}}
{"type": "review-deleted"}
{"type": "review-reloaded", "allAnnotations": {...}}
{"type": "annotations-updated", "path": "src/main.go", "annotations": {...}}
```

### 3c. WebSocket route

**Changes to `internal/server/server.go`:**
- Add `r.Get("/ws", hub.handleWebSocket)` route
- Pass the Hub and watcher event channel to the server
- Start a goroutine that reads from the watcher event channel and broadcasts to the hub

### 3d. Frontend WebSocket client

**Changes to `frontend/app.js`:**

Add WebSocket connection management:
- Connect on page load: `new WebSocket('ws://' + location.host + '/ws')`
- Auto-reconnect with exponential backoff on disconnect
- Handle incoming messages:
  - `file-changed`: if the changed file is currently open, refresh it (re-fetch `/api/file` and annotations). Update `allAnnotations` for the sidebar. Re-render tree to update comment dots.
  - `review-deleted`: show a banner/notification "REVIEW.md was deleted. All annotations have been lost." Clear all state.
  - `review-reloaded`: replace `allAnnotations`, refresh current file if open
  - `annotations-updated`: same as `file-changed` but triggered by the store's own Set/Delete (for multi-tab sync — optional, lower priority)

### 3e. Notification UI

**Changes to `frontend/index.html` and `frontend/style.css`:**
- Add a toast/notification area for showing "file changed" messages
- Add a connection status indicator (small dot in status bar: green = connected, red = disconnected)

---

## Implementation Order

1. **Refactor store data model** — change `map[int]string` to `map[int]*Annotation`, update all call sites (store, handlers, frontend). This is the foundation everything else builds on.
2. **Parse context from REVIEW.md** — update parser to capture context lines into the new Annotation struct
3. **Drift detection** — implement `CheckDrift` with relocation and outdated marking
4. **Serialize outdated marker** — update write.go and parse.go for `(outdated)`
5. **API + frontend for outdated** — expose `outdated` flag, render it in the UI
6. **fsnotify watcher** — implement file watching with debouncing
7. **WebSocket hub** — implement server-side hub and route
8. **Frontend WebSocket** — connect, handle messages, auto-reconnect
9. **Wire everything together in main.go** — watcher → hub → frontend
10. **Tests** — unit tests for drift detection, context parsing, watcher events

---

## Files to Create
- `internal/store/drift.go` — drift detection & relocation logic
- `internal/watcher/watcher.go` — fsnotify-based file watcher
- `internal/server/ws.go` — WebSocket hub and client management

## Files to Modify
- `internal/store/store.go` — new Annotation type, Reload, OnChange, refactored data map
- `internal/store/parse.go` — capture context lines, parse `(outdated)` marker
- `internal/store/write.go` — serialize `(outdated)` marker
- `internal/server/server.go` — add WebSocket route, accept Hub
- `internal/server/handlers.go` — updated annotation response shape
- `internal/server/handlers_test.go` — update for new response shape
- `frontend/app.js` — WebSocket client, outdated rendering, updated annotation shape
- `frontend/style.css` — outdated styles, connection indicator, toast notifications
- `frontend/index.html` — toast container, connection indicator element
- `main.go` — create watcher, create hub, wire together, graceful shutdown
- `go.mod` / `go.sum` — new dependencies (fsnotify, gorilla/websocket)

## New Dependencies
- `github.com/fsnotify/fsnotify` — cross-platform file system notifications
- `github.com/gorilla/websocket` — WebSocket implementation for Go
