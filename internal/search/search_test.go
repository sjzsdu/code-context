package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/store"
)

func setupSearchStore(t *testing.T) (store.Store, string, []string, func()) {
	t.Helper()
	root := t.TempDir()
	files := []struct {
		name    string
		content string
		lang    api.Language
		hash    string
		size    int64
	}{
		{"f1.go", "package main\n// TARGET_ONE\nfunc main(){}\n", api.Go, "hash-1", 100},
		{"f2.go", "package main\n// TARGET_TWO\nfunc other(){}\n", api.Go, "hash-2", 120},
	}
	for _, f := range files {
		p := filepath.Join(root, f.name)
		if err := os.WriteFile(p, []byte(f.content), 0644); err != nil {
			t.Fatalf("write test file %s: %v", f.name, err)
		}
	}

	dbPath := filepath.Join(root, "search_test.db")
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("store init: %v", err)
	}
	ctx := context.Background()
	for _, f := range files {
		fi := &api.FileInfo{Path: f.name, Language: f.lang, ContentHash: f.hash, Size: f.size}
		if _, err := s.UpsertFile(ctx, fi); err != nil {
			t.Fatalf("upsert file %s: %v", f.name, err)
		}
	}

	cleanup := func() { s.Close() }
	pathNames := []string{files[0].name, files[1].name}
	return s, root, pathNames, cleanup
}

func TestSearchText(t *testing.T) {
	s, root, paths, cleanup := setupSearchStore(t)
	defer cleanup()

	sr := New(s, root)
	matches, err := sr.SearchText(context.Background(), "TARGET_", "", 0)
	if err != nil {
		t.Fatalf("SearchText error: %v", err)
	}
	if len(matches) != len(paths) {
		t.Fatalf("SearchText results count = %d, want %d", len(matches), len(paths))
	}
	got := map[string]string{}
	for _, m := range matches {
		got[m.FilePath] = m.Content
	}
	if _, ok := got[paths[0]]; !ok {
		t.Fatalf("missing match for %s", paths[0])
	}
	if _, ok := got[paths[1]]; !ok {
		t.Fatalf("missing match for %s", paths[1])
	}
}

func TestSearchTextWithPattern(t *testing.T) {
	s, root, paths, cleanup := setupSearchStore(t)
	defer cleanup()

	sr := New(s, root)
	matches, err := sr.SearchText(context.Background(), "TARGET_ONE", "f1.go", 0)
	if err != nil {
		t.Fatalf("SearchText error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].FilePath != paths[0] {
		t.Fatalf("expected match from %s, got %s", paths[0], matches[0].FilePath)
	}
}

func TestGrepFile(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "g.go")
	content := "line1\n// TARGET_GREP\nline3\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write grep file: %v", err)
	}
	ms := grepFile(root, "g.go", "TARGET_GREP", 10)
	if len(ms) != 1 {
		t.Fatalf("grepFile expected 1 match, got %d", len(ms))
	}
	if ms[0].Line != 2 || ms[0].FilePath != "g.go" {
		t.Fatalf("grepFile returned unexpected match: %+v", ms[0])
	}
}

func TestFormatSymbols(t *testing.T) {
	syms := []api.Symbol{
		{Name: "Foo", Kind: api.Function, FilePath: "a.go", Line: 10},
		{Name: "Bar", Kind: api.Type, FilePath: "b.go", Line: 20},
	}
	s := FormatSymbols(syms)
	if s == "" || !(strings.Contains(s, "Foo") && strings.Contains(s, "Bar")) {
		t.Fatalf("FormatSymbols output is not as expected: %q", s)
	}
}

func TestFormatMatches(t *testing.T) {
	ms := []api.SearchMatch{{FilePath: "a.go", Line: 3, Content: "x"}, {FilePath: "b.go", Line: 5, Content: "y"}}
	out := FormatMatches(ms)
	if !strings.Contains(out, "a.go:3") || !strings.Contains(out, "b.go:5") {
		t.Fatalf("FormatMatches output not as expected: %q", out)
	}
}
