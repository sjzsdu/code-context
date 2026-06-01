package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/engine"
	"github.com/sjzsdu/code-context/internal/graphhtml"
)

type Server struct {
	eng  *engine.Engine
	port int
}

func New(eng *engine.Engine, port int) *Server {
	return &Server{eng: eng, port: port}
}

func (s *Server) Run() error {
	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("github.com/sjzsdu/code-context server listening on %s\n", addr)
	return http.ListenAndServe(addr, s.Handler())
}

// Handler returns the HTTP handler for testing and running the server without
// binding to a network port. This allows tests to exercise the API without
// starting a real server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/semantic-search", s.handleSemanticSearch)
	mux.HandleFunc("/api/symbols", s.handleFileSymbols)
	mux.HandleFunc("/api/definitions", s.handleDefinitions)
	mux.HandleFunc("/api/references", s.handleReferences)
	mux.HandleFunc("/api/text", s.handleTextSearch)
	mux.HandleFunc("/api/imports", s.handleImports)
	mux.HandleFunc("/api/importers", s.handleImporters)
	mux.HandleFunc("/api/callers", s.handleCallers)
	mux.HandleFunc("/api/callees", s.handleCallees)
	mux.HandleFunc("/api/routes", s.handleRoutes)
	mux.HandleFunc("/api/route-context", s.handleRouteContext)
	mux.HandleFunc("/api/docs-for", s.handleDocsFor)
	mux.HandleFunc("/api/doc-drift", s.handleDocDrift)
	mux.HandleFunc("/api/doc-coverage", s.handleDocCoverage)
	mux.HandleFunc("/api/map", s.handleMap)
	mux.HandleFunc("/api/graph", s.handleGraph)
	mux.HandleFunc("/api/graph/html", s.handleGraphHTML)
	mux.HandleFunc("/api/graph/path", s.handleGraphPath)
	mux.HandleFunc("/api/graph/neighbors", s.handleGraphNeighbors)
	mux.HandleFunc("/api/graph/subgraph", s.handleGraphSubgraph)
	mux.HandleFunc("/api/explain", s.handleExplain)
	mux.HandleFunc("/api/context", s.handleContext)
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("/api/git/files", s.handleGitFiles)
	mux.HandleFunc("/api/git/diff", s.handleGitDiff)
	mux.HandleFunc("/api/snapshot-git", s.handleSnapshotGit)
	mux.HandleFunc("/api/trace", s.handleTrace)
	mux.HandleFunc("/api/impact", s.handleImpact)
	mux.HandleFunc("/api/diff-impact", s.handleDiffImpact)
	mux.HandleFunc("/api/diff-impact-git", s.handleDiffImpactGit)
	mux.HandleFunc("/api/review-context", s.handleReviewContext)
	mux.HandleFunc("/api/test-impact", s.handleTestImpact)
	mux.HandleFunc("/api/symbol-impact", s.handleSymbolImpact)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/freshness", s.handleFreshness)
	mux.HandleFunc("/api/doctor", s.handleDoctor)
	mux.HandleFunc("/api/rebuild", s.handleRebuild)
	mux.HandleFunc("/api/index", s.handleIndex)
	return mux
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	s.handleSearchWithMode(w, r, r.URL.Query().Get("hybrid") == "true")
}

func (s *Server) handleSemanticSearch(w http.ResponseWriter, r *http.Request) {
	s.handleSearchWithMode(w, r, true)
}

func (s *Server) handleSearchWithMode(w http.ResponseWriter, r *http.Request, hybrid bool) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, fmt.Errorf("missing 'q' parameter"), 400)
		return
	}
	kindParam := r.URL.Query().Get("kind")
	var kind *api.SymbolKind
	if kindParam != "" {
		k := api.SymbolKind(kindParam)
		kind = &k
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	var (
		results []api.Symbol
		err     error
	)
	if hybrid {
		results, err = s.eng.SearchSymbolsHybrid(r.Context(), q, kind, limit)
	} else {
		results, err = s.eng.SearchSymbols(r.Context(), q, kind, limit)
	}
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleFileSymbols(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("file")
	if path == "" {
		writeError(w, fmt.Errorf("missing 'file' parameter"), 400)
		return
	}
	results, err := s.eng.FileSymbols(r.Context(), path)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleDefinitions(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, fmt.Errorf("missing 'name' parameter"), 400)
		return
	}
	results, err := s.eng.FindDef(r.Context(), name)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, fmt.Errorf("missing 'name' parameter"), 400)
		return
	}
	results, err := s.eng.FindRefs(r.Context(), name)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleTextSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, fmt.Errorf("missing 'q' parameter"), 400)
		return
	}
	pattern := r.URL.Query().Get("file")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	results, err := s.eng.SearchText(r.Context(), q, pattern, limit)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleImports(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		writeError(w, fmt.Errorf("missing 'file' parameter"), 400)
		return
	}
	results, err := s.eng.Imports(r.Context(), file)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleImporters(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		writeError(w, fmt.Errorf("missing 'source' parameter"), 400)
		return
	}
	results, err := s.eng.Importers(r.Context(), source)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleCallers(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, fmt.Errorf("missing 'name' parameter"), 400)
		return
	}
	results, err := s.eng.Callers(r.Context(), name)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleCallees(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, fmt.Errorf("missing 'name' parameter"), 400)
		return
	}
	results, err := s.eng.Callees(r.Context(), name)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	results, err := s.eng.Routes(r.Context(), query)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleRouteContext(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, fmt.Errorf("missing 'q' parameter"), 400)
		return
	}
	result, err := s.eng.RouteContext(r.Context(), query)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDocsFor(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, fmt.Errorf("missing 'q' parameter"), 400)
		return
	}
	result, err := s.eng.DocsFor(r.Context(), query)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDocDrift(w http.ResponseWriter, r *http.Request) {
	result, err := s.eng.DocDrift(r.Context())
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDocCoverage(w http.ResponseWriter, r *http.Request) {
	result, err := s.eng.DocCoverage(r.Context())
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.eng.Stats(r.Context())
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, stats)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.eng.Status(r.Context())
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, status)
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	report, err := s.eng.Doctor(r.Context())
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, report)
}

func (s *Server) handleFreshness(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 0 {
		limit = 0
	}
	report, err := s.eng.Freshness(r.Context(), limit)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, report)
}

func (s *Server) handleRebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, fmt.Errorf("POST only"), 405)
		return
	}
	stats, err := s.eng.Rebuild(r.Context(), false)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, stats)
}

func (s *Server) handleMap(w http.ResponseWriter, r *http.Request) {
	result, err := s.eng.Map(r.Context())
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	focus := r.URL.Query().Get("focus")
	result, err := s.eng.ExportGraph(r.Context(), focus)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleGraphHTML(w http.ResponseWriter, r *http.Request) {
	focus := r.URL.Query().Get("focus")
	result, err := s.eng.ExportGraph(r.Context(), focus)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := writeGraphHTMLPage(w, s.eng.Root(), result); err != nil {
		writeError(w, err, 500)
		return
	}
}

func (s *Server) handleGraphPath(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	if from == "" {
		writeError(w, fmt.Errorf("missing 'from' parameter"), 400)
		return
	}
	to := r.URL.Query().Get("to")
	if to == "" {
		writeError(w, fmt.Errorf("missing 'to' parameter"), 400)
		return
	}

	result, err := s.eng.GraphPath(r.Context(), from, to)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleGraphNeighbors(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		writeError(w, fmt.Errorf("missing 'target' parameter"), 400)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := s.eng.GraphNeighbors(r.Context(), target, limit)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleGraphSubgraph(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		writeError(w, fmt.Errorf("missing 'target' parameter"), 400)
		return
	}
	depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
	result, err := s.eng.GraphSubgraph(r.Context(), target, depth)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		writeError(w, fmt.Errorf("missing 'file' parameter"), 400)
		return
	}

	result, err := s.eng.Explain(r.Context(), file)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, fmt.Errorf("missing 'name' parameter"), 400)
		return
	}

	result, err := s.eng.Context(r.Context(), name)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, fmt.Errorf("missing 'q' parameter"), 400)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := s.eng.Snapshot(r.Context(), q, limit)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleTrace(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	if from == "" {
		writeError(w, fmt.Errorf("missing 'from' parameter"), 400)
		return
	}

	to := r.URL.Query().Get("to")
	if to == "" {
		writeError(w, fmt.Errorf("missing 'to' parameter"), 400)
		return
	}

	result, err := s.eng.Trace(r.Context(), from, to)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleGitFiles(w http.ResponseWriter, r *http.Request) {
	state, err := engine.ParseGitState(r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, err, 400)
		return
	}

	results, err := s.eng.GitChangedFiles(r.Context(), state)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	state, err := engine.ParseGitState(r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, err, 400)
		return
	}

	contextLines, _ := strconv.Atoi(r.URL.Query().Get("context"))
	if contextLines < 0 {
		contextLines = 0
	}

	results, err := s.eng.GitDiff(r.Context(), state, contextLines)
	if err != nil {
		writeError(w, err, 500)
		return
	}

	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleSnapshotGit(w http.ResponseWriter, r *http.Request) {
	state, err := engine.ParseGitState(r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, err, 400)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := s.eng.SnapshotGit(r.Context(), state, limit)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDiffImpact(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		writeError(w, fmt.Errorf("missing 'file' parameter"), 400)
		return
	}

	depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
	result, err := s.eng.DiffImpact(r.Context(), file, depth)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleImpact(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		writeError(w, fmt.Errorf("missing 'target' parameter"), 400)
		return
	}

	depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
	result, err := s.eng.Impact(r.Context(), target, depth)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDiffImpactGit(w http.ResponseWriter, r *http.Request) {
	state, err := engine.ParseGitState(r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, err, 400)
		return
	}

	depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
	results, err := s.eng.DiffImpactGit(r.Context(), state, depth)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"results": results, "count": len(results)})
}

func (s *Server) handleReviewContext(w http.ResponseWriter, r *http.Request) {
	state, err := engine.ParseGitState(r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, err, 400)
		return
	}

	result, err := s.eng.ReviewContext(r.Context(), state)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleTestImpact(w http.ResponseWriter, r *http.Request) {
	state, err := engine.ParseGitState(r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, err, 400)
		return
	}

	result, err := s.eng.TestImpact(r.Context(), state)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleSymbolImpact(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, fmt.Errorf("missing 'name' parameter"), 400)
		return
	}

	result, err := s.eng.SymbolImpact(r.Context(), name)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func writeGraphHTMLPage(w interface{ Write([]byte) (int, error) }, root string, graph *api.GraphExport) error {
	return graphhtml.Render(w, root, graph)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, fmt.Errorf("POST only"), 405)
		return
	}
	incremental := r.URL.Query().Get("incremental") == "true"
	var stats *api.IndexStats
	var err error
	if incremental {
		stats, err = s.eng.IndexIncremental(r.Context(), false)
	} else {
		stats, err = s.eng.Index(r.Context(), false)
	}
	if err != nil {
		writeError(w, err, 500)
		return
	}
	writeJSON(w, stats)
}
