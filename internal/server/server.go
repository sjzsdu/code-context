package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/engine"
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
	mux.HandleFunc("/api/diff-impact", s.handleDiffImpact)
	mux.HandleFunc("/api/diff-impact-git", s.handleDiffImpactGit)
	mux.HandleFunc("/api/stats", s.handleStats)
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

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.eng.Stats(r.Context())
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
	if err := writeGraphHTMLPage(w, result); err != nil {
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

func writeGraphHTMLPage(w interface{ Write([]byte) (int, error) }, graph *api.GraphExport) error {
	payload, err := json.Marshal(graph)
	if err != nil {
		return err
	}
	view := struct {
		Title       string
		Focus       string
		Summary     string
		NodeCount   int
		EdgeCount   int
		GraphJSON   template.JS
		HasAnalysis bool
		Analysis    *api.GraphAnalysis
	}{
		Title:       "code-context graph view",
		Focus:       graph.Focus,
		Summary:     graph.Summary,
		NodeCount:   len(graph.Nodes),
		EdgeCount:   len(graph.Edges),
		GraphJSON:   template.JS(payload),
		HasAnalysis: graph.Analysis != nil,
		Analysis:    graph.Analysis,
	}
	return graphHTMLTemplate.Execute(w, view)
}

var graphHTMLTemplate = template.Must(template.New("graph-html").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>{{.Title}}</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 0; background: #0b1020; color: #e5e7eb; }
    header { padding: 20px 24px; border-bottom: 1px solid #1f2937; background: #111827; }
    main { display: grid; grid-template-columns: 360px 1fr; min-height: calc(100vh - 89px); }
    aside { padding: 20px 24px; border-right: 1px solid #1f2937; background: #0f172a; overflow: auto; }
    section { padding: 20px 24px; overflow: auto; }
    h1, h2, h3 { margin-top: 0; }
    .meta { color: #94a3b8; font-size: 14px; }
    .pill { display: inline-block; margin: 4px 6px 0 0; padding: 4px 8px; border-radius: 999px; background: #1f2937; color: #cbd5e1; font-size: 12px; }
    .toolbar { display: flex; gap: 12px; align-items: center; margin-bottom: 16px; flex-wrap: wrap; }
    input, select { padding: 8px 10px; border-radius: 8px; border: 1px solid #334155; background: #020617; color: #e5e7eb; }
    .card { padding: 14px 16px; border: 1px solid #1f2937; border-radius: 12px; background: #111827; margin-bottom: 12px; }
    .node { cursor: pointer; }
    .node:hover { background: #172554; }
    ul { padding-left: 18px; }
    code { color: #93c5fd; }
    pre { background: #020617; padding: 12px; border-radius: 10px; overflow: auto; }
    .muted { color: #94a3b8; }
  </style>
</head>
<body>
  <header>
    <h1>{{.Title}}</h1>
    <div class="meta">{{.Summary}}</div>
    <div class="meta">Nodes: {{.NodeCount}} · Edges: {{.EdgeCount}}{{if .Focus}} · Focus: <code>{{.Focus}}</code>{{end}}</div>
  </header>
  <main>
    <aside>
      <div class="card">
        <h2>Filters</h2>
        <div class="toolbar">
          <input id="search" type="search" placeholder="Search nodes">
          <select id="typeFilter">
            <option value="">All node types</option>
          </select>
        </div>
        <div class="meta">Click a node to inspect its connected edges.</div>
      </div>
      {{if .HasAnalysis}}
      <div class="card">
        <h2>Graph analysis</h2>
        {{if .Analysis.TopImports}}
        <h3>Top imports</h3>
        <ul>{{range .Analysis.TopImports}}<li>{{.Name}} ({{.Count}})</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.MostConnectedFiles}}
        <h3>Most connected files</h3>
        <ul>{{range .Analysis.MostConnectedFiles}}<li>{{.Name}} ({{.Count}})</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.BridgeFiles}}
        <h3>Bridge files</h3>
        <ul>{{range .Analysis.BridgeFiles}}<li>{{.Name}} ({{.Count}})</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.HotspotFiles}}
        <h3>Hotspot files</h3>
        <ul>{{range .Analysis.HotspotFiles}}<li>{{.Name}} ({{.Count}})</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.RelationHighlights}}
        <h3>Relation highlights</h3>
        <ul>{{range .Analysis.RelationHighlights}}<li>{{.}}</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.ReadingPaths}}
        <h3>Reading paths</h3>
        <ul>{{range .Analysis.ReadingPaths}}<li><strong>{{.Entry}}</strong>: {{range $i, $part := .Path}}{{if $i}} → {{end}}{{$part}}{{end}}{{if .Reason}}<div class="meta">{{.Reason}}</div>{{end}}</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.RecommendedFiles}}
        <h3>Recommended files</h3>
        <ul>{{range .Analysis.RecommendedFiles}}<li>{{.}}</li>{{end}}</ul>
        {{end}}
      </div>
      {{end}}
      <div id="nodeList"></div>
    </aside>
    <section>
      <div class="card">
        <h2>Selected node</h2>
        <div id="details" class="muted">Pick a node from the list to inspect its attributes and edges.</div>
      </div>
      <div class="card">
        <h2>Graph payload</h2>
        <pre id="raw"></pre>
      </div>
    </section>
  </main>
  <script>
    const graph = {{.GraphJSON}};
    const search = document.getElementById('search');
    const typeFilter = document.getElementById('typeFilter');
    const nodeList = document.getElementById('nodeList');
    const details = document.getElementById('details');
    const raw = document.getElementById('raw');
    raw.textContent = JSON.stringify(graph, null, 2);

    const types = [...new Set(graph.nodes.map(node => node.type))].sort();
    for (const type of types) {
      const option = document.createElement('option');
      option.value = type;
      option.textContent = type;
      typeFilter.appendChild(option);
    }

    function renderList() {
      const q = search.value.trim().toLowerCase();
      const type = typeFilter.value;
      nodeList.innerHTML = '';
      const filtered = graph.nodes.filter(node => {
        if (type && node.type !== type) return false;
        if (!q) return true;
        return [node.label, node.name, node.file].filter(Boolean).join(' ').toLowerCase().includes(q);
      });
      if (!filtered.length) {
        nodeList.innerHTML = '<div class="card muted">No nodes match the current filters.</div>';
        return;
      }
      for (const node of filtered) {
        const el = document.createElement('div');
        el.className = 'card node';
        const label = node.label || node.id;
        const meta = node.type + (node.file ? ' · ' + node.file : '');
        el.innerHTML = '<strong>' + label + '</strong><div class="meta">' + meta + '</div>';
        el.addEventListener('click', () => renderDetails(node));
        nodeList.appendChild(el);
      }
    }

    function renderDetails(node) {
      const incoming = graph.edges.filter(edge => edge.target === node.id);
      const outgoing = graph.edges.filter(edge => edge.source === node.id);
      details.innerHTML = '';
      const title = document.createElement('div');
      title.innerHTML = '<h3>' + (node.label || node.id) + '</h3><div class="meta">' + node.type + '</div>';
      details.appendChild(title);

      const attrs = document.createElement('div');
      attrs.innerHTML = [
        node.file ? '<span class="pill">file: ' + node.file + '</span>' : '',
        node.name ? '<span class="pill">name: ' + node.name + '</span>' : '',
        node.kind ? '<span class="pill">kind: ' + node.kind + '</span>' : '',
        node.language ? '<span class="pill">language: ' + node.language + '</span>' : '',
        node.line ? '<span class="pill">line: ' + node.line + '</span>' : ''
      ].join('');
      details.appendChild(attrs);

      const edges = document.createElement('div');
      const outgoingHTML = outgoing.map(edge => '<li>' + edge.type + ' → ' + edge.target + '</li>').join('');
      const incomingHTML = incoming.map(edge => '<li>' + edge.type + ' ← ' + edge.source + '</li>').join('');
      edges.innerHTML = '<h3>Outgoing (' + outgoing.length + ')</h3><ul>' + outgoingHTML + '</ul><h3>Incoming (' + incoming.length + ')</h3><ul>' + incomingHTML + '</ul>';
      details.appendChild(edges);
    }

    search.addEventListener('input', renderList);
    typeFilter.addEventListener('change', renderList);
    renderList();
  </script>
</body>
</html>`))

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
