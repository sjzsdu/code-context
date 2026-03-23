package store

import (
	"context"

	"github.com/sjzsdu/code-memory/internal/api"
)

type Store interface {
	Init(ctx context.Context) error
	UpsertFile(ctx context.Context, f *api.FileInfo) (int64, error)
	GetFile(ctx context.Context, path string) (*api.FileInfo, error)
	DeleteFile(ctx context.Context, path string) error
	ListFiles(ctx context.Context, lang *api.Language) ([]*api.FileInfo, error)
	ReplaceSymbols(ctx context.Context, fileID int64, symbols []api.Symbol) error
	ReplaceImports(ctx context.Context, fileID int64, imports []api.ImportEdge) error
	SearchSymbols(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error)
	FindDefinitions(ctx context.Context, name string) ([]api.Symbol, error)
	FindReferences(ctx context.Context, name string) ([]api.Symbol, error)
	GetFileSymbols(ctx context.Context, path string) ([]api.Symbol, error)
	GetImports(ctx context.Context, filePath string) ([]api.ImportEdge, error)
	GetImporters(ctx context.Context, importSource string) ([]api.ImportEdge, error)
	Stats(ctx context.Context) (*api.IndexStats, error)
	Close() error
}
