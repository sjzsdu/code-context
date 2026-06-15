package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
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
