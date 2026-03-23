package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/engine"
)

// setupTestServer builds a temporary code base, indexes it, and returns a
// test HTTP server plus a cleanup function.
func setupTestServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "cm-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	file1 := filepath.Join(tmpDir, "a.go")
	file1Content := "package a\nimport \"fmt\"\nfunc Foo() { fmt.Println(\"foo\") }"
	if err := os.WriteFile(file1, []byte(file1Content), 0o644); err != nil {
		t.Fatalf("failed to write go file: %v", err)
	}
	file2 := filepath.Join(tmpDir, "b.go")
	file2Content := "package a\nfunc Bar() int { return 42 }"
	if err := os.WriteFile(file2, []byte(file2Content), 0o644); err != nil {
		t.Fatalf("failed to write go file: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "index.db")
	eng, err := engine.New(tmpDir, dbPath)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	if _, err = eng.Index(context.Background(), false); err != nil {
		t.Fatalf("failed to index test repo: %v", err)
	}

	s := New(eng, 0)
	ts := httptest.NewServer(s.Handler())

	cleanup := func() {
		ts.Close()
		eng.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return ts, cleanup
}

func TestSearchEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/search?q=Foo")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("expected 'results' in response, got: %v", payload)
	}
}

func TestSearchMissingParam(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/search")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFileSymbolsEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	path := "/api/symbols?file=" + url.QueryEscape("a.go")
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("expected 'results' in response, got: %v", payload)
	}
}

func TestDefinitionsEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	resp, err := http.Get(ts.URL + "/api/definitions?name=Foo")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("expected 'results' in response, got: %v", payload)
	}
}

func TestReferencesEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	resp, err := http.Get(ts.URL + "/api/references?name=Foo")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("expected 'results' in response, got: %v", payload)
	}
}

func TestTextSearchEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	resp, err := http.Get(ts.URL + "/api/text?q=Foo")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("expected 'results' in response, got: %v", payload)
	}
}

func TestImportsEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	resp, err := http.Get(ts.URL + "/api/imports?file=a.go")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("expected 'results' in response, got: %v", payload)
	}
}

func TestImportersEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	resp, err := http.Get(ts.URL + "/api/importers?source=fmt")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("expected 'results' in response, got: %v", payload)
	}
}

func TestStatsEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	resp, err := http.Get(ts.URL + "/api/stats")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload api.IndexStats
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.TotalFiles < 0 {
		t.Fatalf("invalid stats: %+v", payload)
	}
}

func TestIndexEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/index", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload api.IndexStats
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestIndexEndpointWrongMethod(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	resp, err := http.Get(ts.URL + "/api/index")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}
