package server

import (
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
	"review/internal/store"
)

type handlers struct {
	store   *store.Store
	rootDir string
}

func (h *handlers) handleTree(w http.ResponseWriter, r *http.Request) {
	tree, err := filetree.Walk(h.rootDir)
	if err != nil {
		jsonError(w, "failed to walk directory", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, tree)
}

type fileResponse struct {
	HTML      string                       `json:"html"`
	Language  string                       `json:"language"`
	DiffLines map[int]gitstatus.LineChange `json:"diffLines,omitempty"`
	DiffHunks []gitstatus.DiffHunk         `json:"diffHunks,omitempty"`
}

func (h *handlers) handleFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		jsonError(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Prevent path traversal
	if strings.Contains(path, "..") {
		jsonError(w, "invalid path", http.StatusBadRequest)
		return
	}

	absPath := filepath.Join(h.rootDir, path)
	content, err := os.ReadFile(absPath)
	if err != nil {
		jsonError(w, "file not found", http.StatusNotFound)
		return
	}

	hl := highlight.Highlight(path, string(content))
	resp := fileResponse{
		HTML:      hl.HTML,
		Language:  hl.Language,
		DiffLines: gitstatus.DiffLines(h.rootDir, path),
		DiffHunks: gitstatus.DiffHunksForFile(h.rootDir, path),
	}
	jsonResponse(w, resp)
}

// annotationResponse is the JSON shape for a single annotation.
type annotationResponse struct {
	Comment  string `json:"comment"`
	Outdated bool   `json:"outdated"`
}

func (h *handlers) handleGetAnnotations(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		// Return all annotations
		all := h.store.All()
		result := make(map[string]map[string]annotationResponse, len(all))
		for file, lines := range all {
			fileAnns := make(map[string]annotationResponse, len(lines))
			for line, ann := range lines {
				fileAnns[strconv.Itoa(line)] = annotationResponse{
					Comment:  ann.Comment,
					Outdated: ann.Outdated,
				}
			}
			result[file] = fileAnns
		}
		jsonResponse(w, result)
		return
	}
	fileAnns := h.store.GetFile(path)
	result := make(map[string]annotationResponse, len(fileAnns))
	for line, ann := range fileAnns {
		result[strconv.Itoa(line)] = annotationResponse{
			Comment:  ann.Comment,
			Outdated: ann.Outdated,
		}
	}
	jsonResponse(w, result)
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
	if strings.Contains(req.Path, "..") {
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

	if err := h.store.Delete(req.Path, req.Line); err != nil {
		jsonError(w, fmt.Sprintf("failed to delete: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

func (h *handlers) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	statuses := gitstatus.Get(h.rootDir)
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

func (h *handlers) handleDeleteReview(w http.ResponseWriter, r *http.Request) {
	mdPath := h.store.MdPath()
	if err := os.Remove(mdPath); err != nil && !os.IsNotExist(err) {
		jsonError(w, fmt.Sprintf("failed to delete: %v", err), http.StatusInternalServerError)
		return
	}
	// Reload store (now empty)
	h.store.Reload()
	jsonResponse(w, map[string]string{"status": "ok"})
}

func (h *handlers) handleChromaCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	w.Write([]byte(highlight.CSS()))
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
