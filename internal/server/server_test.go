package server

import (
	"context"
	"encoding/json"
	"io"
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
	file1Content := strings.Join([]string{
		"package a",
		"",
		"import (",
		"\t\"fmt\"",
		"\t\"net/http\"",
		")",
		"",
		"func Foo() {",
		"\tfmt.Println(\"foo\")",
		"\tBar()",
		"}",
		"",
		"func init() {",
		"\thttp.HandleFunc(\"/foo\", FooHandler)",
		"}",
		"",
		"func FooHandler(w http.ResponseWriter, r *http.Request) {",
		"\tFoo()",
		"}",
	}, "\n")
	if err := os.WriteFile(file1, []byte(file1Content), 0o644); err != nil {
		t.Fatalf("failed to write go file: %v", err)
	}
	file2 := filepath.Join(tmpDir, "b.go")
	file2Content := strings.Join([]string{
		"package a",
		"",
		"import \"fmt\"",
		"",
		"func Bar() int {",
		"\tfmt.Println(\"bar\")",
		"\treturn 42",
		"}",
	}, "\n")
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
	analysis, ok := payload["analysis"].(map[string]interface{})
	if !ok || analysis == nil {
		t.Fatalf("expected analysis in map response, got: %v", payload)
	}
	if _, ok := analysis["bridge_files"]; !ok {
		t.Fatalf("expected bridge_files in map analysis, got: %v", analysis)
	}
	if _, ok := analysis["hotspot_files"]; !ok {
		t.Fatalf("expected hotspot_files in map analysis, got: %v", analysis)
	}
	if _, ok := analysis["reading_paths"]; !ok {
		t.Fatalf("expected reading_paths in map analysis, got: %v", analysis)
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
	if payload.IndexVersion == "" {
		t.Fatalf("expected index version in stats payload, got %+v", payload)
	}
	if payload.LastIndexedAt == "" {
		t.Fatalf("expected last indexed timestamp in stats payload, got %+v", payload)
	}
}

func TestStatusEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	resp, err := http.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload api.ServiceStatus
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.Root == "" {
		t.Fatalf("expected root in service status, got %+v", payload)
	}
	if payload.GraphVersion == "" {
		t.Fatalf("expected graph version in service status, got %+v", payload)
	}
	if payload.Index == nil || payload.Index.IndexVersion == "" {
		t.Fatalf("expected index metadata in service status, got %+v", payload)
	}
	if payload.Watch == nil {
		t.Fatalf("expected watch metadata in service status, got %+v", payload)
	}
}

func TestGraphEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/graph")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload api.GraphExport
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.Version == "" {
		t.Fatalf("expected graph version in response")
	}
	if payload.Version != "graph-export.v2" {
		t.Fatalf("expected graph-export.v2, got %q", payload.Version)
	}
	if len(payload.Nodes) == 0 {
		t.Fatalf("expected graph nodes in response")
	}
	if len(payload.Edges) == 0 {
		t.Fatalf("expected graph edges in response")
	}
	if payload.Analysis == nil {
		t.Fatalf("expected graph analysis in response")
	}
	if len(payload.Analysis.TopImports) == 0 {
		t.Fatalf("expected top imports analysis in response")
	}
	var hasModule, hasPackage, hasDeclaresPackage, hasRepresents bool
	for _, node := range payload.Nodes {
		switch node.Type {
		case "module":
			hasModule = true
		case "package":
			hasPackage = true
		}
	}
	for _, edge := range payload.Edges {
		switch edge.Type {
		case "declares_package":
			hasDeclaresPackage = true
		case "represents":
			hasRepresents = true
		}
	}
	if !hasModule || !hasPackage {
		t.Fatalf("expected module and package nodes in response: %+v", payload.Nodes)
	}
	if !hasDeclaresPackage || !hasRepresents {
		t.Fatalf("expected declares_package and represents edges in response: %+v", payload.Edges)
	}
}

func TestGraphEndpointWithFocus(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/graph?focus=a.go")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var payload map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		t.Fatalf("expected 200, got %d payload=%v", resp.StatusCode, payload)
	}

	var payload api.GraphExport
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.Focus != "a.go" {
		t.Fatalf("expected focus to be preserved, got %q", payload.Focus)
	}
	for _, node := range payload.Nodes {
		if node.Type == "file" && node.FilePath != "a.go" {
			t.Fatalf("expected focused graph to include only a.go file nodes, got %q", node.FilePath)
		}
	}
}

func TestGraphPathEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/graph/path?from=a.go&to=b.go")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var payload map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		t.Fatalf("expected 200, got %d payload=%v", resp.StatusCode, payload)
	}

	var payload api.GraphPathResult
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.FromFile != "a.go" || payload.ToFile != "b.go" {
		t.Fatalf("unexpected graph path endpoints: %+v", payload)
	}
	if !payload.PathFound {
		t.Fatalf("expected path to be found, got %+v", payload)
	}
	if len(payload.Files) != 2 || payload.Files[0] != "a.go" || payload.Files[1] != "b.go" {
		t.Fatalf("unexpected graph path files: %+v", payload.Files)
	}
}

func TestGraphPathEndpointMissingParam(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/graph/path?from=Foo")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGraphNeighborsEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/graph/neighbors?target=Foo&limit=2")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var payload map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		t.Fatalf("expected 200, got %d payload=%v", resp.StatusCode, payload)
	}

	var payload api.GraphNeighborsResult
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.ResolvedFile != "a.go" {
		t.Fatalf("expected resolved file a.go, got %+v", payload)
	}
	if len(payload.Symbols) == 0 {
		t.Fatalf("expected symbol neighbors, got %+v", payload)
	}
	if len(payload.Imports) == 0 || payload.Imports[0] != "fmt" {
		t.Fatalf("expected import neighbors, got %+v", payload.Imports)
	}
}

func TestGraphSubgraphEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/graph/subgraph?target=Foo&depth=1")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var payload map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		t.Fatalf("expected 200, got %d payload=%v", resp.StatusCode, payload)
	}

	var payload api.GraphSubgraphResult
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.ResolvedFile != "a.go" {
		t.Fatalf("expected resolved file a.go, got %+v", payload)
	}
	if payload.Depth != 1 {
		t.Fatalf("expected depth 1, got %+v", payload)
	}
	if len(payload.Files) != 2 || payload.Files[0] != "a.go" || payload.Files[1] != "b.go" {
		t.Fatalf("unexpected subgraph files: %+v", payload.Files)
	}
	if payload.Graph == nil || len(payload.Graph.Nodes) == 0 || len(payload.Graph.Edges) == 0 {
		t.Fatalf("expected subgraph graph payload, got %+v", payload.Graph)
	}
}

func TestGraphHTMLEndpoint(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/graph/html?focus=a.go")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("expected text/html content type, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "<!DOCTYPE html>") {
		t.Fatalf("expected html doctype, got:\n%s", text)
	}
	if !strings.Contains(text, "code-context graph view") {
		t.Fatalf("expected graph html title, got:\n%s", text)
	}
	if !strings.Contains(text, "graphSurface") {
		t.Fatalf("expected visual graph svg in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Document mode") {
		t.Fatalf("expected document mode controls in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Zoom to fit") {
		t.Fatalf("expected graph canvas controls in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Focus depth") {
		t.Fatalf("expected focus depth controls in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "minimapCanvas") {
		t.Fatalf("expected minimap canvas in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "hoverCard") {
		t.Fatalf("expected hover card support in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Shift + drag to box zoom") {
		t.Fatalf("expected marquee zoom hint in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "minimapButton") {
		t.Fatalf("expected clickable minimap control in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "nodeContextMenu") {
		t.Fatalf("expected node context menu support in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "contentModal") {
		t.Fatalf("expected content modal support in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Open node content") {
		t.Fatalf("expected node content action in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Copy content") {
		t.Fatalf("expected copy content action in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Expand view") {
		t.Fatalf("expected expand content action in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Show file path") {
		t.Fatalf("expected file path action in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "selectionActions") {
		t.Fatalf("expected visible selection actions in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Open content") {
		t.Fatalf("expected selection open content action in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "Pin node") {
		t.Fatalf("expected selection pin action in html output, got:\n%s", text)
	}
	if !strings.Contains(text, "a.go") {
		t.Fatalf("expected focused file in html output, got:\n%s", text)
	}
}

func TestGraphNeighborsEndpointMissingParam(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/graph/neighbors")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGraphSubgraphEndpointMissingParam(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/graph/subgraph")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
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
		var payload map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		t.Fatalf("expected 200, got %d payload=%v", resp.StatusCode, payload)
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
		var payload map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		t.Fatalf("expected 200, got %d payload=%v", resp.StatusCode, payload)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := payload["definition"]; !ok {
		t.Fatalf("expected 'definition' in response, got: %v", payload)
	}
	if payload["graph_summary"] == "" {
		t.Fatalf("expected graph_summary in context response, got: %v", payload)
	}
	if _, ok := payload["related_files"]; !ok {
		t.Fatalf("expected related_files in context response, got: %v", payload)
	}
	analysis, ok := payload["analysis"].(map[string]interface{})
	if !ok || analysis == nil {
		t.Fatalf("expected analysis in context response, got: %v", payload)
	}
	if _, ok := analysis["bridge_files"]; !ok {
		t.Fatalf("expected bridge_files in context analysis, got: %v", analysis)
	}
	if _, ok := analysis["relation_highlights"]; !ok {
		t.Fatalf("expected relation_highlights in context analysis, got: %v", analysis)
	}
	if _, ok := analysis["reading_paths"]; !ok {
		t.Fatalf("expected reading_paths in context analysis, got: %v", analysis)
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
	if payload["analysis"] == nil {
		t.Fatalf("expected analysis in snapshot response, got: %v", payload)
	}
	if _, ok := payload["recommended_files"]; !ok {
		t.Fatalf("expected recommended_files in snapshot response, got: %v", payload)
	}
	analysis, ok := payload["analysis"].(map[string]interface{})
	if !ok || analysis == nil {
		t.Fatalf("expected structured analysis in snapshot response, got: %v", payload["analysis"])
	}
	if _, ok := analysis["bridge_files"]; !ok {
		t.Fatalf("expected bridge_files in snapshot analysis, got: %v", analysis)
	}
	if _, ok := analysis["hotspot_files"]; !ok {
		t.Fatalf("expected hotspot_files in snapshot analysis, got: %v", analysis)
	}
	if _, ok := analysis["reading_paths"]; !ok {
		t.Fatalf("expected reading_paths in snapshot analysis, got: %v", analysis)
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
	if payload["analysis"] == nil {
		t.Fatalf("expected analysis in snapshot-git response, got: %v", payload)
	}
	analysis, ok := payload["analysis"].(map[string]interface{})
	if !ok || analysis == nil {
		t.Fatalf("expected structured analysis in snapshot-git response, got: %v", payload["analysis"])
	}
	if _, ok := analysis["bridge_files"]; !ok {
		t.Fatalf("expected bridge_files in snapshot-git analysis, got: %v", analysis)
	}
	if _, ok := analysis["relation_highlights"]; !ok {
		t.Fatalf("expected relation_highlights in snapshot-git analysis, got: %v", analysis)
	}
	if _, ok := payload["recommended_files"]; !ok {
		t.Fatalf("expected recommended_files in snapshot-git response, got: %v", payload)
	}
	files, ok := payload["files"].([]interface{})
	if !ok || len(files) == 0 {
		t.Fatalf("expected files in snapshot-git response, got: %v", payload["files"])
	}
	firstFile, ok := files[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first file object, got: %T", files[0])
	}
	if firstFile["graph_summary"] == nil || firstFile["graph_summary"] == "" {
		t.Fatalf("expected graph_summary in snapshot-git file, got: %v", firstFile)
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

func TestNewAnalysisEndpoints(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	cases := []struct {
		name       string
		path       string
		wantFields []string
	}{
		{name: "callers", path: "/api/callers?name=Bar", wantFields: []string{"results", "count"}},
		{name: "callees", path: "/api/callees?name=Foo", wantFields: []string{"results", "count"}},
		{name: "routes", path: "/api/routes?q=foo", wantFields: []string{"results", "count"}},
		{name: "docs-for", path: "/api/docs-for?q=Foo", wantFields: []string{"query", "links"}},
		{name: "doc-drift", path: "/api/doc-drift", wantFields: []string{"total_links", "broken", "summary"}},
		{name: "review-context", path: "/api/review-context?state=all", wantFields: []string{"changed_files", "risk", "summary"}},
		{name: "test-impact", path: "/api/test-impact?state=all", wantFields: []string{"changed_files", "recommended_tests", "summary"}},
		{name: "symbol-impact", path: "/api/symbol-impact?name=Foo", wantFields: []string{"symbol", "risk", "summary"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tc.path)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
			}
			var payload map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			for _, field := range tc.wantFields {
				if _, ok := payload[field]; !ok {
					t.Fatalf("expected %q in response, got: %v", field, payload)
				}
			}
		})
	}
}

func TestNewAnalysisEndpointsMissingRequiredParams(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	for _, path := range []string{"/api/callers", "/api/callees", "/api/docs-for", "/api/symbol-impact"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", path, resp.StatusCode)
		}
	}
}
