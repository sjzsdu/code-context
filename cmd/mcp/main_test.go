package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/engine"
	"github.com/sjzsdu/code-context/internal/store"
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

func TestRunGraphTraverseToolRequiresStart(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	_, err := runGraphTraverseTool(context.Background(), eng, GraphTraverseArgs{})
	if err == nil || !strings.Contains(err.Error(), "start") {
		t.Fatalf("expected missing start error, got %v", err)
	}
}

func TestRunGraphTraverseToolUnsupported(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	_, err := runGraphTraverseTool(context.Background(), eng, GraphTraverseArgs{
		Start: store.TargetRef{Kind: store.TargetFile, Path: "a.go"},
		Limit: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "capability unsupported") {
		t.Fatalf("expected unsupported capability error, got %v", err)
	}
}

func TestRunVectorSearchToolRequiresQueryOrVector(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	_, err := runVectorSearchTool(context.Background(), eng, VectorSearchArgs{})
	if err == nil || !strings.Contains(err.Error(), "query_text or vector") {
		t.Fatalf("expected missing vector query error, got %v", err)
	}
}

func TestRunVectorSearchToolUnsupported(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	_, err := runVectorSearchTool(context.Background(), eng, VectorSearchArgs{
		Vector: []float32{1},
		Model:  "fake",
		Limit:  5,
	})
	if err == nil || !strings.Contains(err.Error(), "capability unsupported") {
		t.Fatalf("expected unsupported capability error, got %v", err)
	}
}

func TestRunEmbeddingPlanToolDisabled(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runEmbeddingPlanTool(context.Background(), eng, FreshnessArgs{Limit: 2})
	if err != nil {
		t.Fatalf("run embedding plan tool: %v", err)
	}
	if !strings.Contains(out, "\"enabled\": false") || !strings.Contains(out, "embedding provider is disabled") {
		t.Fatalf("expected disabled embedding plan, got:\n%s", out)
	}

	out, err = runEmbeddingStatusTool(context.Background(), eng, FreshnessArgs{Limit: 2})
	if err != nil {
		t.Fatalf("run embedding status tool: %v", err)
	}
	if !strings.Contains(out, "\"enabled\": false") || !strings.Contains(out, "configure_embedding") {
		t.Fatalf("expected disabled embedding lifecycle status, got:\n%s", out)
	}

	out, err = runEmbeddingBackfillTool(context.Background(), eng, EmbeddingBackfillArgs{Limit: 2})
	if err != nil {
		t.Fatalf("run embedding backfill tool: %v", err)
	}
	if !strings.Contains(out, "\"dry_run\": true") || !strings.Contains(out, "embedding provider is disabled") {
		t.Fatalf("expected disabled embedding backfill dry run, got:\n%s", out)
	}

	out, err = runEmbeddingNamespacesTool(context.Background(), eng)
	if err != nil {
		t.Fatalf("run embedding namespaces tool: %v", err)
	}
	if !strings.Contains(out, "\"cache_supported\": true") || !strings.Contains(out, "no embedding namespaces found") {
		t.Fatalf("expected empty embedding namespace inventory, got:\n%s", out)
	}

	out, err = runEmbeddingPruneTool(context.Background(), eng, EmbeddingPruneArgs{Model: "fake", Dimensions: 3})
	if err != nil {
		t.Fatalf("run embedding prune tool: %v", err)
	}
	if !strings.Contains(out, "\"dry_run\": true") || !strings.Contains(out, "embedding namespace fake/3 was not found") {
		t.Fatalf("expected dry-run prune miss, got:\n%s", out)
	}
}

func TestRunProviderDiagnosticsTool(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runProviderDiagnosticsTool(context.Background(), eng)
	if err != nil {
		t.Fatalf("run provider diagnostics: %v", err)
	}
	if !strings.Contains(out, "\"ok\": true") ||
		!strings.Contains(out, "\"kind\": \"embedding\"") ||
		!strings.Contains(out, "\"kind\": \"answer\"") {
		t.Fatalf("expected provider diagnostics output, got:\n%s", out)
	}
}

func TestRunHybridSearchTool(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runHybridSearchTool(context.Background(), eng, HybridSearchArgs{Query: "Foo", Limit: 5})
	if err != nil {
		t.Fatalf("run hybrid search tool: %v", err)
	}
	if !strings.Contains(out, "\"results\"") || !strings.Contains(out, "Foo") {
		t.Fatalf("expected hybrid results, got:\n%s", out)
	}
}

func TestRunAnswerToolMarkdownContextOnly(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runAnswerTool(context.Background(), eng, AnswerArgs{Question: "Foo", ContextOnly: true, Format: "markdown", Limit: 3})
	if err != nil {
		t.Fatalf("run answer tool: %v", err)
	}
	if !strings.Contains(out, "# Answer") ||
		!strings.Contains(out, "**Question:** Foo") ||
		!strings.Contains(out, "Context-only") ||
		!strings.Contains(out, "## Sources") ||
		!strings.Contains(out, "[1]") {
		t.Fatalf("expected markdown answer context, got:\n%s", out)
	}
}

func TestRunAnswerToolJSONContextOnly(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runAnswerTool(context.Background(), eng, AnswerArgs{
		Question:            "Foo",
		Profile:             engine.AnswerProfileExplainCode,
		ContextOnly:         true,
		Format:              "json",
		Limit:               3,
		DedupeContext:       true,
		MaxContextItemChars: 80,
	})
	if err != nil {
		t.Fatalf("run answer tool json: %v", err)
	}
	if !strings.Contains(out, "\"question\": \"Foo\"") ||
		!strings.Contains(out, "\"profile\": \"explain-code\"") ||
		!strings.Contains(out, "\"template\": \"explain\"") ||
		!strings.Contains(out, "\"retrieval\"") ||
		!strings.Contains(out, "\"dedupe_context\": true") ||
		!strings.Contains(out, "\"sources\"") {
		t.Fatalf("expected JSON answer context, got:\n%s", out)
	}
}

func TestRunAnswerToolJSONIncludesEvaluation(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	out, err := runAnswerTool(context.Background(), eng, AnswerArgs{
		Question:    "Foo",
		ContextOnly: true,
		Evaluate:    true,
		Format:      "json",
		Limit:       3,
	})
	if err != nil {
		t.Fatalf("run answer tool json evaluation: %v", err)
	}
	if !strings.Contains(out, "\"evaluation\"") ||
		!strings.Contains(out, "\"evaluator\": \"local-rule\"") ||
		!strings.Contains(out, "\"name\": \"answer_present\"") {
		t.Fatalf("expected JSON evaluation report, got:\n%s", out)
	}
}

func TestRunAnswerProfilesTool(t *testing.T) {
	out, err := runAnswerProfilesTool()
	if err != nil {
		t.Fatalf("run answer profiles tool: %v", err)
	}
	if !strings.Contains(out, "\"profiles\"") ||
		!strings.Contains(out, "\"name\": \"review-change\"") ||
		!strings.Contains(out, "\"name\": \"plan-implementation\"") ||
		!strings.Contains(out, "\"template\"") {
		t.Fatalf("expected answer profiles JSON, got:\n%s", out)
	}
}

func TestRunAnswerTemplatesTool(t *testing.T) {
	out, err := runAnswerTemplatesTool(AnswerTemplatesArgs{IncludePrompts: true})
	if err != nil {
		t.Fatalf("run answer templates tool: %v", err)
	}
	if !strings.Contains(out, "\"templates\"") ||
		!strings.Contains(out, "\"name\": \"general\"") ||
		!strings.Contains(out, "\"name\": \"plan\"") ||
		!strings.Contains(out, "\"prompt\"") {
		t.Fatalf("expected answer templates JSON, got:\n%s", out)
	}
}

func TestFormatHybridHitsMarkdown(t *testing.T) {
	out := formatHybridHitsMarkdown([]store.SearchHit{{
		Target: store.TargetRef{Kind: store.TargetSymbol, Path: "a.go", Name: "Foo", Line: 3},
		Score:  0.9,
		Source: store.SearchSourceHybrid,
		Metadata: map[string]string{
			"sources":                        "text,vector",
			"hybrid_fusion":                  "weighted_normalized_sum",
			"hybrid_text_rank":               "2",
			"hybrid_text_contribution":       "0.0450",
			"hybrid_text_normalized_score":   "0.1000",
			"hybrid_vector_rank":             "1",
			"hybrid_vector_contribution":     "0.4500",
			"hybrid_vector_normalized_score": "1.0000",
		},
		Evidence: "func Foo() {}",
	}})
	if !strings.Contains(out, "## Hybrid Retrieval (1)") ||
		!strings.Contains(out, "`a.go:3`") ||
		!strings.Contains(out, "sources: text,vector") ||
		!strings.Contains(out, "func Foo() {}") ||
		!strings.Contains(out, "ranking: fusion=weighted_normalized_sum") ||
		!strings.Contains(out, "text rank=2 contribution=0.0450 normalized=0.1000") {
		t.Fatalf("unexpected hybrid markdown:\n%s", out)
	}
}

func TestRunHybridSearchToolRequiresSignal(t *testing.T) {
	eng, cleanup := newTestEngine(t)
	defer cleanup()

	_, err := runHybridSearchTool(context.Background(), eng, HybridSearchArgs{})
	if err == nil || !strings.Contains(err.Error(), "query, vector, or expand_from") {
		t.Fatalf("expected missing hybrid query error, got %v", err)
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
