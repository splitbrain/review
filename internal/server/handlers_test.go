package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"review/internal/store"
)

// setupTestServer creates a test server with a temporary directory and store.
func setupTestServer(t *testing.T) (*httptest.Server, string, func()) {
	t.Helper()

	tmpDir := t.TempDir()

	// Create some test files
	if err := os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "hello.go"), []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test Project\n\nThis is a test.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "subdir", "util.go"), []byte("package subdir\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Multi-line file for testing full content loading
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "// line " + strings.Repeat("x", i+1)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "multiline.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mdPath := filepath.Join(tmpDir, "REVIEW.md")
	st, err := store.Load(mdPath, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// We don't have a real frontend FS for tests, so create a minimal one
	frontendDir := filepath.Join(tmpDir, "frontend")
	if err := os.MkdirAll(frontendDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html><body>test</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := New(st, tmpDir, os.DirFS(frontendDir), nil)
	ts := httptest.NewServer(handler)

	return ts, tmpDir, func() {
		ts.Close()
	}
}

// ---- /api/tree tests ----

func TestTreeEndpoint(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/tree")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var tree []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		t.Fatalf("failed to decode tree response: %v", err)
	}

	if len(tree) == 0 {
		t.Error("expected non-empty tree")
	}

	// Check that files we created are in the tree
	names := flattenTreeNames(tree)
	for _, expected := range []string{"hello.go", "README.md", "multiline.txt"} {
		if !contains(names, expected) {
			t.Errorf("expected tree to contain %q, got names: %v", expected, names)
		}
	}
}

func TestTreeEndpointMethod(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// POST should not be allowed
	resp, err := http.Post(ts.URL+"/api/tree", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 for POST on /api/tree")
	}
}

// ---- /api/file tests ----

func TestFileEndpoint_BasicFile(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/file?path=hello.go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode file response: %v", err)
	}

	html, ok := result["html"].(string)
	if !ok || html == "" {
		t.Error("expected non-empty html field")
	}

	lang, ok := result["language"].(string)
	if !ok || lang == "" {
		t.Error("expected non-empty language field")
	}
}

func TestFileEndpoint_MultilineContent(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/file?path=multiline.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode file response: %v", err)
	}

	html, ok := result["html"].(string)
	if !ok || html == "" {
		t.Fatal("expected non-empty html field")
	}

	for _, lineNum := range []string{"10", "20", "30", "40", "50"} {
		if !strings.Contains(html, lineNum) {
			t.Errorf("HTML should contain line number %s for a 50-line file, but it doesn't", lineNum)
		}
	}

	if !strings.Contains(html, "line") {
		t.Error("HTML should contain the word 'line' from file content")
	}
}

func TestFileEndpoint_SubdirFile(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/file?path=subdir/util.go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode file response: %v", err)
	}

	html := result["html"].(string)
	if !strings.Contains(html, "Add") {
		t.Error("expected HTML to contain function name 'Add'")
	}
}

func TestFileEndpoint_MissingPath(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/file")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFileEndpoint_PathTraversal(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/file?path=../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal, got %d", resp.StatusCode)
	}
}

func TestFileEndpoint_NonexistentFile(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/file?path=nonexistent.go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// ---- /api/annotations tests ----

// annotationObj is the new response shape: {comment, outdated}
type annotationObj struct {
	Comment  string `json:"comment"`
	Outdated bool   `json:"outdated"`
}

func TestAnnotations_GetEmpty(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/annotations")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode annotations: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty annotations, got %v", result)
	}
}

func TestAnnotations_SetAndGet(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Set an annotation
	body := `{"path":"hello.go","line":3,"comment":"This is the main function"}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Verify Content-Type header is set
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("POST response should have Content-Type application/json, got %q", ct)
	}

	// Get annotations for the file
	resp2, err := http.Get(ts.URL + "/api/annotations?path=hello.go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	var annots map[string]annotationObj
	if err := json.NewDecoder(resp2.Body).Decode(&annots); err != nil {
		t.Fatalf("failed to decode annotations: %v", err)
	}

	if annots["3"].Comment != "This is the main function" {
		t.Errorf("expected annotation on line 3, got %v", annots)
	}
	if annots["3"].Outdated {
		t.Error("expected annotation to not be outdated")
	}

	// Get all annotations
	resp3, err := http.Get(ts.URL + "/api/annotations")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()

	var allAnnots map[string]interface{}
	if err := json.NewDecoder(resp3.Body).Decode(&allAnnots); err != nil {
		t.Fatalf("failed to decode all annotations: %v", err)
	}

	if _, ok := allAnnots["hello.go"]; !ok {
		t.Errorf("expected hello.go in all annotations, got %v", allAnnots)
	}
}

func TestAnnotations_SetMultipleAndGetAll(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Set annotations on different files and lines
	annotations := []string{
		`{"path":"hello.go","line":1,"comment":"Package declaration"}`,
		`{"path":"hello.go","line":3,"comment":"Main entry point"}`,
		`{"path":"subdir/util.go","line":3,"comment":"Add function"}`,
	}

	for _, body := range annotations {
		resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	}

	// Get all annotations
	resp, err := http.Get(ts.URL + "/api/annotations")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var allAnnots map[string]map[string]annotationObj
	if err := json.NewDecoder(resp.Body).Decode(&allAnnots); err != nil {
		t.Fatalf("failed to decode all annotations: %v", err)
	}

	if len(allAnnots) != 2 {
		t.Errorf("expected 2 files with annotations, got %d", len(allAnnots))
	}

	if len(allAnnots["hello.go"]) != 2 {
		t.Errorf("expected 2 annotations on hello.go, got %d", len(allAnnots["hello.go"]))
	}
}

func TestAnnotations_Update(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Set an annotation
	body := `{"path":"hello.go","line":3,"comment":"Original comment"}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Update it
	body = `{"path":"hello.go","line":3,"comment":"Updated comment"}`
	resp, err = http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Get and verify
	resp, err = http.Get(ts.URL + "/api/annotations?path=hello.go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var annots map[string]annotationObj
	if err := json.NewDecoder(resp.Body).Decode(&annots); err != nil {
		t.Fatalf("failed to decode annotations: %v", err)
	}

	if annots["3"].Comment != "Updated comment" {
		t.Errorf("expected updated comment, got %v", annots)
	}
}

func TestAnnotations_Delete(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Set an annotation
	body := `{"path":"hello.go","line":3,"comment":"To be deleted"}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Delete it
	body = `{"path":"hello.go","line":3}`
	req, err := http.NewRequest("DELETE", ts.URL+"/api/annotations", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for DELETE, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Verify Content-Type on delete response
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("DELETE response should have Content-Type application/json, got %q", ct)
	}

	// Get annotations - should be empty
	resp2, err := http.Get(ts.URL + "/api/annotations?path=hello.go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	var annots map[string]annotationObj
	if err := json.NewDecoder(resp2.Body).Decode(&annots); err != nil {
		t.Fatalf("failed to decode annotations: %v", err)
	}

	if len(annots) != 0 {
		t.Errorf("expected empty annotations after delete, got %v", annots)
	}
}

func TestAnnotations_SetValidation_EmptyPath(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"path":"","line":3,"comment":"test"}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty path, got %d", resp.StatusCode)
	}
}

func TestAnnotations_SetValidation_ZeroLine(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"path":"hello.go","line":0,"comment":"test"}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for line 0, got %d", resp.StatusCode)
	}
}

func TestAnnotations_SetValidation_NegativeLine(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"path":"hello.go","line":-1,"comment":"test"}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for negative line, got %d", resp.StatusCode)
	}
}

func TestAnnotations_SetValidation_EmptyComment(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"path":"hello.go","line":3,"comment":""}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty comment, got %d", resp.StatusCode)
	}
}

func TestAnnotations_SetValidation_WhitespaceComment(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"path":"hello.go","line":3,"comment":"   "}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for whitespace-only comment, got %d", resp.StatusCode)
	}
}

func TestAnnotations_SetValidation_PathTraversal(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"path":"../etc/passwd","line":1,"comment":"test"}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal, got %d", resp.StatusCode)
	}
}

func TestAnnotations_SetValidation_InvalidJSON(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

func TestAnnotations_DeleteValidation_EmptyPath(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"path":"","line":3}`
	req, err := http.NewRequest("DELETE", ts.URL+"/api/annotations", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAnnotations_DeleteNonexistent(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Should succeed even if nothing to delete
	body := `{"path":"hello.go","line":999}`
	req, err := http.NewRequest("DELETE", ts.URL+"/api/annotations", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for deleting nonexistent annotation, got %d", resp.StatusCode)
	}
}

func TestAnnotations_GetForFile_Empty(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/annotations?path=hello.go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var annots map[string]annotationObj
	if err := json.NewDecoder(resp.Body).Decode(&annots); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(annots) != 0 {
		t.Errorf("expected empty annotations, got %v", annots)
	}
}

// ---- /api/git-status tests ----

func TestGitStatusEndpoint(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/git-status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	// Should return a JSON object (even if empty for non-git dirs)
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode git status: %v", err)
	}
}

// ---- Response format tests ----

func TestResponseHeaders_FileEndpoint(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/file?path=hello.go")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

func TestAnnotations_PostResponseFormat(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"path":"hello.go","line":1,"comment":"test comment"}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Read the full response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	// Should be valid JSON
	var result map[string]string
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("response should be valid JSON, got: %s, error: %v", string(respBody), err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result)
	}
}

func TestAnnotations_DeleteResponseFormat(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// First set an annotation
	body := `{"path":"hello.go","line":1,"comment":"test"}`
	resp, err := http.Post(ts.URL+"/api/annotations", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Now delete it
	body = `{"path":"hello.go","line":1}`
	req, err := http.NewRequest("DELETE", ts.URL+"/api/annotations", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]string
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("DELETE response should be valid JSON, got: %s, error: %v", string(respBody), err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result)
	}
}

// ---- Index page test ----

func TestIndexPage(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for index, got %d", resp.StatusCode)
	}
}

// ---- Helper functions ----

func flattenTreeNames(tree []map[string]interface{}) []string {
	var names []string
	for _, entry := range tree {
		if name, ok := entry["name"].(string); ok {
			names = append(names, name)
		}
		if children, ok := entry["children"].([]interface{}); ok {
			for _, child := range children {
				if childMap, ok := child.(map[string]interface{}); ok {
					names = append(names, flattenTreeNames([]map[string]interface{}{childMap})...)
				}
			}
		}
	}
	return names
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
