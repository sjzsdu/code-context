package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/engine"
)

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
	if !strings.Contains(out, "\"version\": \"graph-export.v1\"") {
		t.Fatalf("expected graph version output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"file\"") {
		t.Fatalf("expected file nodes in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"imports\"") {
		t.Fatalf("expected import edges in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"analysis\"") {
		t.Fatalf("expected graph analysis in output, got:\n%s", out)
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

	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()
	return buf.String(), runErr
}
