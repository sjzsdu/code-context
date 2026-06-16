package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/sjzsdu/code-context/internal/engine"
)

func TestStatsCmdIncludesIndexMetadata(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-stats-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "a.go")
	if err := os.WriteFile(path, []byte("package main\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newStatsCmd()
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute stats cmd: %v", err)
	}
	if !strings.Contains(out, "Index version:") {
		t.Fatalf("expected index version in stats output, got:\n%s", out)
	}
	if !strings.Contains(out, "Last indexed:") {
		t.Fatalf("expected last indexed timestamp in stats output, got:\n%s", out)
	}
}

func TestStatusCmdIncludesWorkflowMetadata(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-status-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "a.go")
	if err := os.WriteFile(path, []byte("package main\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newStatusCmd()
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute status cmd: %v", err)
	}
	if !strings.Contains(out, "Graph version:") {
		t.Fatalf("expected graph version in status output, got:\n%s", out)
	}
	if !strings.Contains(out, "Capabilities:") {
		t.Fatalf("expected capabilities in status output, got:\n%s", out)
	}
	if !strings.Contains(out, "Watch enabled:") {
		t.Fatalf("expected watch metadata in status output, got:\n%s", out)
	}
	if !strings.Contains(out, "Index version:") {
		t.Fatalf("expected index metadata in status output, got:\n%s", out)
	}
}

func TestGraphCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-graph-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "a.go")
	if err := os.WriteFile(path, []byte("package main\nimport \"fmt\"\nfunc A() { fmt.Println(\"a\") }\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newGraphCmd()
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute graph cmd: %v", err)
	}
	if !strings.Contains(out, "\"version\": \"graph-export.v2\"") {
		t.Fatalf("expected graph version output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"file\"") {
		t.Fatalf("expected file nodes in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"module\"") {
		t.Fatalf("expected module nodes in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"package\"") {
		t.Fatalf("expected package nodes in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"imports\"") {
		t.Fatalf("expected import edges in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"belongs_to\"") {
		t.Fatalf("expected belongs_to edges in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"declares_package\"") {
		t.Fatalf("expected declares_package edges in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"represents\"") {
		t.Fatalf("expected represents edges in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"analysis\"") {
		t.Fatalf("expected graph analysis in output, got:\n%s", out)
	}
}

func TestGraphTraverseCmdReportsUnsupportedBackend(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-graph-traverse-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	cmd := newGraphCmd()
	cmd.SetArgs([]string{"traverse", "--kind", "file", "--path", "a.go"})
	_, err = captureStdout(func() error { return cmd.Execute() })
	if err == nil || !strings.Contains(err.Error(), "capability unsupported") {
		t.Fatalf("expected unsupported capability error, got %v", err)
	}
}

func TestVectorSearchCmdReportsUnsupportedBackend(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-vector-search-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	prevRoot, prevDB, prevStoreBackend := root, dbPath, storeBackend
	prevEmbeddingProvider, prevEmbeddingModel, prevEmbeddingDimensions := embeddingProvider, embeddingModel, embeddingDimensions
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	storeBackend = ""
	embeddingProvider = ""
	embeddingModel = ""
	embeddingDimensions = 0
	defer func() {
		root, dbPath, storeBackend = prevRoot, prevDB, prevStoreBackend
		embeddingProvider, embeddingModel, embeddingDimensions = prevEmbeddingProvider, prevEmbeddingModel, prevEmbeddingDimensions
	}()

	cmd := newVectorSearchCmd()
	cmd.SetArgs([]string{"--vector", "1", "--model", "fake"})
	_, err = captureStdout(func() error { return cmd.Execute() })
	if err == nil || !strings.Contains(err.Error(), "capability unsupported") {
		t.Fatalf("expected unsupported capability error, got %v", err)
	}
}

func TestParseFloat32List(t *testing.T) {
	got, err := parseFloat32List("1, 0.5, -2")
	if err != nil {
		t.Fatalf("parseFloat32List: %v", err)
	}
	want := []float32{1, 0.5, -2}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestGraphPathCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-graph-path-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte(strings.Join([]string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func Foo() {",
		"\tfmt.Println(\"foo\")",
		"}",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte(strings.Join([]string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func Bar() {",
		"\tfmt.Println(\"bar\")",
		"}",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newGraphPathCmd()
	cmd.SetArgs([]string{"Foo", "Bar"})
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute graph path cmd: %v", err)
	}
	if !strings.Contains(out, "Graph path: Foo -> Bar") {
		t.Fatalf("expected graph path header, got:\n%s", out)
	}
	if !strings.Contains(out, "1. a.go") || !strings.Contains(out, "2. b.go") {
		t.Fatalf("expected path files in output, got:\n%s", out)
	}
}

func TestGraphNeighborsCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-graph-neighbors-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte(strings.Join([]string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func Foo() {",
		"\tfmt.Println(\"foo\")",
		"}",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte(strings.Join([]string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func Bar() {",
		"\tfmt.Println(\"bar\")",
		"}",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newGraphNeighborsCmd()
	cmd.SetArgs([]string{"Foo", "--limit", "2"})
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute graph neighbors cmd: %v", err)
	}
	if !strings.Contains(out, "Graph neighbors: Foo") {
		t.Fatalf("expected graph neighbors header, got:\n%s", out)
	}
	if !strings.Contains(out, "Resolved file: a.go") {
		t.Fatalf("expected resolved file output, got:\n%s", out)
	}
	if !strings.Contains(out, "Imports") || !strings.Contains(out, "fmt") {
		t.Fatalf("expected imports output, got:\n%s", out)
	}
}

func TestGraphSubgraphCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-graph-subgraph-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte(strings.Join([]string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func Foo() {",
		"\tfmt.Println(\"foo\")",
		"}",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte(strings.Join([]string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func Bar() {",
		"\tfmt.Println(\"bar\")",
		"}",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newGraphSubgraphCmd()
	cmd.SetArgs([]string{"Foo", "--depth", "1"})
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute graph subgraph cmd: %v", err)
	}
	if !strings.Contains(out, "\"resolved_file\": ") || !strings.Contains(out, "a.go") {
		t.Fatalf("expected resolved file in JSON output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"files\"") || !strings.Contains(out, "b.go") {
		t.Fatalf("expected subgraph files in output, got:\n%s", out)
	}
}

func TestGraphHTMLCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-graph-html-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for name, body := range map[string]string{
		"a.go": "package main\nimport \"fmt\"\nfunc A() { fmt.Println(\"a\") }\n",
		"b.go": "package main\nimport \"fmt\"\nfunc B() { fmt.Println(\"b\") }\n",
	} {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newGraphHTMLCmd()
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute graph html cmd: %v", err)
	}
	if !strings.Contains(out, "<!DOCTYPE html>") {
		t.Fatalf("expected html doctype, got:\n%s", out)
	}
	if !strings.Contains(out, "code-context graph view") {
		t.Fatalf("expected graph HTML title, got:\n%s", out)
	}
	if !strings.Contains(out, "Graph analysis") {
		t.Fatalf("expected graph analysis section, got:\n%s", out)
	}
	if !strings.Contains(out, "graphSurface") {
		t.Fatalf("expected visual graph svg, got:\n%s", out)
	}
	if !strings.Contains(out, "Document mode") {
		t.Fatalf("expected document mode controls, got:\n%s", out)
	}
	if !strings.Contains(out, "Zoom to fit") {
		t.Fatalf("expected graph canvas controls, got:\n%s", out)
	}
	if !strings.Contains(out, "Focus depth") {
		t.Fatalf("expected focus depth controls, got:\n%s", out)
	}
	if !strings.Contains(out, "minimapCanvas") {
		t.Fatalf("expected minimap canvas, got:\n%s", out)
	}
	if !strings.Contains(out, "hoverCard") {
		t.Fatalf("expected hover card support, got:\n%s", out)
	}
	if !strings.Contains(out, "Shift + drag to box zoom") {
		t.Fatalf("expected marquee zoom hint, got:\n%s", out)
	}
	if !strings.Contains(out, "minimapButton") {
		t.Fatalf("expected clickable minimap control, got:\n%s", out)
	}
	if !strings.Contains(out, "nodeContextMenu") {
		t.Fatalf("expected node context menu support, got:\n%s", out)
	}
	if !strings.Contains(out, "contentModal") {
		t.Fatalf("expected content modal support, got:\n%s", out)
	}
	if !strings.Contains(out, "Open node content") {
		t.Fatalf("expected node content action, got:\n%s", out)
	}
	if !strings.Contains(out, "Copy content") {
		t.Fatalf("expected copy content action, got:\n%s", out)
	}
	if !strings.Contains(out, "Expand view") {
		t.Fatalf("expected expand content action, got:\n%s", out)
	}
	if !strings.Contains(out, "Show file path") {
		t.Fatalf("expected file path action, got:\n%s", out)
	}
	if !strings.Contains(out, "selectionActions") {
		t.Fatalf("expected visible selection actions, got:\n%s", out)
	}
	if !strings.Contains(out, "Open content") {
		t.Fatalf("expected selection open content action, got:\n%s", out)
	}
	if !strings.Contains(out, "Pin node") {
		t.Fatalf("expected selection pin action, got:\n%s", out)
	}
	if !strings.Contains(out, "Bridge files") {
		t.Fatalf("expected bridge files section, got:\n%s", out)
	}
	if !strings.Contains(out, "Reading paths") {
		t.Fatalf("expected reading paths section, got:\n%s", out)
	}
}

func TestMapCmdIncludesGraphAnalysis(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-map-analysis-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for name, body := range map[string]string{
		"a.go": "package main\nimport \"fmt\"\nfunc Foo() { fmt.Println(\"foo\") }\n",
		"b.go": "package main\nimport \"fmt\"\nfunc Bar() { fmt.Println(\"bar\") }\n",
	} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newMapCmd()
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute map cmd: %v", err)
	}
	if !strings.Contains(out, "Graph analysis:") {
		t.Fatalf("expected graph analysis in map output, got:\n%s", out)
	}
	if !strings.Contains(out, "Bridge files:") {
		t.Fatalf("expected bridge files in map output, got:\n%s", out)
	}
	if !strings.Contains(out, "Reading paths:") {
		t.Fatalf("expected reading paths in map output, got:\n%s", out)
	}
}

func TestContextCmdIncludesGraphGuidance(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-context-analysis-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for name, body := range map[string]string{
		"a.go": "package main\nimport \"fmt\"\nfunc Foo() { fmt.Println(\"foo\") }\n",
		"b.go": "package main\nimport \"fmt\"\nfunc Bar() { fmt.Println(\"bar\") }\n",
	} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newContextCmd()
	cmd.SetArgs([]string{"Foo"})
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute context cmd: %v", err)
	}
	if !strings.Contains(out, "Graph:") {
		t.Fatalf("expected graph summary in context output, got:\n%s", out)
	}
	if !strings.Contains(out, "Related files:") {
		t.Fatalf("expected related files in context output, got:\n%s", out)
	}
	if !strings.Contains(out, "Bridge files:") {
		t.Fatalf("expected bridge files in context output, got:\n%s", out)
	}
	if !strings.Contains(out, "Relation highlights:") {
		t.Fatalf("expected relation highlights in context output, got:\n%s", out)
	}
	if !strings.Contains(out, "Reading paths:") {
		t.Fatalf("expected reading paths in context output, got:\n%s", out)
	}
}

func TestSnapshotCmdIncludesGraphGuidance(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-snapshot-analysis-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for name, body := range map[string]string{
		"a.go": "package main\nimport \"fmt\"\nfunc Foo() { fmt.Println(\"foo\") }\n",
		"b.go": "package main\nimport \"fmt\"\nfunc Bar() { fmt.Println(\"bar\") }\n",
	} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newSnapshotCmd()
	cmd.SetArgs([]string{"Foo", "--limit", "1"})
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute snapshot cmd: %v", err)
	}
	if !strings.Contains(out, "Recommended next files:") {
		t.Fatalf("expected recommended files in snapshot output, got:\n%s", out)
	}
	if !strings.Contains(out, "Graph analysis:") {
		t.Fatalf("expected graph analysis in snapshot output, got:\n%s", out)
	}
	if !strings.Contains(out, "Bridge files:") {
		t.Fatalf("expected bridge files in snapshot output, got:\n%s", out)
	}
	if !strings.Contains(out, "Reading paths:") {
		t.Fatalf("expected reading paths in snapshot output, got:\n%s", out)
	}
}

func TestSnapshotCmdAcceptsNoQuery(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package main\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newSnapshotCmd()
	cmd.SetArgs([]string{"--limit", "1"})
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute snapshot cmd without query: %v", err)
	}
	if !strings.Contains(out, "=== Code Snapshot ===") || !strings.Contains(out, "--- a.go ---") {
		t.Fatalf("expected snapshot output for indexed project, got:\n%s", out)
	}
}

func TestSnapshotGitCmdIncludesGraphGuidance(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-snapshot-git-analysis-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pathA := filepath.Join(tmpDir, "a.go")
	pathB := filepath.Join(tmpDir, "b.go")
	for path, body := range map[string]string{
		pathA: "package main\nimport \"fmt\"\nfunc Foo() { fmt.Println(\"foo\") }\n",
		pathB: "package main\nimport \"fmt\"\nfunc Bar() { fmt.Println(\"bar\") }\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	runGitCLI(t, tmpDir, "init")
	runGitCLI(t, tmpDir, "add", "a.go", "b.go")
	runGitCLI(t, tmpDir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")

	if err := os.WriteFile(pathA, []byte("package main\nimport \"fmt\"\nfunc Foo() { fmt.Println(\"foo2\") }\n"), 0o644); err != nil {
		t.Fatalf("write changed file a.go: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("package main\nimport \"fmt\"\nfunc Bar() { fmt.Println(\"bar2\") }\n"), 0o644); err != nil {
		t.Fatalf("write changed file b.go: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	eng, err := engine.New(root, dbPath)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		t.Fatalf("index repo: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	cmd := newSnapshotGitCmd()
	cmd.SetArgs([]string{"--state", "all", "--limit", "2"})
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute snapshot-git cmd: %v", err)
	}
	if !strings.Contains(out, "Recommended next files:") {
		t.Fatalf("expected recommended files in snapshot-git output, got:\n%s", out)
	}
	if !strings.Contains(out, "Graph analysis:") {
		t.Fatalf("expected graph analysis in snapshot-git output, got:\n%s", out)
	}
	if !strings.Contains(out, "Graph:") {
		t.Fatalf("expected per-file graph summary in snapshot-git output, got:\n%s", out)
	}
	if !strings.Contains(out, "Bridge files:") {
		t.Fatalf("expected bridge files in snapshot-git output, got:\n%s", out)
	}
	if !strings.Contains(out, "Relation highlights:") {
		t.Fatalf("expected relation highlights in snapshot-git output, got:\n%s", out)
	}
}

func TestWatchCmdRequiresEnablement(t *testing.T) {
	prevRoot, prevDB := root, dbPath
	root = t.TempDir()
	dbPath = filepath.Join(root, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	cmd := newWatchCmd()
	cmd.SetArgs(nil)
	_, err := captureStdout(func() error { return cmd.Execute() })
	if err == nil || !strings.Contains(err.Error(), "watch mode is disabled") {
		t.Fatalf("expected disabled watch error, got %v", err)
	}
}

func TestWatchCmdUsesConfigDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".code-context.yaml")
	content := []byte("watch:\n  enabled: true\n  interval: 3s\n  debounce: 400ms\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().StringVarP(&root, "root", "r", ".", "")
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "", "")
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	watchCmd := newWatchCmd()
	serveCmd := newServeCmd()
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(serveCmd)
	attachWatchConfig(rootCmd)
	attachServeConfig(rootCmd)

	watchFound, _, err := rootCmd.Find([]string{"watch"})
	if err != nil {
		t.Fatalf("find watch cmd: %v", err)
	}
	if err := watchFound.PreRunE(watchFound, nil); err != nil {
		t.Fatalf("watch pre-run: %v", err)
	}

	interval, err := watchFound.Flags().GetDuration("interval")
	if err != nil {
		t.Fatalf("get interval: %v", err)
	}
	if interval != 3*time.Second {
		t.Fatalf("interval = %s, want 3s", interval)
	}
	debounce, err := watchFound.Flags().GetDuration("debounce")
	if err != nil {
		t.Fatalf("get debounce: %v", err)
	}
	if debounce != 400*time.Millisecond {
		t.Fatalf("debounce = %s, want 400ms", debounce)
	}
	enabled, err := watchFound.Flags().GetBool("enabled")
	if err != nil {
		t.Fatalf("get enabled: %v", err)
	}
	if !enabled {
		t.Fatalf("expected enabled flag from config")
	}

	serveFound, _, err := rootCmd.Find([]string{"serve"})
	if err != nil {
		t.Fatalf("find serve cmd: %v", err)
	}
	if err := serveFound.PreRunE(serveFound, nil); err != nil {
		t.Fatalf("serve pre-run: %v", err)
	}
	serveWatch, err := serveFound.Flags().GetBool("watch")
	if err != nil {
		t.Fatalf("get serve watch: %v", err)
	}
	if !serveWatch {
		t.Fatalf("expected serve watch flag from config")
	}
	serveInterval, err := serveFound.Flags().GetDuration("watch-interval")
	if err != nil {
		t.Fatalf("get serve interval: %v", err)
	}
	if serveInterval != 3*time.Second {
		t.Fatalf("serve watch interval = %s, want 3s", serveInterval)
	}
	serveDebounce, err := serveFound.Flags().GetDuration("watch-debounce")
	if err != nil {
		t.Fatalf("get serve debounce: %v", err)
	}
	if serveDebounce != 400*time.Millisecond {
		t.Fatalf("serve watch debounce = %s, want 400ms", serveDebounce)
	}
}

func TestGitDiffCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-git-diff-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "a.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	runGitCLI(t, tmpDir, "init")
	runGitCLI(t, tmpDir, "add", "a.go")
	runGitCLI(t, tmpDir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")

	if err := os.WriteFile(path, []byte("package main\n\nfunc A() string { return \"a\" }\n"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	cmd := newGitDiffCmd()
	cmd.SetArgs([]string{"--state", "unstaged", "--context", "1"})

	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute git-diff cmd: %v", err)
	}
	if !strings.Contains(out, "File: a.go") {
		t.Fatalf("expected file output, got:\n%s", out)
	}
	if !strings.Contains(out, "@@ -") {
		t.Fatalf("expected hunk header output, got:\n%s", out)
	}
}

func TestConfigInspectCmdPrintsMergedConfigAndSources(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	projectDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(filepath.Join(homeDir, ".code-context"), 0o755); err != nil {
		t.Fatalf("mkdir home config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, ".code-context"), 0o755); err != nil {
		t.Fatalf("mkdir project config: %v", err)
	}
	t.Setenv("HOME", homeDir)

	userConfigPath := filepath.Join(homeDir, ".code-context", "config.yaml")
	if err := os.WriteFile(userConfigPath, []byte("store:\n  backend: helix\nserver:\n  port: 7070\n"), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	projectConfigPath := filepath.Join(projectDir, ".code-context", "config.yaml")
	if err := os.WriteFile(projectConfigPath, []byte("root: .\nserver:\n  port: 9090\n"), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	prevRoot := root
	root = projectDir
	defer func() { root = prevRoot }()

	cmd := newConfigInspectCmd()
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute config inspect: %v", err)
	}

	var decoded struct {
		Sources []struct {
			Path string `json:"path"`
		} `json:"sources"`
		Config struct {
			Root   string                   `json:"root"`
			Store  struct{ Backend string } `json:"store"`
			Server struct{ Port int }       `json:"server"`
		} `json:"config"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode config inspect output: %v\n%s", err, out)
	}
	if len(decoded.Sources) != 2 {
		t.Fatalf("sources = %d, want 2; output:\n%s", len(decoded.Sources), out)
	}
	if decoded.Sources[0].Path != userConfigPath || decoded.Sources[1].Path != projectConfigPath {
		t.Fatalf("sources = %#v", decoded.Sources)
	}
	if decoded.Config.Root != projectDir {
		t.Fatalf("root = %q, want %q", decoded.Config.Root, projectDir)
	}
	if decoded.Config.Store.Backend != "helix" {
		t.Fatalf("store.backend = %q", decoded.Config.Store.Backend)
	}
	if decoded.Config.Server.Port != 9090 {
		t.Fatalf("server.port = %d", decoded.Config.Server.Port)
	}
}

func TestOnboardCmdCreatesConfigAndRefusesOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	cmd := newOnboardCmd()
	cmd.SetArgs([]string{"--dir", tmpDir})
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute onboard: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".code-context", "config.yaml")
	if !strings.Contains(out, configPath) {
		t.Fatalf("expected created path in output, got:\n%s", out)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if !strings.Contains(string(data), "store:") || !strings.Contains(string(data), "watch:") {
		t.Fatalf("generated config missing expected sections:\n%s", string(data))
	}

	cmd = newOnboardCmd()
	cmd.SetArgs([]string{"--dir", tmpDir})
	_, err = captureStdout(func() error { return cmd.Execute() })
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
}

func TestOnboardCmdGlobalCreatesUserConfig(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	t.Setenv("HOME", homeDir)

	cmd := newOnboardCmd()
	cmd.SetArgs([]string{"--global"})
	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute onboard --global: %v", err)
	}

	configPath := filepath.Join(homeDir, ".code-context", "config.yaml")
	if !strings.Contains(out, configPath) {
		t.Fatalf("expected global config path in output, got:\n%s", out)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read generated global config: %v", err)
	}
	if !strings.Contains(string(data), "root: .") {
		t.Fatalf("generated global config missing root:\n%s", string(data))
	}
}

func runGitCLI(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func captureStdout(fn func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	var buf bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		_, _ = buf.ReadFrom(r)
		_ = r.Close()
		close(readDone)
	}()

	runErr := fn()
	_ = w.Close()
	<-readDone
	return buf.String(), runErr
}
