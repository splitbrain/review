package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"review/internal/filetree"
	"review/internal/gitstatus"
	"review/internal/highlight"
	"review/internal/safepath"
	"review/internal/store"
)

type handlers struct {
	store       *store.Store
	rootDir     string
	base        gitstatus.Base
	highlighter *highlight.Cache
}

func (h *handlers) handleTree(w http.ResponseWriter, r *http.Request) {
	tree, err := filetree.Walk(h.rootDir)
	if err != nil {
		jsonError(w, "failed to walk directory", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, tree)
}

// maxFileSize is the largest file the server reads into memory and hands to
// the highlighter.
const maxFileSize = 2 << 20 // 2 MiB

// binarySniffLen is how much of a file is examined to tell code from binary
// data. Git looks at the same amount.
const binarySniffLen = 8000

type fileResponse struct {
	HTML          string                       `json:"html"`
	Language      string                       `json:"language"`
	DiffLines     map[int]gitstatus.LineChange `json:"diffLines,omitempty"`
	DiffHunks     []gitstatus.DiffHunk         `json:"diffHunks,omitempty"`
	DiffDeletions []gitstatus.DiffDeletion     `json:"diffDeletions,omitempty"`
}

func (h *handlers) handleFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		jsonError(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Prevent path traversal
	absPath, ok := h.resolvePath(path)
	if !ok {
		jsonError(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		jsonError(w, "file not found", http.StatusNotFound)
		return
	}
	if info.Size() > maxFileSize {
		jsonError(w, "file is too large to display", http.StatusRequestEntityTooLarge)
		return
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		jsonError(w, "file not found", http.StatusNotFound)
		return
	}
	if isBinary(content) {
		jsonError(w, "binary file", http.StatusBadRequest)
		return
	}

	hl := h.highlighter.Highlight(path, string(content))
	diff := gitstatus.GetFileDiff(h.rootDir, h.base, path, content)
	resp := fileResponse{
		HTML:          hl.HTML,
		Language:      hl.Language,
		DiffLines:     diff.Lines,
		DiffHunks:     diff.Hunks,
		DiffDeletions: diff.Deletions,
	}
	jsonResponse(w, resp)
}

// annotationResponse is the JSON shape of a single annotation.
type annotationResponse struct {
	Comment  string `json:"comment"`
	Outdated bool   `json:"outdated"`
}

// fileAnnotations converts one file's annotations into the shape clients
// receive, keyed by line number.
func fileAnnotations(anns map[int]*store.Annotation) map[string]annotationResponse {
	result := make(map[string]annotationResponse, len(anns))
	for line, ann := range anns {
		result[strconv.Itoa(line)] = annotationResponse{
			Comment:  ann.Comment,
			Outdated: ann.Outdated,
		}
	}
	return result
}

// allAnnotations converts the annotations of every file into the shape clients
// receive, keyed by file path.
func allAnnotations(all map[string]map[int]*store.Annotation) map[string]map[string]annotationResponse {
	result := make(map[string]map[string]annotationResponse, len(all))
	for file, lines := range all {
		result[file] = fileAnnotations(lines)
	}
	return result
}

func (h *handlers) handleGetAnnotations(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		jsonResponse(w, allAnnotations(h.store.All()))
		return
	}
	jsonResponse(w, fileAnnotations(h.store.GetFile(path)))
}

type annotationRequest struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Comment string `json:"comment"`
}

func (h *handlers) handleSetAnnotation(w http.ResponseWriter, r *http.Request) {
	var req annotationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Path == "" || req.Line < 1 {
		jsonError(w, "path and line (>= 1) are required", http.StatusBadRequest)
		return
	}
	if _, ok := h.resolvePath(req.Path); !ok {
		jsonError(w, "invalid path", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Comment) == "" {
		jsonError(w, "comment cannot be empty", http.StatusBadRequest)
		return
	}

	if err := h.store.Set(req.Path, req.Line, req.Comment); err != nil {
		jsonError(w, fmt.Sprintf("failed to save: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

func (h *handlers) handleDeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	var req annotationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Path == "" || req.Line < 1 {
		jsonError(w, "path and line (>= 1) are required", http.StatusBadRequest)
		return
	}
	if _, ok := h.resolvePath(req.Path); !ok {
		jsonError(w, "invalid path", http.StatusBadRequest)
		return
	}

	if err := h.store.Delete(req.Path, req.Line); err != nil {
		jsonError(w, fmt.Sprintf("failed to delete: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// configResponse is the JSON shape for the review's global settings.
type configResponse struct {
	Base           string `json:"base"`                  // revision changes are compared against, empty when comparing against HEAD
	HistoryEnabled bool   `json:"historyEnabled"`        // whether starting a new review archives the old one instead of deleting it
	HistoryDest    string `json:"historyDest,omitempty"` // where the current review would be archived, relative to the review root
}

func (h *handlers) handleConfig(w http.ResponseWriter, r *http.Request) {
	resp := configResponse{Base: h.base.Rev, HistoryEnabled: h.store.HistoryEnabled()}
	if dest, ok := h.store.NextHistoryDest(); ok {
		resp.HistoryDest = dest
	}
	jsonResponse(w, resp)
}

func (h *handlers) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	statuses := gitstatus.Get(h.rootDir, h.base)
	if statuses == nil {
		// Not a git repo — return empty object
		jsonResponse(w, map[string]string{})
		return
	}
	// Convert to string→string for JSON
	result := make(map[string]string, len(statuses))
	for path, status := range statuses {
		result[path] = string(status)
	}
	jsonResponse(w, result)
}

// newReviewResponse reports what starting a new review did with the old one.
type newReviewResponse struct {
	Status   string `json:"status"`
	Archived string `json:"archived,omitempty"` // path of the retired review, empty when it was deleted
}

func (h *handlers) handleDeleteReview(w http.ResponseWriter, r *http.Request) {
	dest, err := h.store.ArchiveReview()
	if err != nil {
		jsonError(w, fmt.Sprintf("failed to start a new review: %v", err), http.StatusInternalServerError)
		return
	}

	resp := newReviewResponse{Status: "ok"}
	if dest != "" {
		// The path is relative to the review root, like every other path the
		// client sees.
		if rel, err := filepath.Rel(h.rootDir, dest); err == nil {
			resp.Archived = filepath.ToSlash(rel)
		} else {
			resp.Archived = dest
		}
	}
	jsonResponse(w, resp)
}

func (h *handlers) handleChromaCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	w.Write([]byte(highlight.CSS()))
}

// isBinary reports whether content looks like binary data rather than text.
// A NUL byte early in the file is the same signal git uses.
func isBinary(content []byte) bool {
	if len(content) > binarySniffLen {
		content = content[:binarySniffLen]
	}
	return bytes.IndexByte(content, 0) >= 0
}

// resolvePath resolves a client-supplied relative path against the review root.
func (h *handlers) resolvePath(path string) (string, bool) {
	return safepath.Resolve(h.rootDir, path)
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
