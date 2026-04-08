package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/parser"
	"github.com/sjzsdu/code-context/internal/store"
)

type Indexer struct {
	parser  parser.Parser
	store   store.Store
	root    string
	workers int
}

func New(p parser.Parser, s store.Store, root string) *Indexer {
	w := runtime.NumCPU()
	if w > 16 {
		w = 16
	}
	return &Indexer{parser: p, store: s, root: root, workers: w}
}

type parseResult struct {
	path    string
	content []byte
	lang    api.Language
	result  *parser.ParseResult
	err     error
}

func (idx *Indexer) IndexAll(ctx context.Context, verbose bool) (*api.IndexStats, error) {
	start := time.Now()

	if err := idx.store.Init(ctx); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	files, err := idx.walk()
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}

	total := len(files)
	if verbose {
		fmt.Printf("Found %d files to process\n", total)
	}

	results := make(chan parseResult, idx.workers*4)

	go idx.parseAll(ctx, files, results)

	var indexed, skipped, failed, syms, imps int64
	sem := make(chan struct{}, idx.workers)
	var wg sync.WaitGroup

	for pr := range results {
		if pr.err != nil {
			atomic.AddInt64(&failed, 1)
			if verbose {
				fmt.Fprintf(os.Stderr, "  skip %s: %v\n", pr.path, pr.err)
			}
			continue
		}
		if pr.result == nil {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		hash := sha256Hex(pr.content)
		existing, err := idx.store.GetFile(ctx, pr.path)
		if err == nil && existing != nil && existing.ContentHash == hash {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(pr parseResult) {
			defer wg.Done()
			defer func() { <-sem }()

			fileID, err := idx.store.UpsertFile(ctx, &api.FileInfo{
				Path: pr.path, Language: pr.lang, ContentHash: hash, Size: int64(len(pr.content)),
			})
			if err != nil {
				atomic.AddInt64(&failed, 1)
				return
			}

			if err := idx.store.ReplaceSymbols(ctx, fileID, pr.result.Symbols); err != nil {
				atomic.AddInt64(&failed, 1)
				return
			}
			if err := idx.store.ReplaceImports(ctx, fileID, pr.result.Imports); err != nil {
				atomic.AddInt64(&failed, 1)
				return
			}

			atomic.AddInt64(&syms, int64(len(pr.result.Symbols)))
			atomic.AddInt64(&imps, int64(len(pr.result.Imports)))
			atomic.AddInt64(&indexed, 1)
		}(pr)
	}
	wg.Wait()

	// Remove deleted files from store
	existingFiles, _ := idx.store.ListFiles(ctx, nil)
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	for _, f := range existingFiles {
		if !fileSet[f.Path] {
			idx.store.DeleteFile(ctx, f.Path)
			atomic.AddInt64(&skipped, 1)
		}
	}

	stats, _ := idx.store.Stats(ctx)
	return &api.IndexStats{
		TotalFiles:   total,
		IndexedFiles: int(indexed),
		SkippedFiles: int(skipped),
		FailedFiles:  int(failed),
		TotalSymbols: stats.TotalSymbols,
		TotalImports: stats.TotalImports,
		Duration:     time.Since(start).Seconds(),
	}, nil
}

func (idx *Indexer) parseAll(ctx context.Context, files []string, out chan<- parseResult) {
	defer close(out)
	sem := make(chan struct{}, idx.workers)
	var wg sync.WaitGroup

	for _, f := range files {
		select {
		case <-ctx.Done():
			return
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			lang, ok := idx.parser.DetectLanguage(path)
			if !ok {
				out <- parseResult{path: path}
				return
			}

			content, err := os.ReadFile(filepath.Join(idx.root, path))
			if err != nil {
				out <- parseResult{path: path, err: err}
				return
			}

			result, err := idx.parser.Parse(ctx, path, content, lang)
			if err != nil {
				out <- parseResult{path: path, err: err}
				return
			}

			out <- parseResult{path: path, content: content, lang: lang, result: result}
		}(f)
	}
	wg.Wait()
}

func (idx *Indexer) IndexIncremental(ctx context.Context, verbose bool) (*api.IndexStats, error) {
	start := time.Now()

	if err := idx.store.Init(ctx); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	files, err := idx.walk()
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}

	existingFiles, _ := idx.store.ListFiles(ctx, nil)
	existingMap := make(map[string]*api.FileInfo)
	for _, f := range existingFiles {
		existingMap[f.Path] = f
	}

	// Find files to update
	var toUpdate []string
	for _, f := range files {
		ef, ok := existingMap[f]
		if !ok {
			toUpdate = append(toUpdate, f)
			continue
		}
		content, err := os.ReadFile(filepath.Join(idx.root, f))
		if err != nil {
			continue
		}
		hash := sha256Hex(content)
		if hash != ef.ContentHash {
			toUpdate = append(toUpdate, f)
		}
	}

	// Remove deleted files
	var removed int
	for _, ef := range existingFiles {
		if !fileSet[ef.Path] {
			idx.store.DeleteFile(ctx, ef.Path)
			removed++
		}
	}

	// Re-index changed files
	var indexed, failed int64
	sem := make(chan struct{}, idx.workers)
	var wg sync.WaitGroup

	for _, f := range toUpdate {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()
			_, _, _, err := idx.indexOneFile(ctx, path)
			if err != nil {
				atomic.AddInt64(&failed, 1)
			} else {
				atomic.AddInt64(&indexed, 1)
			}
		}(f)
	}
	wg.Wait()

	stats, _ := idx.store.Stats(ctx)
	return &api.IndexStats{
		TotalFiles:   len(files),
		IndexedFiles: int(indexed),
		SkippedFiles: len(files) - len(toUpdate) + removed,
		FailedFiles:  int(failed),
		TotalSymbols: stats.TotalSymbols,
		TotalImports: stats.TotalImports,
		Duration:     time.Since(start).Seconds(),
	}, nil
}

func (idx *Indexer) indexOneFile(ctx context.Context, path string) (nSyms, nImps int, skip bool, err error) {
	fullPath := filepath.Join(idx.root, path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return 0, 0, false, err
	}

	lang, ok := idx.parser.DetectLanguage(path)
	if !ok {
		return 0, 0, true, nil
	}

	hash := sha256Hex(content)
	existing, err := idx.store.GetFile(ctx, path)
	if err == nil && existing != nil && existing.ContentHash == hash {
		return 0, 0, false, nil
	}

	result, err := idx.parser.Parse(ctx, path, content, lang)
	if err != nil {
		return 0, 0, false, err
	}

	fileID, err := idx.store.UpsertFile(ctx, &api.FileInfo{
		Path:        path,
		Language:    lang,
		ContentHash: hash,
		Size:        int64(len(content)),
	})
	if err != nil {
		return 0, 0, false, err
	}

	if err := idx.store.ReplaceSymbols(ctx, fileID, result.Symbols); err != nil {
		return 0, 0, false, err
	}
	if err := idx.store.ReplaceImports(ctx, fileID, result.Imports); err != nil {
		return 0, 0, false, err
	}

	return len(result.Symbols), len(result.Imports), false, nil
}

func (idx *Indexer) walk() ([]string, error) {
	var files []string
	err := filepath.WalkDir(idx.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(idx.root, path)
		if rel == "." {
			return nil
		}

		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.IsDir() && isSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			if _, ok := idx.parser.DetectLanguage(rel); !ok {
				return nil
			}
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func isSkipDir(name string) bool {
	skip := map[string]bool{
		"node_modules": true, "vendor": true, "__pycache__": true,
		".git": true, ".idea": true, ".vscode": true,
		"target": true, "build": true, "dist": true, "venv": true, ".venv": true,
	}
	return skip[name]
}
