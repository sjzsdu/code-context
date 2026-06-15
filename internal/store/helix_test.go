package store

import (
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
