// Code Context MCP Server
// Provides code-context capabilities as an MCP server for AI agents (Claude Desktop, Cursor, etc.)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/config"
	"github.com/sjzsdu/code-context/internal/engine"
	"github.com/sjzsdu/code-context/internal/search"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	root string
	db   string
)

func main() {
	flag.StringVar(&root, "root", ".", "codebase root directory")
	flag.StringVar(&db, "db", "", "database path (default: <root>/.code-context/index.db)")
	flag.Parse()
	applyConfigDefaults()

	// Initialize the engine
	eng, err := engine.New(root, db)
	if err != nil {
		log.Fatalf("Failed to initialize engine: %v", err)
	}
	defer eng.Close()

	// Create MCP server
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "code-context",
		Title:   "Code Context",
		Version: "0.1.0",
	}, nil)

	// Register all tools
	registerTools(srv, eng)

	// Auto-index on startup
	log.Println("Indexing codebase...")
	stats, err := eng.Index(context.Background(), false)
	if err != nil {
		log.Printf("Warning: auto-index failed: %v", err)
	} else {
		log.Printf("Auto-index completed: %d files, %d symbols, %d imports (%.1fs)",
			stats.IndexedFiles, stats.TotalSymbols, stats.TotalImports, stats.Duration)
	}

	// Run with stdio transport (for Claude Desktop, Cursor, etc.)
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func applyConfigDefaults() {
	loaded, err := config.Load(root)
	if err != nil {
		return
	}

	visited := map[string]bool{}
	flag.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})

	if !visited["root"] && loaded.Config.Root != "" {
		root = loaded.Config.Root
	}
	if !visited["db"] && loaded.Config.DB != "" {
		db = loaded.Config.DB
	}
}

func registerTools(srv *mcp.Server, eng *engine.Engine) {
	// Index tool
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "index",
		Description: "Index the codebase for search. Use before searching.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		stats, err := eng.Index(ctx, false)
		if err != nil {
			return nil, nil, fmt.Errorf("index failed: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf(
					"Indexed %d files, %d symbols, %d imports (%.1fs)",
					stats.IndexedFiles, stats.TotalSymbols, stats.TotalImports, stats.Duration,
				)},
			},
		}, nil, nil
	})

	// Search tool
	type SearchArgs struct {
		Query string `json:"query"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search",
		Description: "Search symbols by name in the indexed codebase",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args SearchArgs) (*mcp.CallToolResult, any, error) {
		if args.Query == "" {
			return nil, nil, fmt.Errorf("missing required parameter: query")
		}
		results, err := eng.SearchSymbols(ctx, args.Query, nil, 20)
		if err != nil {
			return nil, nil, fmt.Errorf("search failed: %w", err)
		}
		if len(results) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "No results found"}},
			}, nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: search.FormatSymbols(results)}},
		}, nil, nil
	})

	// Find definition tool
	type FindDefArgs struct {
		Name string `json:"name"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_def",
		Description: "Find where a symbol is defined",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args FindDefArgs) (*mcp.CallToolResult, any, error) {
		if args.Name == "" {
			return nil, nil, fmt.Errorf("missing required parameter: name")
		}
		results, err := eng.FindDef(ctx, args.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("find_def failed: %w", err)
		}
		if len(results) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "Definition not found"}},
			}, nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: search.FormatSymbols(results)}},
		}, nil, nil
	})

	// Find references tool
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_refs",
		Description: "Find all references to a symbol",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args FindDefArgs) (*mcp.CallToolResult, any, error) {
		if args.Name == "" {
			return nil, nil, fmt.Errorf("missing required parameter: name")
		}
		results, err := eng.FindRefs(ctx, args.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("find_refs failed: %w", err)
		}
		if len(results) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "No references found"}},
			}, nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: search.FormatSymbols(results)}},
		}, nil, nil
	})

	// Files tool
	type FilesArgs struct {
		Language string `json:"language,omitempty"`
	}
	type GitStateArgs struct {
		State string `json:"state,omitempty"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "files",
		Description: "List indexed files, optionally filtered by language (go,typescript,python,rust,java)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args FilesArgs) (*mcp.CallToolResult, any, error) {
		var lang *api.Language
		if args.Language != "" {
			v := api.Language(args.Language)
			lang = &v
		}
		files, err := eng.ListFiles(ctx, lang)
		if err != nil {
			return nil, nil, fmt.Errorf("files failed: %w", err)
		}
		output := ""
		for _, f := range files {
			output += fmt.Sprintf("  %-6s  %s\n", f.Language, f.Path)
		}
		output += fmt.Sprintf("\n%d files\n", len(files))
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "git_files",
		Description: "List files changed in local git state (unstaged, staged, all)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GitStateArgs) (*mcp.CallToolResult, any, error) {
		gitState, err := engine.ParseGitState(args.State)
		if err != nil {
			return nil, nil, err
		}
		files, err := eng.GitChangedFiles(ctx, gitState)
		if err != nil {
			return nil, nil, fmt.Errorf("git_files failed: %w", err)
		}
		output := ""
		for _, f := range files {
			output += fmt.Sprintf("  %s\n", f)
		}
		output += fmt.Sprintf("\n%d changed files (%s)\n", len(files), gitState)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	// Imports tool
	type ImportsArgs struct {
		File string `json:"file"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "imports",
		Description: "Show imports of a specific file",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ImportsArgs) (*mcp.CallToolResult, any, error) {
		if args.File == "" {
			return nil, nil, fmt.Errorf("missing required parameter: file")
		}
		results, err := eng.Imports(ctx, args.File)
		if err != nil {
			return nil, nil, fmt.Errorf("imports failed: %w", err)
		}
		output := ""
		for _, e := range results {
			output += fmt.Sprintf("  %s:%d  %s\n", e.FromFile, e.Line, e.ToSource)
		}
		output += fmt.Sprintf("\n%d imports\n", len(results))
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	// Importers tool
	type ImportersArgs struct {
		Source string `json:"source"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "importers",
		Description: "Find files that import a given source path",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ImportersArgs) (*mcp.CallToolResult, any, error) {
		if args.Source == "" {
			return nil, nil, fmt.Errorf("missing required parameter: source")
		}
		results, err := eng.Importers(ctx, args.Source)
		if err != nil {
			return nil, nil, fmt.Errorf("importers failed: %w", err)
		}
		output := ""
		for _, e := range results {
			output += fmt.Sprintf("  %s:%d\n", e.FromFile, e.Line)
		}
		output += fmt.Sprintf("\n%d importers\n", len(results))
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	// Stats tool
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "stats",
		Description: "Show index statistics",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		stats, err := eng.Stats(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("stats failed: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
				"Files: %d\nSymbols: %d\nImports: %d",
				stats.TotalFiles, stats.TotalSymbols, stats.TotalImports,
			)}},
		}, nil, nil
	})

	// Map tool
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "map",
		Description: "Show project architecture overview",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		m, err := eng.Map(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("map failed: %w", err)
		}
		output := ""
		printMap(m, 0, &output)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	// Explain tool
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "explain",
		Description: "Show file summary with symbols and dependencies",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ImportsArgs) (*mcp.CallToolResult, any, error) {
		if args.File == "" {
			return nil, nil, fmt.Errorf("missing required parameter: file")
		}
		s, err := eng.Explain(ctx, args.File)
		if err != nil {
			return nil, nil, fmt.Errorf("explain failed: %w", err)
		}
		output := fmt.Sprintf("File: %s\nLanguage: %s\n\nSymbols (%d):\n%s\n\nImports (%d):\n",
			s.Path, s.Language, len(s.Symbols), search.FormatSymbols(s.Symbols), len(s.Imports))
		for _, imp := range s.Imports {
			output += fmt.Sprintf("  %s (line %d)\n", imp.ToSource, imp.Line)
		}
		output += fmt.Sprintf("\nImporters (%d):\n", len(s.Importers))
		for _, imp := range s.Importers {
			output += fmt.Sprintf("  %s (line %d)\n", imp.FromFile, imp.Line)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	// Context tool
	type ContextArgs struct {
		Symbol string `json:"symbol"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "context",
		Description: "Show symbol profile with definition, methods and related symbols",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ContextArgs) (*mcp.CallToolResult, any, error) {
		if args.Symbol == "" {
			return nil, nil, fmt.Errorf("missing required parameter: symbol")
		}
		c, err := eng.Context(ctx, args.Symbol)
		if err != nil {
			return nil, nil, fmt.Errorf("context failed: %w", err)
		}
		d := c.Definition
		output := fmt.Sprintf("Definition: %s (%s) at %s:%d\n", d.Name, d.Kind, d.FilePath, d.Line)
		if d.Signature != "" {
			output += fmt.Sprintf("  Signature: %s\n", d.Signature)
		}
		if len(c.Methods) > 0 {
			output += fmt.Sprintf("\nMethods (%d):\n", len(c.Methods))
			for _, m := range c.Methods {
				output += fmt.Sprintf("  %s at %s:%d\n", m.Name, m.FilePath, m.Line)
			}
		}
		if len(c.Related) > 0 {
			output += fmt.Sprintf("\nRelated (%d):\n", len(c.Related))
			n := 10
			if len(c.Related) < 10 {
				n = len(c.Related)
			}
			output += search.FormatSymbols(c.Related[:n])
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	// Snapshot tool
	type SnapshotArgs struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}
	type SnapshotGitArgs struct {
		State string `json:"state,omitempty"`
		Limit int    `json:"limit,omitempty"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "snapshot",
		Description: "Generate LLM context package for a query",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args SnapshotArgs) (*mcp.CallToolResult, any, error) {
		if args.Query == "" {
			return nil, nil, fmt.Errorf("missing required parameter: query")
		}
		limit := 5
		if args.Limit > 0 {
			limit = args.Limit
		}
		s, err := eng.Snapshot(ctx, args.Query, limit)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot failed: %w", err)
		}
		output := fmt.Sprintf("Query: %s\nSummary: %s\n\n", s.Query, s.Summary)
		for _, f := range s.Files {
			output += fmt.Sprintf("--- %s ---\n", f.Path)
			output += fmt.Sprintf("Language: %s\n", f.Language)
			symLimit := 5
			if len(f.Symbols) < 5 {
				symLimit = len(f.Symbols)
			}
			for _, sym := range f.Symbols[:symLimit] {
				output += fmt.Sprintf("  %s (%s)\n", sym.Name, sym.Kind)
			}
			if len(f.Symbols) > 5 {
				output += fmt.Sprintf("  ... and %d more\n", len(f.Symbols)-5)
			}
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "snapshot_git",
		Description: "Generate context snapshot from git changed files",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args SnapshotGitArgs) (*mcp.CallToolResult, any, error) {
		gitState, err := engine.ParseGitState(args.State)
		if err != nil {
			return nil, nil, err
		}
		limit := 5
		if args.Limit > 0 {
			limit = args.Limit
		}
		s, err := eng.SnapshotGit(ctx, gitState, limit)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot_git failed: %w", err)
		}
		output := fmt.Sprintf("Query: %s\nSummary: %s\n\n", s.Query, s.Summary)
		for _, f := range s.Files {
			output += fmt.Sprintf("--- %s ---\n", f.Path)
			output += fmt.Sprintf("Language: %s\n", f.Language)
			symLimit := 5
			if len(f.Symbols) < 5 {
				symLimit = len(f.Symbols)
			}
			for _, sym := range f.Symbols[:symLimit] {
				output += fmt.Sprintf("  %s (%s)\n", sym.Name, sym.Kind)
			}
			if len(f.Symbols) > 5 {
				output += fmt.Sprintf("  ... and %d more\n", len(f.Symbols)-5)
			}
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	// Diff impact tool
	type DiffImpactArgs struct {
		File  string `json:"file"`
		Depth int    `json:"depth,omitempty"`
	}
	type DiffImpactGitArgs struct {
		State string `json:"state,omitempty"`
		Depth int    `json:"depth,omitempty"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "diff_impact",
		Description: "Analyze change impact for a file",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DiffImpactArgs) (*mcp.CallToolResult, any, error) {
		if args.File == "" {
			return nil, nil, fmt.Errorf("missing required parameter: file")
		}
		depth := 3
		if args.Depth > 0 {
			depth = args.Depth
		}
		d, err := eng.DiffImpact(ctx, args.File, depth)
		if err != nil {
			return nil, nil, fmt.Errorf("diff_impact failed: %w", err)
		}
		output := fmt.Sprintf("File: %s\n\nDirect imports (%d):\n", d.File, len(d.DirectDeps))
		for _, dep := range d.DirectDeps {
			output += fmt.Sprintf("  %s\n", dep)
		}
		output += fmt.Sprintf("\nAll dependencies (%d):\n", len(d.AllDeps))
		for _, dep := range d.AllDeps {
			output += fmt.Sprintf("  %s\n", dep)
		}
		output += fmt.Sprintf("\nDependents (%d):\n", len(d.Dependents))
		for _, dep := range d.Dependents {
			output += fmt.Sprintf("  %s\n", dep)
		}
		if len(d.Recommends) > 0 {
			output += "\nRecommended test files:\n"
			for _, r := range d.Recommends {
				output += fmt.Sprintf("  %s\n", r)
			}
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "diff_impact_git",
		Description: "Analyze impact for files changed in local git state",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DiffImpactGitArgs) (*mcp.CallToolResult, any, error) {
		gitState, err := engine.ParseGitState(args.State)
		if err != nil {
			return nil, nil, err
		}
		depth := 3
		if args.Depth > 0 {
			depth = args.Depth
		}
		impacts, err := eng.DiffImpactGit(ctx, gitState, depth)
		if err != nil {
			return nil, nil, fmt.Errorf("diff_impact_git failed: %w", err)
		}

		output := fmt.Sprintf("Analyzed %d changed files (%s)\n\n", len(impacts), gitState)
		for _, d := range impacts {
			output += fmt.Sprintf("File: %s\n", d.File)
			output += fmt.Sprintf("Direct imports (%d):\n", len(d.DirectDeps))
			for _, dep := range d.DirectDeps {
				output += fmt.Sprintf("  %s\n", dep)
			}
			output += fmt.Sprintf("All dependencies (%d):\n", len(d.AllDeps))
			for _, dep := range d.AllDeps {
				output += fmt.Sprintf("  %s\n", dep)
			}
			output += fmt.Sprintf("Dependents - files that import this (%d):\n", len(d.Dependents))
			for _, dep := range d.Dependents {
				output += fmt.Sprintf("  %s\n", dep)
			}
			if len(d.Recommends) > 0 {
				output += "Recommended test files to run:\n"
				for _, r := range d.Recommends {
					output += fmt.Sprintf("  %s\n", r)
				}
			}
			output += "\n"
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})

	// Trace tool
	type TraceArgs struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "trace",
		Description: "Trace call chain between two symbols",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args TraceArgs) (*mcp.CallToolResult, any, error) {
		if args.From == "" || args.To == "" {
			return nil, nil, fmt.Errorf("missing required parameters: from and to")
		}
		t, err := eng.Trace(ctx, args.From, args.To)
		if err != nil {
			return nil, nil, fmt.Errorf("trace failed: %w", err)
		}
		output := fmt.Sprintf("Trace: %s -> %s\nPath length: %d files\n\n", t.From, t.To, len(t.Files))
		for i, f := range t.Files {
			output += fmt.Sprintf("  %d. %s\n", i+1, f)
		}
		if len(t.Path) > 0 {
			output += "\nKey points:\n"
			for _, p := range t.Path {
				output += fmt.Sprintf("  %s\n", p)
			}
		}
		output += fmt.Sprintf("\n%s\n", t.Metadata)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	})
}

func printMap(m *engine.ModuleMap, indent int, output *string) {
	prefix := ""
	for i := 0; i < indent; i++ {
		prefix += "  "
	}
	if m.Path == "" {
		*output += fmt.Sprintf("%s[root]\n", prefix)
	} else {
		*output += fmt.Sprintf("%s%s/\n", prefix, m.Path)
	}
	if m.Files > 0 {
		*output += fmt.Sprintf("%s  files: %d, symbols: %d (func: %d, type: %d, method: %d)\n",
			prefix, m.Files, m.Symbols, m.Functions, m.Types, m.Methods)
	}
	for _, c := range m.Children {
		printMap(&c, indent+1, output)
	}
}
