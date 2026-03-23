package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sjzsdu/code-memory/internal/api"
	"github.com/sjzsdu/code-memory/internal/graph"
	"github.com/sjzsdu/code-memory/internal/indexer"
	"github.com/sjzsdu/code-memory/internal/lang"
	"github.com/sjzsdu/code-memory/internal/parser"
	"github.com/sjzsdu/code-memory/internal/search"
	"github.com/sjzsdu/code-memory/internal/store"
)

type Engine struct {
	root    string
	dbPath  string
	store   store.Store
	parser  parser.Parser
	indexer *indexer.Indexer
	search  *search.Searcher
	graph   *graph.Graph
}

func New(root string, dbPath string) (*Engine, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	root, _ = filepath.Abs(root)

	if dbPath == "" {
		dbPath = filepath.Join(root, ".github.com/sjzsdu/code-memory", "index.db")
		os.MkdirAll(filepath.Dir(dbPath), 0o755)
	}

	reg := lang.NewRegistry()
	p := parser.NewTreeSitterParser(reg)
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	if err := s.Init(context.Background()); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	idx := indexer.New(p, s, root)
	sr := search.New(s, root)
	g := graph.New(s)

	return &Engine{
		root:    root,
		dbPath:  dbPath,
		store:   s,
		parser:  p,
		indexer: idx,
		search:  sr,
		graph:   g,
	}, nil
}

func (e *Engine) Index(ctx context.Context, verbose bool) (*api.IndexStats, error) {
	return e.indexer.IndexAll(ctx, verbose)
}

func (e *Engine) IndexIncremental(ctx context.Context, verbose bool) (*api.IndexStats, error) {
	return e.indexer.IndexIncremental(ctx, verbose)
}

func (e *Engine) SearchSymbols(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error) {
	return e.search.SearchSymbols(ctx, query, kind, limit)
}

func (e *Engine) FindDef(ctx context.Context, name string) ([]api.Symbol, error) {
	return e.search.FindDefinition(ctx, name)
}

func (e *Engine) FindRefs(ctx context.Context, name string) ([]api.Symbol, error) {
	return e.search.FindReferences(ctx, name)
}

func (e *Engine) FileSymbols(ctx context.Context, path string) ([]api.Symbol, error) {
	return e.search.GetFileSymbols(ctx, path)
}

func (e *Engine) SearchText(ctx context.Context, query string, filePattern string, limit int) ([]api.SearchMatch, error) {
	return e.search.SearchText(ctx, query, filePattern, limit)
}

func (e *Engine) Imports(ctx context.Context, file string) ([]api.ImportEdge, error) {
	return e.store.GetImports(ctx, file)
}

func (e *Engine) Importers(ctx context.Context, source string) ([]api.ImportEdge, error) {
	return e.store.GetImporters(ctx, source)
}

func (e *Engine) BuildGraph(ctx context.Context) error {
	return e.graph.Build(ctx)
}

func (e *Engine) GraphDeps(file string, depth int) []string {
	return e.graph.Dependencies(file, depth)
}

func (e *Engine) GraphRelated(file string, topN int) []string {
	return e.graph.Related(file, topN)
}

func (e *Engine) Stats(ctx context.Context) (*api.IndexStats, error) {
	return e.store.Stats(ctx)
}

func (e *Engine) ListFiles(ctx context.Context, lang *api.Language) ([]*api.FileInfo, error) {
	return e.store.ListFiles(ctx, lang)
}

func (e *Engine) Close() error {
	return e.store.Close()
}
