package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/store"
)

type Searcher struct {
	store store.Store
	root  string
}

func New(s store.Store, root string) *Searcher {
	return &Searcher{store: s, root: root}
}

func (sr *Searcher) SearchSymbols(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error) {
	return sr.store.SearchSymbols(ctx, query, kind, limit)
}

func (sr *Searcher) FindDefinition(ctx context.Context, name string) ([]api.Symbol, error) {
	return sr.store.FindDefinitions(ctx, name)
}

func (sr *Searcher) FindReferences(ctx context.Context, name string) ([]api.Symbol, error) {
	// First find where the symbol is defined
	defs, err := sr.store.FindDefinitions(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, nil
	}

	// Get the definition to know its kind and file
	def := defs[0]

	// Find all symbols with the same name across ALL files
	// This finds usages (not just definitions)
	refs, err := sr.store.FindReferences(ctx, name)
	if err != nil {
		return nil, err
	}

	// Filter out the definition itself
	var result []api.Symbol
	for _, r := range refs {
		// Exclude the definition file's same-name symbol
		if r.FilePath == def.FilePath && r.Line == def.Line {
			continue
		}
		result = append(result, r)
	}

	return result, nil
}

func (sr *Searcher) GetFileSymbols(ctx context.Context, path string) ([]api.Symbol, error) {
	return sr.store.GetFileSymbols(ctx, path)
}

func (sr *Searcher) SearchText(ctx context.Context, query string, filePattern string, limit int) ([]api.SearchMatch, error) {
	if limit <= 0 {
		limit = 50
	}

	files, err := sr.store.ListFiles(ctx, nil)
	if err != nil {
		return nil, err
	}

	var matches []api.SearchMatch
	for _, f := range files {
		if len(matches) >= limit {
			break
		}
		if filePattern != "" && !strings.Contains(f.Path, filePattern) {
			continue
		}
		ms := grepFile(sr.root, f.Path, query, limit-len(matches))
		matches = append(matches, ms...)
	}
	return matches, nil
}

func grepFile(root, path, pattern string, max int) []api.SearchMatch {
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return nil
	}

	var matches []api.SearchMatch
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if len(matches) >= max {
			break
		}
		if strings.Contains(line, pattern) {
			matches = append(matches, api.SearchMatch{
				FilePath: path,
				Line:     i + 1,
				Content:  strings.TrimSpace(line),
			})
		}
	}
	return matches
}

func formatSym(s api.Symbol) string {
	return fmt.Sprintf("  %-40s  %-10s  %s:%d", s.Name, s.Kind, s.FilePath, s.Line)
}

func FormatSymbols(syms []api.Symbol) string {
	var lines []string
	for _, s := range syms {
		lines = append(lines, formatSym(s))
	}
	return strings.Join(lines, "\n")
}

func FormatMatches(ms []api.SearchMatch) string {
	var lines []string
	for _, m := range ms {
		lines = append(lines, fmt.Sprintf("  %s:%d  %s", m.FilePath, m.Line, m.Content))
	}
	return strings.Join(lines, "\n")
}
