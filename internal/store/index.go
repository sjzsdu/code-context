package store

import (
	"context"
	"fmt"

	"github.com/sjzsdu/code-context/internal/api"
)

type FileIndex struct {
	File    *api.FileInfo
	Symbols []api.Symbol
	Imports []api.ImportEdge
	Calls   []api.CallEdge
	Routes  []api.Route
}

type fileIndexReplacer interface {
	ReplaceFileIndex(ctx context.Context, idx FileIndex) (int64, error)
}

func ReplaceFileIndex(ctx context.Context, s Store, idx FileIndex) (int64, error) {
	if idx.File == nil {
		return 0, fmt.Errorf("file index file is required")
	}
	if replacer, ok := s.(fileIndexReplacer); ok {
		return replacer.ReplaceFileIndex(ctx, idx)
	}
	fileID, err := s.UpsertFile(ctx, idx.File)
	if err != nil {
		return 0, err
	}
	if err := s.ReplaceSymbols(ctx, fileID, idx.Symbols); err != nil {
		return 0, err
	}
	if err := s.ReplaceImports(ctx, fileID, idx.Imports); err != nil {
		return 0, err
	}
	if err := s.ReplaceCalls(ctx, fileID, idx.Calls); err != nil {
		return 0, err
	}
	if err := s.ReplaceRoutes(ctx, fileID, idx.Routes); err != nil {
		return 0, err
	}
	return fileID, nil
}
