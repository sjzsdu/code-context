package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
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
