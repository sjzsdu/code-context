package search

import (
	"context"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
)

type searchMockStore struct {
	searchResults []api.Symbol
	files         []*api.FileInfo
	fileSymbols   map[string][]api.Symbol
}

func (m *searchMockStore) Init(ctx context.Context) error { return nil }
func (m *searchMockStore) UpsertFile(ctx context.Context, f *api.FileInfo) (int64, error) {
	return 0, nil
}
func (m *searchMockStore) GetFile(ctx context.Context, path string) (*api.FileInfo, error) {
	return nil, nil
}
func (m *searchMockStore) DeleteFile(ctx context.Context, path string) error { return nil }
func (m *searchMockStore) ListFiles(ctx context.Context, lang *api.Language) ([]*api.FileInfo, error) {
	return m.files, nil
}
func (m *searchMockStore) ReplaceSymbols(ctx context.Context, fileID int64, symbols []api.Symbol) error {
	return nil
}
func (m *searchMockStore) ReplaceImports(ctx context.Context, fileID int64, imports []api.ImportEdge) error {
	return nil
}
func (m *searchMockStore) SearchSymbols(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error) {
	return m.searchResults, nil
}
func (m *searchMockStore) FindDefinitions(ctx context.Context, name string) ([]api.Symbol, error) {
	return nil, nil
}
func (m *searchMockStore) FindReferences(ctx context.Context, name string) ([]api.Symbol, error) {
	return nil, nil
}
func (m *searchMockStore) GetFileSymbols(ctx context.Context, path string) ([]api.Symbol, error) {
	return m.fileSymbols[path], nil
}
func (m *searchMockStore) GetImports(ctx context.Context, filePath string) ([]api.ImportEdge, error) {
	return nil, nil
}
func (m *searchMockStore) GetImporters(ctx context.Context, importSource string) ([]api.ImportEdge, error) {
	return nil, nil
}
func (m *searchMockStore) Stats(ctx context.Context) (*api.IndexStats, error) {
	return &api.IndexStats{}, nil
}
func (m *searchMockStore) Close() error { return nil }

func TestSearchSymbolsFuzzyRanking(t *testing.T) {
	m := &searchMockStore{
		files: []*api.FileInfo{{Path: "a.go"}},
		fileSymbols: map[string][]api.Symbol{
			"a.go": {
				{Name: "MyComputeTool", FilePath: "a.go", Line: 1, Kind: api.Function},
				{Name: "ComputeFast", FilePath: "a.go", Line: 2, Kind: api.Function},
				{Name: "compute", FilePath: "a.go", Line: 3, Kind: api.Function},
			},
		},
	}

	sr := New(m, "")
	got, err := sr.SearchSymbols(context.Background(), "CoMpUtE", nil, 10)
	if err != nil {
		t.Fatalf("SearchSymbols error: %v", err)
	}
	if len(got) < 3 {
		t.Fatalf("expected at least 3 results, got %d", len(got))
	}

	if got[0].Name != "compute" {
		t.Fatalf("rank 1 should be exact match, got %q", got[0].Name)
	}
	if got[1].Name != "ComputeFast" {
		t.Fatalf("rank 2 should be prefix match, got %q", got[1].Name)
	}
	if got[2].Name != "MyComputeTool" {
		t.Fatalf("rank 3 should be substring match, got %q", got[2].Name)
	}
}

func TestSearchSymbolsKeepsExistingStoreResults(t *testing.T) {
	m := &searchMockStore{
		searchResults: []api.Symbol{{Name: "LegacyFTSResult", FilePath: "f.go", Line: 1, Kind: api.Function}},
	}

	sr := New(m, "")
	got, err := sr.SearchSymbols(context.Background(), "compute", nil, 10)
	if err != nil {
		t.Fatalf("SearchSymbols error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected existing store result to be preserved, got %d results", len(got))
	}
	if got[0].Name != "LegacyFTSResult" {
		t.Fatalf("unexpected result: %+v", got[0])
	}
}

func TestSearchSymbolsHybridIncludesSemanticMatches(t *testing.T) {
	m := &searchMockStore{
		searchResults: nil,
		files:         []*api.FileInfo{{Path: "a.go"}},
		fileSymbols: map[string][]api.Symbol{
			"a.go": {
				{Name: "AuthSessionManager", FilePath: "a.go", Line: 1, Kind: api.Type},
				{Name: "RenderButton", FilePath: "a.go", Line: 2, Kind: api.Function},
			},
		},
	}

	sr := New(m, "")
	got, err := sr.SearchSymbolsHybrid(context.Background(), "auth session", nil, 10)
	if err != nil {
		t.Fatalf("SearchSymbolsHybrid error: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected semantic results, got none")
	}
	if got[0].Name != "AuthSessionManager" {
		t.Fatalf("expected semantic top hit AuthSessionManager, got %q", got[0].Name)
	}
}

func TestSearchSymbolsHybridMergesFTSAndSemantic(t *testing.T) {
	m := &searchMockStore{
		searchResults: []api.Symbol{{Name: "FindToken", FilePath: "a.go", Line: 1, Kind: api.Function}},
		files:         []*api.FileInfo{{Path: "a.go"}, {Path: "b.go"}},
		fileSymbols: map[string][]api.Symbol{
			"a.go": {
				{Name: "FindToken", FilePath: "a.go", Line: 1, Kind: api.Function},
			},
			"b.go": {
				{Name: "TokenResolverService", FilePath: "b.go", Line: 2, Kind: api.Type},
			},
		},
	}

	sr := New(m, "")
	got, err := sr.SearchSymbolsHybrid(context.Background(), "token resolver", nil, 10)
	if err != nil {
		t.Fatalf("SearchSymbolsHybrid error: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("expected hybrid results from FTS + semantic, got %d", len(got))
	}

	seen := map[string]bool{}
	for _, sym := range got {
		seen[sym.Name] = true
	}
	if !seen["FindToken"] {
		t.Fatalf("expected FTS hit to be preserved")
	}
	if !seen["TokenResolverService"] {
		t.Fatalf("expected semantic hit to be included")
	}
}
