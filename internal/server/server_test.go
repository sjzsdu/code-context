package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/engine"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}

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

	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@example.com")
	runGit(t, tmpDir, "config", "user.name", "Test User")
	runGit(t, tmpDir, "add", "a.go", "b.go")
	runGit(t, tmpDir, "commit", "-m", "initial commit")

	dbPath := filepath.Join(tmpDir, "index.db")
	eng, err := engine.New(tmpDir, dbPath)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	if _, err = eng.Index(context.Background(), false); err != nil {
		t.Fatalf("failed to index test repo: %v", err)
	}

	if err := os.WriteFile(file1, []byte(file1Content+"\n// unstaged change\n"), 0o644); err != nil {
		t.Fatalf("failed to write unstaged change: %v", err)
	}
	if err := os.WriteFile(file2, []byte(file2Content+"\n// staged change\n"), 0o644); err != nil {
		t.Fatalf("failed to write staged change: %v", err)
	}
	runGit(t, tmpDir, "add", "b.go")

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

func TestSemanticSearchEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/semantic-search?q=foo")
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

func TestMapEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/map")
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
	if _, ok := payload["path"]; !ok {
		t.Fatalf("expected 'path' in response, got: %v", payload)
	}
}

func TestExplainEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/explain?file=a.go")
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
	if payload["path"] != "a.go" {
		t.Fatalf("expected path a.go, got: %v", payload["path"])
	}
}

func TestExplainMissingParam(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/explain")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestContextEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/context?name=Foo")
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
	if _, ok := payload["definition"]; !ok {
		t.Fatalf("expected 'definition' in response, got: %v", payload)
	}
}

func TestContextMissingParam(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/context")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestSnapshotEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/snapshot?q=Foo&limit=1")
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
	if payload["query"] != "Foo" {
		t.Fatalf("expected query Foo, got: %v", payload["query"])
	}
}

func TestSnapshotMissingParam(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/snapshot")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestTraceEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/trace?from=Foo&to=Foo")
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
	if payload["from"] != "Foo" || payload["to"] != "Foo" {
		t.Fatalf("unexpected trace payload: %v", payload)
	}
}

func TestTraceMissingParam(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/trace?from=Foo")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestDiffImpactEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/diff-impact?file=a.go&depth=2")
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
	if payload["file"] != "a.go" {
		t.Fatalf("expected file a.go, got: %v", payload["file"])
	}
}

func TestDiffImpactMissingParam(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/diff-impact")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
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

func TestGitFilesEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/git/files?state=unstaged")
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
	rawResults, ok := payload["results"].([]interface{})
	if !ok {
		t.Fatalf("expected array results, got: %T", payload["results"])
	}
	foundA := false
	for _, item := range rawResults {
		if s, ok := item.(string); ok && s == "a.go" {
			foundA = true
			break
		}
	}
	if !foundA {
		t.Fatalf("expected unstaged results to contain a.go, got: %v", rawResults)
	}
}

func TestGitFilesInvalidState(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/git/files?state=invalid")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGitDiffEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/git/diff?state=all&context=1")
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
	rawResults, ok := payload["results"].([]interface{})
	if !ok {
		t.Fatalf("expected array results, got: %T", payload["results"])
	}
	if len(rawResults) < 2 {
		t.Fatalf("expected at least two changed files in git diff, got: %d", len(rawResults))
	}
}

func TestSnapshotGitEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/snapshot-git?state=all&limit=2")
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
	if payload["query"] != "git:all" {
		t.Fatalf("expected query git:all, got: %v", payload["query"])
	}
	if summary, ok := payload["summary"].(string); !ok || !strings.Contains(summary, "changed files") {
		t.Fatalf("expected changed-files summary, got: %v", payload["summary"])
	}
}

func TestDiffImpactGitEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/diff-impact-git?state=all&depth=2")
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
	rawResults, ok := payload["results"].([]interface{})
	if !ok {
		t.Fatalf("expected array results, got: %T", payload["results"])
	}
	if len(rawResults) == 0 {
		t.Fatalf("expected at least one diff impact result, got none")
	}
}
