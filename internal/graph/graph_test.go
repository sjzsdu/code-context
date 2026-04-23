package graph

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/store"
)

func setupGraphStore(t *testing.T) (store.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graph_test.db")
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}

	ctx := context.Background()
	files := []struct {
		path string
		lang api.Language
		hash string
		size int64
	}{
		{"a.go", api.Go, "hash-a", 100},
		{"b.go", api.Go, "hash-b", 200},
		{"c.go", api.Go, "hash-c", 50},
	}
	ids := make(map[string]int64)
	for _, f := range files {
		fi := &api.FileInfo{Path: f.path, Language: f.lang, ContentHash: f.hash, Size: f.size}
		id, err := s.UpsertFile(ctx, fi)
		if err != nil {
			t.Fatalf("upsert file %s: %v", f.path, err)
		}
		ids[f.path] = id
	}

	// a.go imports fmt, os
	// b.go imports fmt, net/http
	// c.go imports os
	edges := map[string][]api.ImportEdge{
		"a.go": {{FromFile: "a.go", ToSource: "fmt", Line: 1}, {FromFile: "a.go", ToSource: "os", Line: 2}},
		"b.go": {{FromFile: "b.go", ToSource: "fmt", Line: 1}, {FromFile: "b.go", ToSource: "net/http", Line: 2}},
		"c.go": {{FromFile: "c.go", ToSource: "os", Line: 1}},
	}
	for path, list := range edges {
		id := ids[path]
		if err := s.ReplaceImports(ctx, id, list); err != nil {
			t.Fatalf("replace imports for %s: %v", path, err)
		}
	}

	// return a cleanup function
	cleanup := func() {
		s.Close()
	}
	return s, cleanup
}

func TestBuild(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
}

func TestDirectImports(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	got := g.DirectImports("a.go")
	want := []string{"fmt", "os"}
	if len(got) != len(want) {
		t.Fatalf("DirectImports(a.go) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DirectImports(a.go)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDirectImporters(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	got := g.DirectImporters("fmt")
	want := []string{"a.go", "b.go"}
	if len(got) != len(want) {
		t.Fatalf("DirectImporters(fmt) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DirectImporters(fmt)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDependencies(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	got := g.Dependencies("a.go", 10)
	want := []string{"fmt", "os"}
	if len(got) != len(want) {
		t.Fatalf("Dependencies(a.go) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Dependencies(a.go)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDependents(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	got := g.Dependents("fmt", 10)
	want := []string{"a.go", "b.go", "c.go"}
	if len(got) != len(want) {
		t.Fatalf("Dependents(fmt) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Dependents(fmt)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRelated(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	related := g.Related("a.go", 10)
	// expect both b.go and c.go to be related via shared imports
	foundB := false
	foundC := false
	for _, r := range related {
		if r == "b.go" {
			foundB = true
		}
		if r == "c.go" {
			foundC = true
		}
	}
	if !foundB || !foundC {
		t.Fatalf("Related(a.go) missing expected files: got %v", related)
	}
}

func TestFileNeighbors(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	got := g.FileNeighbors("a.go")
	want := []string{"b.go", "c.go"}
	if len(got) != len(want) {
		t.Fatalf("FileNeighbors(a.go) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FileNeighbors(a.go)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSubgraphFiles(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	got := g.SubgraphFiles("a.go", 1)
	want := []string{"a.go", "b.go", "c.go"}
	if len(got) != len(want) {
		t.Fatalf("SubgraphFiles(a.go,1) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SubgraphFiles(a.go,1)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTraceFiles(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	got := g.TraceFiles("a.go", "b.go", 5)
	want := []string{"a.go", "b.go"}
	if len(got) != len(want) {
		t.Fatalf("TraceFiles(a.go,b.go) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TraceFiles(a.go,b.go)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestImportCounts(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	counts := g.ImportCounts()
	if counts["fmt"] != 2 {
		t.Fatalf("ImportCounts()[fmt] = %d, want 2", counts["fmt"])
	}
	if counts["os"] != 2 {
		t.Fatalf("ImportCounts()[os] = %d, want 2", counts["os"])
	}
}

func TestRelatedScores(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	scores := g.RelatedScores("a.go")
	if scores["b.go"] != 1 {
		t.Fatalf("RelatedScores(a.go)[b.go] = %d, want 1", scores["b.go"])
	}
	if scores["c.go"] != 1 {
		t.Fatalf("RelatedScores(a.go)[c.go] = %d, want 1", scores["c.go"])
	}
}

func TestFileConnectionCounts(t *testing.T) {
	s, cleanup := setupGraphStore(t)
	defer cleanup()

	g := New(s)
	if err := g.Build(context.Background()); err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	counts := g.FileConnectionCounts()
	if counts["a.go"] == 0 {
		t.Fatalf("expected a.go to have file connections, got %v", counts)
	}
	if counts["b.go"] == 0 {
		t.Fatalf("expected b.go to have file connections, got %v", counts)
	}
}

func TestDedup(t *testing.T) {
	// directly test dedup function
	input := []string{"b.go", "a.go", "b.go", "c.go", "a.go"}
	got := dedup(input)
	want := []string{"a.go", "b.go", "c.go"}
	if len(got) != len(want) {
		t.Fatalf("dedup() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dedup()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
