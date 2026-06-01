package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
