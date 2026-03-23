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
