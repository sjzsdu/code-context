package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
)

func TestDocDriftResolvesDocumentRouteLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(`package main

import "net/http"

func main() {
	http.HandleFunc("/ok", handler)
	http.HandleFunc("/undocumented", handler)
}

func handler(w http.ResponseWriter, r *http.Request) {}

func DocumentedThing() {}

func UndocumentedThing() {}
	`), 0o644); err != nil {
		t.Fatalf("write go file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".code-context"), 0o755); err != nil {
		t.Fatalf("mkdir index dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "api.md"), []byte(`# API

## Routes

GET /ok
POST /missing

## Symbols

Use `+"`DocumentedThing`"+` when preparing examples.
	`), 0o644); err != nil {
		t.Fatalf("write doc file: %v", err)
	}

	eng, err := New(root, filepath.Join(root, ".code-context", "index.db"))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	if _, err := eng.Index(ctx, false); err != nil {
		t.Fatalf("index: %v", err)
	}
	report, err := eng.DocDrift(ctx)
	if err != nil {
		t.Fatalf("doc drift: %v", err)
	}

	var foundMissing, foundOK bool
	for _, item := range report.Broken {
		switch item.TargetValue {
		case "POST /missing":
			if item.TargetType != "route" || item.Reason != "referenced route was not found" {
				t.Fatalf("unexpected missing route drift item: %+v", item)
			}
			foundMissing = true
		case "GET /ok":
			foundOK = true
		}
	}
	if !foundMissing {
		t.Fatalf("expected missing route drift item, got %+v", report.Broken)
	}
	if foundOK {
		t.Fatalf("documented existing route should not drift, got %+v", report.Broken)
	}

	coverage, err := eng.DocCoverage(ctx)
	if err != nil {
		t.Fatalf("doc coverage: %v", err)
	}
	if coverage.TotalRoutes != 2 || coverage.DocumentedRoutes != 1 || len(coverage.MissingRoutes) != 1 {
		t.Fatalf("unexpected coverage report: %+v", coverage)
	}
	if coverage.MissingRoutes[0].Path != "/undocumented" {
		t.Fatalf("expected undocumented route, got %+v", coverage.MissingRoutes)
	}
	if coverage.TotalSymbols != 2 || coverage.DocumentedSymbols != 1 || len(coverage.MissingSymbols) != 1 {
		t.Fatalf("unexpected symbol coverage report: %+v", coverage)
	}
	if coverage.MissingSymbols[0].Name != "UndocumentedThing" {
		t.Fatalf("expected undocumented symbol, got %+v", coverage.MissingSymbols)
	}
}

func TestImpactAutoDetectsFileAndSymbol(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer eng.Close()
	ctx := context.Background()

	coreID, err := eng.store.UpsertFile(ctx, &api.FileInfo{Path: "core.go", Language: api.Go, ContentHash: "core", Size: 10})
	if err != nil {
		t.Fatalf("upsert core: %v", err)
	}
	userID, err := eng.store.UpsertFile(ctx, &api.FileInfo{Path: "user.go", Language: api.Go, ContentHash: "user", Size: 10})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	if err := eng.store.ReplaceSymbols(ctx, coreID, []api.Symbol{{Name: "Target", Kind: api.Function, FilePath: "core.go", Line: 3, EndLine: 5}}); err != nil {
		t.Fatalf("replace symbols: %v", err)
	}
	if err := eng.store.ReplaceImports(ctx, userID, []api.ImportEdge{{FromFile: "user.go", ToSource: "core.go", Line: 2}}); err != nil {
		t.Fatalf("replace imports: %v", err)
	}
	if err := eng.store.ReplaceCalls(ctx, userID, []api.CallEdge{{FromFile: "user.go", FromSymbol: "Use", ToName: "Target", Line: 8, Confidence: "HEURISTIC"}}); err != nil {
		t.Fatalf("replace calls: %v", err)
	}

	fileImpact, err := eng.Impact(ctx, "core.go", 2)
	if err != nil {
		t.Fatalf("file impact: %v", err)
	}
	if fileImpact.Kind != "file" || fileImpact.FileImpact == nil || len(fileImpact.FileImpact.Dependents) != 1 || fileImpact.FileImpact.Dependents[0] != "user.go" {
		t.Fatalf("unexpected file impact: %+v", fileImpact)
	}

	symbolImpact, err := eng.Impact(ctx, "Target", 2)
	if err != nil {
		t.Fatalf("symbol impact: %v", err)
	}
	if symbolImpact.Kind != "symbol" || symbolImpact.SymbolImpact == nil {
		t.Fatalf("unexpected symbol impact wrapper: %+v", symbolImpact)
	}
	if len(symbolImpact.SymbolImpact.Callers) != 1 || symbolImpact.SymbolImpact.Callers[0].FromFile != "user.go" {
		t.Fatalf("unexpected symbol callers: %+v", symbolImpact.SymbolImpact.Callers)
	}
	if len(symbolImpact.SymbolImpact.Dependents) != 1 || symbolImpact.SymbolImpact.Dependents[0] != "user.go" {
		t.Fatalf("unexpected symbol dependents: %+v", symbolImpact.SymbolImpact.Dependents)
	}
}

func TestSnapshotAndContextIncludeHybridHits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer eng.Close()
	ctx := context.Background()
	if _, err := eng.Index(ctx, false); err != nil {
		t.Fatalf("index: %v", err)
	}

	snapshot, err := eng.Snapshot(ctx, "Foo", 2)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.HybridHits) == 0 {
		t.Fatalf("snapshot hybrid hits = %#v, want non-empty", snapshot.HybridHits)
	}

	symbolContext, err := eng.Context(ctx, "Foo")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if len(symbolContext.HybridHits) == 0 {
		t.Fatalf("context hybrid hits = %#v, want non-empty", symbolContext.HybridHits)
	}
}

func TestEmbeddingPlanReportsMissingAndCachedChunks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer eng.Close()
	ctx := context.Background()
	if _, err := eng.Index(ctx, false); err != nil {
		t.Fatalf("index without embeddings: %v", err)
	}

	eng.embedder = fakeEmbedder{}
	missing, err := eng.EmbeddingPlan(ctx, 10)
	if err != nil {
		t.Fatalf("embedding plan missing: %v", err)
	}
	if !missing.Enabled || !missing.CacheSupported || missing.TotalChunks == 0 || missing.MissingChunks == 0 || !missing.BackfillRequired {
		t.Fatalf("missing plan = %+v, want pending backfill", missing)
	}
	if len(missing.Items) == 0 || missing.Items[0].Status != "missing" {
		t.Fatalf("missing items = %+v, want missing item", missing.Items)
	}

	eng.indexer.SetEmbedder(fakeEmbedder{})
	if _, err := eng.Index(ctx, false); err != nil {
		t.Fatalf("index with embeddings: %v", err)
	}
	cached, err := eng.EmbeddingPlan(ctx, 10)
	if err != nil {
		t.Fatalf("embedding plan cached: %v", err)
	}
	if cached.BackfillRequired || cached.CachedChunks != cached.TotalChunks || cached.MissingChunks != 0 {
		t.Fatalf("cached plan = %+v, want complete cache", cached)
	}
	if len(cached.Namespaces) != 1 || cached.Namespaces[0].Model != "fake" || cached.Namespaces[0].Dimensions != 1 {
		t.Fatalf("namespaces = %+v, want fake/1", cached.Namespaces)
	}
}
