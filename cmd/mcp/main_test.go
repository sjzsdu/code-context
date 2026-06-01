package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/engine"
)

func TestRunGraphTool(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runGraphTool(context.Background(), eng, GraphArgs{})
	if err != nil {
		t.Fatalf("run graph tool: %v", err)
	}
	if !strings.Contains(out, "\"version\": \"graph-export.v2\"") {
		t.Fatalf("expected graph export version, got:\n%s", out)
	}
	if !strings.Contains(out, "\"analysis\"") {
		t.Fatalf("expected graph analysis output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"module\"") {
		t.Fatalf("expected module nodes in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"package\"") {
		t.Fatalf("expected package nodes in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"declares_package\"") {
		t.Fatalf("expected declares_package edges in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\": \"represents\"") {
		t.Fatalf("expected represents edges in output, got:\n%s", out)
	}
}

func TestRunGraphPathTool(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runGraphPathTool(context.Background(), eng, GraphPathArgs{From: "Foo", To: "Bar"})
	if err != nil {
		t.Fatalf("run graph path tool: %v", err)
	}
	if !strings.Contains(out, "\"path_found\": true") {
		t.Fatalf("expected path_found true, got:\n%s", out)
	}
	if !strings.Contains(out, "\"from_file\": ") {
		t.Fatalf("expected from_file in output, got:\n%s", out)
	}
}

func TestRunGraphNeighborsTool(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runGraphNeighborsTool(context.Background(), eng, GraphNeighborsArgs{Target: "Foo", Limit: 2})
	if err != nil {
		t.Fatalf("run graph neighbors tool: %v", err)
	}
	if !strings.Contains(out, "\"resolved_file\": \"a.go\"") {
		t.Fatalf("expected resolved file in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"related_files\"") {
		t.Fatalf("expected related files in output, got:\n%s", out)
	}
}

func TestRunGraphSubgraphTool(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runGraphSubgraphTool(context.Background(), eng, GraphSubgraphArgs{Target: "Foo", Depth: 1})
	if err != nil {
		t.Fatalf("run graph subgraph tool: %v", err)
	}
	if !strings.Contains(out, "\"resolved_file\": \"a.go\"") {
		t.Fatalf("expected resolved file in output, got:\n%s", out)
	}
	if !strings.Contains(out, "\"graph\"") {
		t.Fatalf("expected graph payload in output, got:\n%s", out)
	}
}

func TestRunImpactToolForFileAndSymbol(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	fileImpact, err := runImpactTool(context.Background(), eng, ImpactArgs{Target: "a.go", Depth: 2})
	if err != nil {
		t.Fatalf("run file impact tool: %v", err)
	}
	if fileImpact.Kind != "file" || fileImpact.FileImpact == nil {
		t.Fatalf("expected file impact, got: %+v", fileImpact)
	}

	symbolImpact, err := runImpactTool(context.Background(), eng, ImpactArgs{Target: "Foo"})
	if err != nil {
		t.Fatalf("run symbol impact tool: %v", err)
	}
	if symbolImpact.Kind != "symbol" || symbolImpact.SymbolImpact == nil {
		t.Fatalf("expected symbol impact, got: %+v", symbolImpact)
	}

	out := formatImpactMarkdown(symbolImpact)
	if !strings.Contains(out, "# Impact: `Foo` (symbol)") || !strings.Contains(out, "# Symbol Impact: `Foo`") {
		t.Fatalf("expected formatted symbol impact, got:\n%s", out)
	}
}

func TestRunImpactToolRequiresTarget(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	_, err := runImpactTool(context.Background(), eng, ImpactArgs{})
	if err == nil || !strings.Contains(err.Error(), "missing required parameter: target") {
		t.Fatalf("expected missing target error, got: %v", err)
	}
}

func TestRunImpactGitTool(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()
	root := eng.Root()
	runGitCmd(t, root, "init")
	runGitCmd(t, root, "config", "user.email", "test@example.com")
	runGitCmd(t, root, "config", "user.name", "Test User")
	runGitCmd(t, root, "add", "a.go", "b.go")
	runGitCmd(t, root, "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc Foo() {\n\tfmt.Println(\"foo2\")\n}\n"), 0o644); err != nil {
		t.Fatalf("write change: %v", err)
	}

	impact, err := runImpactGitTool(context.Background(), eng, GitImpactArgs{State: "all", Depth: 2})
	if err != nil {
		t.Fatalf("run impact git tool: %v", err)
	}
	if len(impact.ChangedFiles) == 0 || impact.Summary == "" || impact.Risk.Level == "" {
		t.Fatalf("unexpected git impact: %+v", impact)
	}
	out := formatGitImpactMarkdown(impact)
	if !strings.Contains(out, "# Git Impact") || !strings.Contains(out, "Changed Files") || !strings.Contains(out, "## Risk") {
		t.Fatalf("unexpected formatted git impact:\n%s", out)
	}
}

func TestRunGraphPathToolRequiresArgs(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	_, err := runGraphPathTool(context.Background(), eng, GraphPathArgs{From: "Foo"})
	if err == nil || !strings.Contains(err.Error(), "missing required parameters") {
		t.Fatalf("expected missing parameter error, got: %v", err)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func TestRunGraphNeighborsToolRequiresTarget(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	_, err := runGraphNeighborsTool(context.Background(), eng, GraphNeighborsArgs{})
	if err == nil || !strings.Contains(err.Error(), "missing required parameter: target") {
		t.Fatalf("expected missing target error, got: %v", err)
	}
}

func TestRunGraphSubgraphToolRequiresTarget(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	_, err := runGraphSubgraphTool(context.Background(), eng, GraphSubgraphArgs{})
	if err == nil || !strings.Contains(err.Error(), "missing required parameter: target") {
		t.Fatalf("expected missing target error, got: %v", err)
	}
}

func newTestEngine(t *testing.T) (*engine.Engine, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "mcp-graph-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	write := func(name string, lines []string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	write("a.go", []string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func Foo() {",
		"\tfmt.Println(\"foo\")",
		"}",
	})
	write("b.go", []string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func Bar() {",
		"\tfmt.Println(\"bar\")",
		"}",
	})

	dbPath := filepath.Join(tmpDir, "index.db")
	eng, err := engine.New(tmpDir, dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("create engine: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		eng.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("index repo: %v", err)
	}

	cleanup := func() {
		_ = eng.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return eng, cleanup
}
