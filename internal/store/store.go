package store

import (
	"context"

	"github.com/sjzsdu/code-context/internal/api"
)

type Store interface {
	Init(ctx context.Context) error
	UpsertFile(ctx context.Context, f *api.FileInfo) (int64, error)
	GetFile(ctx context.Context, path string) (*api.FileInfo, error)
	DeleteFile(ctx context.Context, path string) error
	ListFiles(ctx context.Context, lang *api.Language) ([]*api.FileInfo, error)
	ReplaceSymbols(ctx context.Context, fileID int64, symbols []api.Symbol) error
	ReplaceImports(ctx context.Context, fileID int64, imports []api.ImportEdge) error
	ReplaceCalls(ctx context.Context, fileID int64, calls []api.CallEdge) error
	SearchSymbols(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error)
	FindDefinitions(ctx context.Context, name string) ([]api.Symbol, error)
	FindReferences(ctx context.Context, name string) ([]api.Symbol, error)
	GetFileSymbols(ctx context.Context, path string) ([]api.Symbol, error)
	GetImports(ctx context.Context, filePath string) ([]api.ImportEdge, error)
	GetImporters(ctx context.Context, importSource string) ([]api.ImportEdge, error)
	GetCallees(ctx context.Context, fromSymbol string) ([]api.CallEdge, error)
	GetCallers(ctx context.Context, toName string) ([]api.CallEdge, error)
	Stats(ctx context.Context) (*api.IndexStats, error)
	UpsertDocument(ctx context.Context, doc *api.Document) (int64, error)
	GetDocument(ctx context.Context, path string) (*api.Document, error)
	DeleteDocument(ctx context.Context, path string) error
	ListDocuments(ctx context.Context) ([]*api.Document, error)
	ReplaceDocumentLinks(ctx context.Context, docID int64, links []api.DocumentLink) error
	GetDocumentLinks(ctx context.Context, docPath string) ([]api.DocumentLink, error)
	GetDocumentsByTarget(ctx context.Context, targetType, targetValue string) ([]api.DocumentLink, error)
	GetDocumentStats(ctx context.Context) (int, int, error)
	Close() error
}
