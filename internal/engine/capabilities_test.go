package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

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
