package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	embeddingpkg "github.com/sjzsdu/code-context/internal/embedding"
	"github.com/sjzsdu/code-context/internal/store"
)

func TestCapabilityNames(t *testing.T) {
	got := capabilityNames([]store.Capability{
		store.CapabilityTextSearch,
		"",
		store.CapabilityGraphTraversal,
	})
	want := []string{"text_search", "graph_traversal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilityNames() = %#v, want %#v", got, want)
	}
}

func TestCapabilityNamesEmpty(t *testing.T) {
	got := capabilityNames(nil)
	if got == nil {
		t.Fatal("capabilityNames(nil) returned nil, want empty slice for stable JSON")
	}
	if len(got) != 0 {
		t.Fatalf("capabilityNames(nil) = %#v, want empty", got)
	}
}

func TestStatusIncludesEmbeddingCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := NewWithOptions(root, Options{
		Store: store.Options{
			Backend: store.BackendSQLite,
			SQLite:  store.SQLiteOptions{Path: filepath.Join(root, "index.db")},
		},
		Embedding: embeddingpkg.Options{
			Provider:   embeddingpkg.ProviderOpenAICompatible,
			BaseURL:    "http://embedding.local/v1",
			Model:      "text-embedding-test",
			Dimensions: 3,
		},
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}
	defer eng.Close()

	status, err := eng.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Embedding == nil || !status.Embedding.Enabled {
		t.Fatalf("embedding status = %#v, want enabled", status.Embedding)
	}
	if status.Embedding.Model != "text-embedding-test" {
		t.Fatalf("embedding model = %q", status.Embedding.Model)
	}
	if !containsString(status.Capabilities, "embedding") {
		t.Fatalf("capabilities = %#v, want embedding", status.Capabilities)
	}
}

func TestEmbedUnsupportedCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	_, err = eng.Embed(context.Background(), []store.EmbeddingInput{{ID: "q", Text: "hello"}})
	if !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("Embed error = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestEmbedDelegatesToProvider(t *testing.T) {
	eng := &Engine{embedder: fakeEmbedder{}}
	vectors, err := eng.Embed(context.Background(), []store.EmbeddingInput{{ID: "q", Text: "hello"}})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 1 || vectors[0].ID != "q" || len(vectors[0].Values) != 1 {
		t.Fatalf("vectors = %#v", vectors)
	}
}

func TestSearchVectorUnsupportedCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	_, err = eng.SearchVector(context.Background(), store.VectorSearchQuery{
		Vector: []float32{1},
		Model:  "fake",
	})
	if !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("SearchVector error = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestSearchVectorTextEmbedsQueryAndSearchesVectorProvider(t *testing.T) {
	vectorStore := &fakeVectorStore{}
	eng := &Engine{store: vectorStore, embedder: fakeEmbedder{}}
	hits, err := eng.SearchVectorText(context.Background(), "hello", store.VectorSearchQuery{Limit: 3})
	if err != nil {
		t.Fatalf("SearchVectorText: %v", err)
	}
	if len(hits) != 1 || hits[0].Target.Name != "Result" {
		t.Fatalf("hits = %#v", hits)
	}
	if vectorStore.query.QueryText != "hello" || vectorStore.query.Model != "fake" || vectorStore.query.Dimensions != 1 {
		t.Fatalf("query = %#v", vectorStore.query)
	}
	if len(vectorStore.query.Vector) != 1 || vectorStore.query.Vector[0] != 1 {
		t.Fatalf("query vector = %#v", vectorStore.query.Vector)
	}
}

func TestSearchVectorTextRequiresEmbedder(t *testing.T) {
	eng := &Engine{store: &fakeVectorStore{}}
	_, err := eng.SearchVectorText(context.Background(), "hello", store.VectorSearchQuery{})
	if !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("SearchVectorText error = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestSearchHybridFusesTextAndVector(t *testing.T) {
	hybridStore := &fakeHybridStore{}
	eng := &Engine{store: hybridStore, embedder: fakeEmbedder{}}
	hits, err := eng.SearchHybrid(context.Background(), store.HybridSearchQuery{Query: "hello", Limit: 5})
	if err != nil {
		t.Fatalf("SearchHybrid: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %#v, want one fused hit", hits)
	}
	if hits[0].Source != store.SearchSourceHybrid || hits[0].Target.Name != "Result" {
		t.Fatalf("unexpected hit: %#v", hits[0])
	}
	if !strings.Contains(hits[0].Metadata["sources"], "text") || !strings.Contains(hits[0].Metadata["sources"], "vector") {
		t.Fatalf("metadata = %#v, want text and vector sources", hits[0].Metadata)
	}
	if hits[0].Metadata["hybrid_fusion"] != "weighted_normalized_sum" {
		t.Fatalf("metadata = %#v, want weighted normalized fusion", hits[0].Metadata)
	}
	if hits[0].Metadata["hybrid_text_normalized_score"] != "1.0000" || hits[0].Metadata["hybrid_vector_normalized_score"] != "1.0000" {
		t.Fatalf("metadata = %#v, want normalized source scores", hits[0].Metadata)
	}
	if hybridStore.vectorQuery.QueryText != "hello" || hybridStore.vectorQuery.Model != "fake" || hybridStore.vectorQuery.Dimensions != 1 {
		t.Fatalf("vector query = %#v", hybridStore.vectorQuery)
	}
}

func TestSearchHybridNormalizesSourceScoreScales(t *testing.T) {
	hybridStore := &fakeHybridRankStore{}
	eng := &Engine{store: hybridStore, embedder: fakeEmbedder{}}
	hits, err := eng.SearchHybrid(context.Background(), store.HybridSearchQuery{Query: "hello", Limit: 5})
	if err != nil {
		t.Fatalf("SearchHybrid: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("hits = %#v, want at least two hits", hits)
	}
	if hits[0].Target.Name != "SemanticMatch" {
		t.Fatalf("top hit = %#v, want SemanticMatch after score normalization", hits[0])
	}
	if hits[0].Metadata["hybrid_text_score"] != "10.0000" ||
		hits[0].Metadata["hybrid_text_normalized_score"] != "0.1000" ||
		hits[0].Metadata["hybrid_text_rank"] != "2" {
		t.Fatalf("text metadata = %#v, want raw score, normalized score, and rank", hits[0].Metadata)
	}
	if hits[0].Metadata["hybrid_vector_normalized_score"] != "1.0000" ||
		hits[0].Metadata["hybrid_vector_rank"] != "1" {
		t.Fatalf("vector metadata = %#v, want normalized score and rank", hits[0].Metadata)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatalf("scores = %.4f <= %.4f, want fused semantic match first", hits[0].Score, hits[1].Score)
	}
}

func TestSearchHybridRequiresSignal(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}}
	_, err := eng.SearchHybrid(context.Background(), store.HybridSearchQuery{})
	if err == nil || !strings.Contains(err.Error(), "requires query") {
		t.Fatalf("SearchHybrid error = %v, want missing query/vector/expand_from", err)
	}
}

func TestCapabilityNamesIncludesHybridForAdvancedProvider(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}}
	got := eng.capabilityNames()
	if !containsString(got, string(store.CapabilityHybridSearch)) {
		t.Fatalf("capabilities = %#v, want hybrid_search", got)
	}
}

func TestTraverseGraphUnsupportedCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	_, err = eng.TraverseGraph(context.Background(), store.GraphTraversalQuery{
		Start: store.TargetRef{Kind: store.TargetFile, Path: "a.go"},
	})
	if !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("TraverseGraph error = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestBestEffortGraphTraversalIgnoresUnsupportedCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	if got := eng.graphTraversalForFile(context.Background(), "a.go", 2); got != nil {
		t.Fatalf("graphTraversalForFile on sqlite = %+v, want nil", got)
	}
}

func TestGraphTraversalDepthBounds(t *testing.T) {
	if got := graphTraversalDepth(0); got != 2 {
		t.Fatalf("graphTraversalDepth(0) = %d, want 2", got)
	}
	if got := graphTraversalDepth(9); got != 3 {
		t.Fatalf("graphTraversalDepth(9) = %d, want 3", got)
	}
}

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, inputs []store.EmbeddingInput) ([]store.EmbeddingVector, error) {
	out := make([]store.EmbeddingVector, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, store.EmbeddingVector{ID: input.ID, Values: []float32{1}, Model: "fake", Dimensions: 1})
	}
	return out, nil
}

func (fakeEmbedder) EmbeddingModel() store.EmbeddingModelInfo {
	return store.EmbeddingModelInfo{Provider: "fake", Model: "fake", Dimensions: 1}
}

type fakeVectorStore struct {
	store.Store
	query store.VectorSearchQuery
}

func (s *fakeVectorStore) SearchVector(_ context.Context, query store.VectorSearchQuery) ([]store.SearchHit, error) {
	s.query = query
	return []store.SearchHit{{
		Target: store.TargetRef{Kind: store.TargetSymbol, Name: "Result"},
		Score:  1,
		Source: store.SearchSourceVector,
	}}, nil
}

type fakeHybridStore struct {
	store.Store
	textQuery   store.TextSearchQuery
	vectorQuery store.VectorSearchQuery
}

func (s *fakeHybridStore) SearchText(_ context.Context, query store.TextSearchQuery) ([]store.SearchHit, error) {
	s.textQuery = query
	return []store.SearchHit{{
		Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "a.go", Name: "Result", Line: 3},
		Score:    0.8,
		Source:   store.SearchSourceText,
		Evidence: "text evidence",
	}}, nil
}

func (s *fakeHybridStore) SearchVector(_ context.Context, query store.VectorSearchQuery) ([]store.SearchHit, error) {
	s.vectorQuery = query
	return []store.SearchHit{{
		Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "a.go", Name: "Result", Line: 3},
		Score:    0.9,
		Source:   store.SearchSourceVector,
		Evidence: "vector evidence",
	}}, nil
}

type fakeHybridRankStore struct {
	store.Store
}

func (s *fakeHybridRankStore) SearchText(_ context.Context, query store.TextSearchQuery) ([]store.SearchHit, error) {
	return []store.SearchHit{
		{
			Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "a.go", Name: "LexicalMatch", Line: 3},
			Score:    100,
			Source:   store.SearchSourceText,
			Evidence: "lexical evidence",
		},
		{
			Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "b.go", Name: "SemanticMatch", Line: 5},
			Score:    10,
			Source:   store.SearchSourceText,
			Evidence: "weaker lexical evidence",
		},
	}, nil
}

func (s *fakeHybridRankStore) SearchVector(_ context.Context, query store.VectorSearchQuery) ([]store.SearchHit, error) {
	return []store.SearchHit{{
		Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "b.go", Name: "SemanticMatch", Line: 5},
		Score:    0.9,
		Source:   store.SearchSourceVector,
		Evidence: "semantic evidence",
	}}, nil
}
