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
}

func handler(w http.ResponseWriter, r *http.Request) {}
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
}
