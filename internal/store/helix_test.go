package store

import (
	"reflect"
	"strings"
	"testing"

	helix "github.com/helixdb/helix-db/sdks/go"

	"github.com/sjzsdu/code-context/internal/api"
)

func TestHelixReplaceFileIndexRequestMarshals(t *testing.T) {
	projectID := "project-a"
	file := &api.FileInfo{
		Path:        "internal/example.go",
		Language:    api.Go,
		ContentHash: "hash",
		Size:        123,
	}
	symbols := symbolParamRows(projectID, file.Path, []api.Symbol{{
		Name:      "Handle",
		Kind:      api.Function,
		FilePath:  file.Path,
		Line:      10,
		EndLine:   20,
		Signature: "func Handle()",
	}})
	imports := importParamRows(projectID, file.Path, []api.ImportEdge{{
		FromFile: file.Path,
		ToSource: "fmt",
		Line:     3,
	}})
	calls := callParamRows(projectID, file.Path, []api.CallEdge{{
		FromFile:   file.Path,
		FromSymbol: "Handle",
		ToName:     "fmt.Println",
		Line:       12,
		Confidence: "HEURISTIC",
	}})
	routes := routeParamRows(projectID, file.Path, []api.Route{{
		FilePath:   file.Path,
		Method:     "GET",
		Path:       "/health",
		Handler:    "Handle",
		Framework:  "net/http",
		Line:       10,
		Confidence: "HEURISTIC",
	}})

	q := helix.WriteQuery("test_replace_file_index")
	project := q.ParamString("project_id", projectID)
	key := q.ParamString("key", helixKey(projectID, file.Path))
	path := q.ParamString("path", file.Path)
	language := q.ParamString("language", string(file.Language))
	contentHash := q.ParamString("content_hash", file.ContentHash)
	size := q.ParamI64("size", file.Size)
	indexedAt := q.ParamI64("indexed_at", 42)
	q.ParamArray("symbols", symbols, helix.ParamTypeObject())
	q.ParamArray("imports", imports, helix.ParamTypeObject())
	q.ParamArray("calls", calls, helix.ParamTypeObject())
	q.ParamArray("routes", routes, helix.ParamTypeObject())
	q.VarAs("existing", helix.G().NWithLabel(helixFileLabel).Where(helix.PredEq("key", key)))
	q.VarAs("drop_symbols", helix.G().N(helix.NodeVar("existing")).Out(helixDefinesEdge).Drop().Count())
	q.VarAs("drop_imports", helix.G().N(helix.NodeVar("existing")).Out(helixImportsEdge).Drop().Count())
	q.VarAs("drop_calls", helix.G().N(helix.NodeVar("existing")).Out(helixRecordsCallEdge).Drop().Count())
	q.VarAs("drop_routes", helix.G().N(helix.NodeVar("existing")).Out(helixDeclaresRouteEdge).Drop().Count())
	q.VarAs("drop_file", helix.G().N(helix.NodeVar("existing")).Drop().Count())
	q.VarAs("file", helix.G().AddN(helixFileLabel, helix.Props{
		helix.Prop("key", key),
		helix.Prop("project_id", project),
		helix.Prop("path", path),
		helix.Prop("language", language),
		helix.Prop("content_hash", contentHash),
		helix.Prop("size", size),
		helix.Prop("indexed_at", indexedAt),
	}))
	q.ForEachParam("symbols", symbolWriteBatch(helix.NodeVar("file")))
	q.ForEachParam("imports", importWriteBatch(helix.NodeVar("file")))
	q.ForEachParam("calls", callWriteBatch(helix.NodeVar("file")))
	q.ForEachParam("routes", routeWriteBatch(helix.NodeVar("file")))
	q.VarAs("file_id", helix.G().N(helix.NodeVar("file")).Project(helix.ProjectPropAs("$id", "id")))

	if _, err := helix.MarshalRequest(q.Returning("file_id")); err != nil {
		t.Fatalf("marshal replace file index request: %v", err)
	}
}

func TestHelixSearchSymbolsRequestMarshals(t *testing.T) {
	q := helix.ReadQuery("test_search_symbols")
	limitParam := q.ParamI64("limit", 20)
	projectParam := q.ParamString("project_id", "project-a")
	tr := helix.G().TextSearchNodesWith(helixSymbolLabel, "search_text", q.ParamString("query", "Handle").Input(), limitParam.Bound(), nil).
		Where(helix.PredEq("project_id", projectParam)).
		Where(helix.PredEq("kind", q.ParamString("kind", string(api.Function)))).
		Project(symbolProjections()...)

	if _, err := helix.MarshalRequest(q.VarAs("symbols", tr).Returning("symbols")); err != nil {
		t.Fatalf("marshal search symbols request: %v", err)
	}
}

func TestHelixTextSearchRequestMarshals(t *testing.T) {
	req, includeSymbols, includeDocuments, limit := helixTextSearchRequest("project-a", TextSearchQuery{
		Query: "Handle health",
		Filter: SearchFilter{
			TargetKinds: []TargetKind{TargetSymbol, TargetDocument},
		},
		Limit: 25,
	})
	if !includeSymbols || !includeDocuments {
		t.Fatalf("targets includeSymbols=%v includeDocuments=%v, want both true", includeSymbols, includeDocuments)
	}
	if limit != 25 {
		t.Fatalf("limit = %d, want 25", limit)
	}
	if _, err := helix.MarshalRequest(req); err != nil {
		t.Fatalf("marshal text search request: %v", err)
	}
}

func TestHelixTextRowsToHitsFiltersAndRanks(t *testing.T) {
	hits := helixTextRowsToHits("project-a",
		[]helixTextSymbolRow{{
			Name:       "HandleHealth",
			Kind:       string(api.Function),
			FilePath:   "internal/health.go",
			Line:       12,
			EndLine:    18,
			Signature:  "func HandleHealth()",
			SearchText: "HandleHealth internal/health.go function",
			Score:      0,
		}},
		[]helixTextDocumentRow{{
			Path:       "README.md",
			Title:      "Health endpoint",
			Summary:    "Documents the health endpoint",
			SearchText: "Health endpoint README.md",
			Score:      5,
		}},
		SearchFilter{FilePattern: "*.go"},
	)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Target.Kind != TargetSymbol || hits[0].Target.Path != "internal/health.go" || hits[0].Target.Line != 12 {
		t.Fatalf("unexpected hit target: %+v", hits[0].Target)
	}
	if hits[0].Score <= 0 {
		t.Fatalf("score = %v, want positive", hits[0].Score)
	}
}

func TestHelixVectorSearchRequestMarshals(t *testing.T) {
	req, limit := helixVectorSearchRequest("project-a", VectorSearchQuery{
		Vector:     []float32{0.1, 0.2, 0.3},
		Model:      "text-embedding-test",
		Dimensions: 3,
		Limit:      10,
	}, "text-embedding-test", 3)
	if limit != 10 {
		t.Fatalf("limit = %d, want 10", limit)
	}
	if _, err := helix.MarshalRequest(req); err != nil {
		t.Fatalf("marshal vector search request: %v", err)
	}
}

func TestHelixVectorRowsToHitsFiltersAndRanks(t *testing.T) {
	rows := []helixVectorChunkRow{
		{
			helixEmbeddingChunkRow: helixEmbeddingChunkRow{
				Key:               "a",
				Model:             "text-embedding-test",
				Dimensions:        3,
				ContentHash:       "hash-a",
				InputKind:         string(EmbeddingInputSymbol),
				TargetKind:        string(TargetSymbol),
				TargetPath:        "internal/health.go",
				TargetName:        "HealthHandler",
				TargetType:        string(api.Function),
				TargetLine:        12,
				TargetEndLine:     18,
				MetadataJSON:      `{"signature":"func HealthHandler()"}`,
				EmbeddingProperty: helixEmbeddingVectorProperty("text-embedding-test", 3),
			},
			Score: 0,
		},
		{
			helixEmbeddingChunkRow: helixEmbeddingChunkRow{
				Key:          "b",
				Model:        "text-embedding-test",
				Dimensions:   3,
				ContentHash:  "hash-b",
				InputKind:    string(EmbeddingInputDocument),
				TargetKind:   string(TargetDocument),
				TargetPath:   "README.md",
				TargetName:   "Readme",
				TargetType:   "document",
				MetadataJSON: `{"title":"Readme"}`,
			},
			Score: 2,
		},
	}
	hits := helixVectorRowsToHits("project-a", rows, SearchFilter{TargetKinds: []TargetKind{TargetSymbol}, FilePattern: "*.go"})
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Source != SearchSourceVector || hits[0].Target.Name != "HealthHandler" {
		t.Fatalf("unexpected hit: %+v", hits[0])
	}
	if hits[0].Metadata["model"] != "text-embedding-test" {
		t.Fatalf("metadata = %+v", hits[0].Metadata)
	}
}

func TestHelixEmbeddingVectorPropertyIsNamespaced(t *testing.T) {
	a := helixEmbeddingVectorProperty("model-a", 3)
	b := helixEmbeddingVectorProperty("model-b", 3)
	c := helixEmbeddingVectorProperty("model-a", 4)
	if a == b || a == c {
		t.Fatalf("property should vary by model and dimensions: %q %q %q", a, b, c)
	}
	if !strings.HasPrefix(a, "embedding_") {
		t.Fatalf("property = %q", a)
	}
}

func TestHelixEmbeddingKeyScopesByProject(t *testing.T) {
	if helixEmbeddingKey("project-a", "same") == helixEmbeddingKey("project-b", "same") {
		t.Fatalf("embedding cache key should be project-scoped")
	}
}

func TestDocumentSearchTextIncludesMetadata(t *testing.T) {
	got := documentSearchText(&api.Document{
		Path:     "README.md",
		Language: "markdown",
		Title:    "Health Checks",
		Summary:  "How to inspect service health",
	})
	for _, want := range []string{"README.md", "markdown", "Health Checks", "service health"} {
		if !strings.Contains(got, want) {
			t.Fatalf("documentSearchText() = %q, missing %q", got, want)
		}
	}
}

func TestSearchFilterAllowsProject(t *testing.T) {
	if !searchFilterAllowsProject("project-a", SearchFilter{}) {
		t.Fatal("empty project filter should allow current project")
	}
	if !searchFilterAllowsProject("project-a", SearchFilter{ProjectIDs: []string{"project-b", "project-a"}}) {
		t.Fatal("filter containing current project should allow it")
	}
	if searchFilterAllowsProject("project-a", SearchFilter{ProjectIDs: []string{"project-b"}}) {
		t.Fatal("filter without current project should reject it")
	}
}

func TestHelixStoreDetectsAdvancedCapabilities(t *testing.T) {
	got := DetectCapabilities(&helixStore{})
	want := []Capability{CapabilityEmbeddingCache, CapabilityGraphTraversal, CapabilityTextSearch, CapabilityVectorSearch}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectCapabilities(helixStore) = %#v, want %#v", got, want)
	}
}

func TestGraphTraversalBuilderDeduplicatesNodesAndEdges(t *testing.T) {
	builder := newGraphTraversalBuilder("project-a")
	from := TargetRef{Kind: TargetFile, Path: "internal/health.go"}
	to := TargetRef{Kind: TargetSymbol, Path: "internal/health.go", Name: "HealthMessage", Type: "function", Line: 3}

	builder.addEdge(from, to, GraphEdgeDefines, graphProperties("line", "3"), 0, 1, 1)
	builder.addEdge(from, to, GraphEdgeDefines, graphProperties("line", "3"), 0, 1, 1)

	result := builder.result()
	if len(result.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2: %+v", len(result.Nodes), result.Nodes)
	}
	if len(result.Edges) != 1 {
		t.Fatalf("edges = %d, want 1: %+v", len(result.Edges), result.Edges)
	}
	if result.Edges[0].Kind != GraphEdgeDefines {
		t.Fatalf("edge kind = %q, want %q", result.Edges[0].Kind, GraphEdgeDefines)
	}
	if result.Nodes[1].Depth != 1 || result.Edges[0].Depth != 1 {
		t.Fatalf("depth metadata missing: nodes=%+v edges=%+v", result.Nodes, result.Edges)
	}
}

func TestGraphDocumentLinkTargetRoute(t *testing.T) {
	target := graphDocumentLinkTarget(api.DocumentLink{TargetType: "route", TargetValue: "GET /health"})
	if target.Kind != TargetRoute || target.Method != "GET" || target.RoutePath != "/health" {
		t.Fatalf("unexpected route target: %+v", target)
	}
}

func TestGraphEdgeAllowed(t *testing.T) {
	allowed := graphEdgeKindSet([]GraphEdgeKind{GraphEdgeCalls})
	if !graphEdgeAllowed(allowed, GraphEdgeCalls) {
		t.Fatal("expected calls edge to be allowed")
	}
	if graphEdgeAllowed(allowed, GraphEdgeImports) {
		t.Fatal("expected imports edge to be rejected")
	}
	if !graphEdgeAllowed(nil, GraphEdgeImports) {
		t.Fatal("empty edge filter should allow imports")
	}
}

func TestNormalizeGraphEdgeKindsExpandsGroups(t *testing.T) {
	kinds, allowed, err := normalizeGraphEdgeKinds([]GraphEdgeKind{"code", "docs", GraphEdgeSimilar})
	if err != nil {
		t.Fatalf("normalizeGraphEdgeKinds: %v", err)
	}
	for _, want := range []GraphEdgeKind{GraphEdgeDefines, GraphEdgeImports, GraphEdgeCalls, GraphEdgeRoutes, GraphEdgeDocuments, GraphEdgeReferences, GraphEdgeSimilar} {
		if !graphEdgeAllowed(allowed, want) {
			t.Fatalf("expected %q in allowed set %#v", want, allowed)
		}
	}
	if len(kinds) != 7 {
		t.Fatalf("expanded kinds = %#v, want 7 unique kinds", kinds)
	}
}

func TestNormalizeGraphEdgeKindsRejectsUnknown(t *testing.T) {
	if _, _, err := normalizeGraphEdgeKinds([]GraphEdgeKind{"mystery"}); err == nil {
		t.Fatal("expected invalid graph edge kind error")
	}
}

func TestNormalizeGraphDirectionRejectsUnknown(t *testing.T) {
	if _, err := normalizeGraphDirection("sideways"); err == nil {
		t.Fatal("expected invalid graph direction error")
	}
}

func TestGraphTraversalFilterAllowsTarget(t *testing.T) {
	filter := SearchFilter{TargetKinds: []TargetKind{TargetSymbol}, FilePattern: "internal/*.go", Metadata: map[string]string{"type": "function"}}
	target := TargetRef{Kind: TargetSymbol, Path: "internal/health.go", Type: "function", Name: "HealthMessage"}
	if !graphTraversalFilterAllows("project-a", filter, target) {
		t.Fatal("expected target to pass traversal filter")
	}
	if graphTraversalFilterAllows("project-a", filter, TargetRef{Kind: TargetDocument, Path: "docs/health.md", Type: "document"}) {
		t.Fatal("expected document target to be rejected by symbol filter")
	}
}

func TestGraphTraversalPathsBuildsShortestPath(t *testing.T) {
	start := TargetRef{Kind: TargetDocument, Path: "docs/health.md"}
	symbol := TargetRef{Kind: TargetSymbol, Name: "HealthHandler"}
	callee := TargetRef{Kind: TargetSymbol, Name: "HealthMessage"}
	parents := map[string]graphPathHop{
		graphTargetKey(symbol): {from: start, to: symbol, edgeKind: GraphEdgeReferences, direction: GraphOutbound},
		graphTargetKey(callee): {from: symbol, to: callee, edgeKind: GraphEdgeCalls, direction: GraphOutbound},
	}
	paths := graphTraversalPaths(start, []GraphNode{{Target: start}, {Target: symbol}, {Target: callee}}, parents)
	if len(paths) != 3 {
		t.Fatalf("paths = %d, want 3: %+v", len(paths), paths)
	}
	if paths[2].Depth != 2 || len(paths[2].Steps) != 2 {
		t.Fatalf("callee path = %+v, want depth 2 with two steps", paths[2])
	}
	if paths[2].Steps[0].EdgeKind != GraphEdgeReferences || paths[2].Steps[1].EdgeKind != GraphEdgeCalls {
		t.Fatalf("unexpected path steps: %+v", paths[2].Steps)
	}
}

func TestNewHelixStoreDefaultsProjectID(t *testing.T) {
	st, err := NewHelixStore(HelixOptions{})
	if err != nil {
		t.Fatalf("NewHelixStore: %v", err)
	}
	hs, ok := st.(*helixStore)
	if !ok {
		t.Fatalf("store type = %T", st)
	}
	if hs.projectID != "default" {
		t.Fatalf("projectID = %q", hs.projectID)
	}
}
