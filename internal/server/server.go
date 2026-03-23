package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/sjzsdu/code-memory/internal/api"
	"github.com/sjzsdu/code-memory/internal/engine"
)

type Server struct {
	eng  *engine.Engine
	port int
}

func New(eng *engine.Engine, port int) *Server {
	return &Server{eng: eng, port: port}
}

func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/symbols", s.handleFileSymbols)
	mux.HandleFunc("/api/definitions", s.handleDefinitions)
	mux.HandleFunc("/api/references", s.handleReferences)
	mux.HandleFunc("/api/text", s.handleTextSearch)
	mux.HandleFunc("/api/imports", s.handleImports)
	mux.HandleFunc("/api/importers", s.handleImporters)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/index", s.handleIndex)

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("github.com/sjzsdu/code-memory server listening on %s\n", addr)
	return http.ListenAndServe(addr, mux)
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

	results, err := s.eng.SearchSymbols(r.Context(), q, kind, limit)
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
