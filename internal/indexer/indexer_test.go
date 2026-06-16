package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
	embeddingpkg "github.com/sjzsdu/code-context/internal/embedding"
	"github.com/sjzsdu/code-context/internal/parser"
	"github.com/sjzsdu/code-context/internal/store"
)

func TestIndexAllReturnsStatsError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "health.md"), []byte("# Health\n\nHealthHandler\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := New(markdownOnlyParser{}, statsErrorStore{}, root).IndexAll(context.Background(), false)
	if err == nil {
		t.Fatalf("expected stats error")
	}
	if !strings.Contains(err.Error(), "load index stats") {
		t.Fatalf("expected contextual stats error, got %v", err)
	}
}

func TestIndexAllCachesSymbolEmbeddings(t *testing.T) {
	root := t.TempDir()
	content := []byte("package main\n\nfunc HealthHandler() {}\n")
	if err := os.WriteFile(filepath.Join(root, "main.go"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(root, "index.db")
	st, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()

	idx := New(fakeGoParser{}, st, root)
	idx.SetEmbedder(fakeEmbeddingProvider{})
	if _, err := idx.IndexAll(context.Background(), false); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	symbol := api.Symbol{Name: "HealthHandler", Kind: api.Function, FilePath: "main.go", Line: 3, EndLine: 3, Signature: "func HealthHandler()"}
	chunks := embeddingpkg.BuildSymbolChunks("", "main.go", content, []api.Symbol{symbol})
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	key := embeddingpkg.CacheKey("fake-model", 1, chunks[0].Text)
	cache := st.(store.EmbeddingCache)
	entry, err := cache.GetEmbedding(context.Background(), key)
	if err != nil {
		t.Fatalf("get embedding: %v", err)
	}
	if entry == nil {
		t.Fatalf("expected cached embedding")
	}
	if entry.Target.Kind != store.TargetSymbol || entry.Target.Name != "HealthHandler" {
		t.Fatalf("target = %+v", entry.Target)
	}
	if len(entry.Values) != 1 || entry.Values[0] != 1 {
		t.Fatalf("values = %+v", entry.Values)
	}
}

type fakeGoParser struct{}

func (fakeGoParser) Parse(context.Context, string, []byte, api.Language) (*parser.ParseResult, error) {
	return &parser.ParseResult{
		Symbols: []api.Symbol{{Name: "HealthHandler", Kind: api.Function, FilePath: "main.go", Line: 3, EndLine: 3, Signature: "func HealthHandler()"}},
	}, nil
}

func (fakeGoParser) DetectLanguage(path string) (api.Language, bool) {
	if strings.HasSuffix(path, ".go") {
		return api.Go, true
	}
	return "", false
}

func (fakeGoParser) SupportsLanguage(lang api.Language) bool {
	return lang == api.Go
}

type fakeEmbeddingProvider struct{}

func (fakeEmbeddingProvider) Embed(_ context.Context, inputs []store.EmbeddingInput) ([]store.EmbeddingVector, error) {
	out := make([]store.EmbeddingVector, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, store.EmbeddingVector{ID: input.ID, Values: []float32{1}, Model: "fake-model", Dimensions: 1, Target: input.Target})
	}
	return out, nil
}

func (fakeEmbeddingProvider) EmbeddingModel() store.EmbeddingModelInfo {
	return store.EmbeddingModelInfo{Provider: "fake", Model: "fake-model", Dimensions: 1}
}

type markdownOnlyParser struct{}

func (markdownOnlyParser) Parse(context.Context, string, []byte, api.Language) (*parser.ParseResult, error) {
	return &parser.ParseResult{}, nil
}

func (markdownOnlyParser) DetectLanguage(path string) (api.Language, bool) {
	if strings.HasSuffix(path, ".md") {
		return api.Markdown, true
	}
	return "", false
}

func (markdownOnlyParser) SupportsLanguage(lang api.Language) bool {
	return lang == api.Markdown
}

type statsErrorStore struct {
	store.Store
}

func (statsErrorStore) Init(context.Context) error { return nil }

func (statsErrorStore) UpsertDocument(context.Context, *api.Document) (int64, error) {
	return 1, nil
}

func (statsErrorStore) ReplaceDocumentLinks(context.Context, int64, []api.DocumentLink) error {
	return nil
}

func (statsErrorStore) ListFiles(context.Context, *api.Language) ([]*api.FileInfo, error) {
	return nil, nil
}

func (statsErrorStore) ListDocuments(context.Context) ([]*api.Document, error) {
	return nil, nil
}

func (statsErrorStore) Stats(context.Context) (*api.IndexStats, error) {
	return nil, errors.New("stats failed")
}
