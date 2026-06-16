// Code Context MCP Server
// Provides code-context capabilities as an MCP server for AI agents (Claude Desktop, Cursor, etc.)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/config"
	"github.com/sjzsdu/code-context/internal/engine"
	"github.com/sjzsdu/code-context/internal/search"
	"github.com/sjzsdu/code-context/internal/store"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	root           string
	db             string
	storeBackend   string
	helixURL       string
	helixAPIKey    string
	helixAPIKeyEnv string
	helixProjectID string
)

type GraphArgs struct {
	Focus string `json:"focus,omitempty"`
}

type GraphPathArgs struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type GraphNeighborsArgs struct {
	Target string `json:"target"`
	Limit  int    `json:"limit,omitempty"`
}

type GraphSubgraphArgs struct {
	Target string `json:"target"`
	Depth  int    `json:"depth,omitempty"`
}

type GraphTraverseArgs struct {
	Start     store.TargetRef       `json:"start,omitempty"`
	Target    string                `json:"target,omitempty"`
	EdgeKinds []store.GraphEdgeKind `json:"edge_kinds,omitempty"`
	Direction store.GraphDirection  `json:"direction,omitempty"`
	MaxDepth  int                   `json:"max_depth,omitempty"`
	Limit     int                   `json:"limit,omitempty"`
	Filter    store.SearchFilter    `json:"filter,omitempty"`
	// IncludePaths includes shortest traversal paths from the start target when supported.
	IncludePaths bool `json:"include_paths,omitempty"`
}

type SearchArgs struct {
	Query string `json:"query"`
}

type ContextArgs struct {
	Symbol string `json:"symbol"`
}

type SnapshotArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type ImpactArgs struct {
	Target string `json:"target"`
	Depth  int    `json:"depth,omitempty"`
}

type RoutesArgs struct {
	Query string `json:"query,omitempty"`
}

type DocsForArgs struct {
	Query string `json:"query"`
}

type GitStateToolArgs struct {
	State string `json:"state,omitempty"`
}

type GitImpactArgs struct {
	State string `json:"state,omitempty"`
	Depth int    `json:"depth,omitempty"`
}

type FreshnessArgs struct {
	Limit int `json:"limit,omitempty"`
}

func main() {
	flag.StringVar(&root, "root", ".", "codebase root directory")
	flag.StringVar(&db, "db", "", "database path (default: <root>/.code-context/index.db)")
	flag.StringVar(&storeBackend, "store-backend", "", "storage backend (sqlite|helix; default: sqlite)")
	flag.StringVar(&helixURL, "helix-url", "", "HelixDB endpoint URL for --store-backend=helix")
	flag.StringVar(&helixAPIKey, "helix-api-key", "", "HelixDB API key for --store-backend=helix")
	flag.StringVar(&helixAPIKeyEnv, "helix-api-key-env", "", "environment variable containing the HelixDB API key")
	flag.StringVar(&helixProjectID, "helix-project-id", "", "Helix project namespace for --store-backend=helix (default: absolute root)")
	flag.Parse()
	applyConfigDefaults()

	// Initialize the engine
	eng, err := engine.NewWithStoreOptions(root, mcpStoreOptions())
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

	// Fast startup reconciliation: only reindex changed files when an index already exists.
	log.Println("Reconciling codebase index...")
	stats, err := eng.IndexIncremental(context.Background(), false)
	if err != nil {
		log.Printf("Warning: auto-index failed: %v", err)
	} else {
		docInfo := ""
		if stats.TotalDocuments > 0 {
			docInfo = fmt.Sprintf(", %d docs", stats.TotalDocuments)
		}
		log.Printf("Auto-index completed: %d files, %d symbols, %d imports%s (%.1fs)",
			stats.IndexedFiles, stats.TotalSymbols, stats.TotalImports, docInfo, stats.Duration)
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
	} else if !visited["db"] && loaded.Config.Store.SQLite.DB != "" {
		db = loaded.Config.Store.SQLite.DB
	}
	if !visited["store-backend"] && loaded.Config.Store.Backend != "" {
		storeBackend = loaded.Config.Store.Backend
	}
	if !visited["helix-url"] && loaded.Config.Store.Helix.URL != "" {
		helixURL = loaded.Config.Store.Helix.URL
	}
	if !visited["helix-api-key"] && loaded.Config.Store.Helix.APIKey != "" {
		helixAPIKey = loaded.Config.Store.Helix.APIKey
	}
	if !visited["helix-api-key-env"] && loaded.Config.Store.Helix.APIKeyEnv != "" {
		helixAPIKeyEnv = loaded.Config.Store.Helix.APIKeyEnv
	}
	if !visited["helix-project-id"] && loaded.Config.Store.Helix.ProjectID != "" {
		helixProjectID = loaded.Config.Store.Helix.ProjectID
	}
}

func mcpStoreOptions() store.Options {
	return store.Options{
		Backend: store.Backend(storeBackend),
		SQLite:  store.SQLiteOptions{Path: db},
		Helix: store.HelixOptions{
			URL:       helixURL,
			APIKey:    helixAPIKey,
			APIKeyEnv: helixAPIKeyEnv,
			ProjectID: helixProjectID,
		},
	}
}

func registerAgentTools(srv *mcp.Server, eng *engine.Engine) {
	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_instructions", Description: "How agents should use code-context before broad grep/read exploration"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return textResult(`# code-context agent instructions

1. Start with code_context_status to verify freshness.
2. Use code_context_explore for a query before broad grep/read.
3. Use code_context_search for symbols, code_context_context for a symbol profile, and code_context_snapshot for focused LLM context.
4. Use code_context_callers and code_context_callees for lightweight call graph navigation.
5. If status or a result reports stale/pending files, read those files directly before editing.
6. Prefer the recommended next tool calls in each response.`), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_status", Description: "Show index freshness, pending files, provider capabilities, and service metadata"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			st, err := eng.Status(ctx)
			if err != nil {
				return nil, nil, err
			}
			out, err := marshalIndentedJSON(st)
			if err != nil {
				return nil, nil, err
			}
			return textResult(out), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_doctor", Description: "Check database schema, index freshness, and service health"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			report, err := eng.Doctor(ctx)
			if err != nil {
				return nil, nil, err
			}
			out, err := marshalIndentedJSON(report)
			if err != nil {
				return nil, nil, err
			}
			return textResult(out), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_freshness", Description: "Show indexed files/documents that differ from the filesystem"},
		func(ctx context.Context, req *mcp.CallToolRequest, args FreshnessArgs) (*mcp.CallToolResult, any, error) {
			limit := args.Limit
			if limit <= 0 {
				limit = 50
			}
			report, err := eng.Freshness(ctx, limit)
			if err != nil {
				return nil, nil, err
			}
			out, err := marshalIndentedJSON(report)
			if err != nil {
				return nil, nil, err
			}
			return textResult(out), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_search", Description: "Search symbols by name in the indexed codebase"},
		func(ctx context.Context, req *mcp.CallToolRequest, args SearchArgs) (*mcp.CallToolResult, any, error) {
			if args.Query == "" {
				return nil, nil, fmt.Errorf("missing required parameter: query")
			}
			results, err := eng.SearchSymbols(ctx, args.Query, nil, 20)
			if err != nil {
				return nil, nil, err
			}
			return textResult(withStaleWarning(ctx, eng, search.FormatSymbols(results)) + recommendedCalls("code_context_context", "code_context_explore")), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_explore", Description: "One-stop codebase exploration for an agent query: symbols, text matches, and recommended next calls"},
		func(ctx context.Context, req *mcp.CallToolRequest, args SearchArgs) (*mcp.CallToolResult, any, error) {
			if args.Query == "" {
				return nil, nil, fmt.Errorf("missing required parameter: query")
			}
			syms, _ := eng.SearchSymbolsHybrid(ctx, args.Query, nil, 10)
			texts, _ := eng.SearchText(ctx, args.Query, "", 8)
			out := "# Explore: " + args.Query + "\n\n## Symbols\n" + search.FormatSymbols(syms) + "\n## Text Matches\n"
			for _, m := range texts {
				out += fmt.Sprintf("- `%s:%d` %s\n", m.FilePath, m.Line, strings.TrimSpace(m.Content))
			}
			out += recommendedCalls("code_context_context", "code_context_snapshot", "code_context_graph_neighbors")
			return textResult(withStaleWarning(ctx, eng, out)), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_callers", Description: "Show functions/methods that call a symbol name"},
		func(ctx context.Context, req *mcp.CallToolRequest, args ContextArgs) (*mcp.CallToolResult, any, error) {
			calls, err := eng.Callers(ctx, args.Symbol)
			if err != nil {
				return nil, nil, err
			}
			return textResult(formatCallsMarkdown("Callers", calls)), nil, nil
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_callees", Description: "Show symbols called by a function/method"},
		func(ctx context.Context, req *mcp.CallToolRequest, args ContextArgs) (*mcp.CallToolResult, any, error) {
			calls, err := eng.Callees(ctx, args.Symbol)
			if err != nil {
				return nil, nil, err
			}
			return textResult(formatCallsMarkdown("Callees", calls)), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_context", Description: "Show symbol profile with definition, methods, and related symbols"},
		func(ctx context.Context, req *mcp.CallToolRequest, args ContextArgs) (*mcp.CallToolResult, any, error) {
			if args.Symbol == "" {
				return nil, nil, fmt.Errorf("missing required parameter: symbol")
			}
			c, err := eng.Context(ctx, args.Symbol)
			if err != nil {
				return nil, nil, err
			}
			out := fmt.Sprintf("# Symbol Context: %s\n\nDefinition: `%s` (%s) at `%s:%d`\n", args.Symbol, c.Definition.Name, c.Definition.Kind, c.Definition.FilePath, c.Definition.Line)
			if len(c.Methods) > 0 {
				out += "\n## Methods\n"
				for _, m := range c.Methods {
					out += fmt.Sprintf("- `%s` at `%s:%d`\n", m.Name, m.FilePath, m.Line)
				}
			}
			out += recommendedCalls("code_context_callers", "code_context_callees", "code_context_snapshot")
			return textResult(withStaleWarning(ctx, eng, out)), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_snapshot", Description: "Generate focused LLM context package for a query"},
		func(ctx context.Context, req *mcp.CallToolRequest, args SnapshotArgs) (*mcp.CallToolResult, any, error) {
			if args.Query == "" {
				return nil, nil, fmt.Errorf("missing required parameter: query")
			}
			limit := 5
			if args.Limit > 0 {
				limit = args.Limit
			}
			s, err := eng.Snapshot(ctx, args.Query, limit)
			if err != nil {
				return nil, nil, err
			}
			out := fmt.Sprintf("# Snapshot: %s\n\n%s\n", s.Query, s.Summary)
			for _, f := range s.Files {
				out += fmt.Sprintf("\n## `%s`\nLanguage: %s\n", f.Path, f.Language)
			}
			return textResult(withStaleWarning(ctx, eng, out+recommendedCalls("code_context_graph_neighbors", "code_context_callers"))), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_graph_neighbors", Description: "Show adjacent graph context for a file or symbol"},
		func(ctx context.Context, req *mcp.CallToolRequest, args GraphNeighborsArgs) (*mcp.CallToolResult, any, error) {
			out, err := runGraphNeighborsTool(ctx, eng, args)
			if err != nil {
				return nil, nil, err
			}
			return textResult(withStaleWarning(ctx, eng, out+recommendedCalls("code_context_explore", "code_context_snapshot"))), nil, nil
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_graph_traverse", Description: "Run provider-backed graph traversal with a GraphTraversalQuery"},
		func(ctx context.Context, req *mcp.CallToolRequest, args GraphTraverseArgs) (*mcp.CallToolResult, any, error) {
			out, err := runGraphTraverseTool(ctx, eng, args)
			if err != nil {
				return nil, nil, err
			}
			return textResult(withStaleWarning(ctx, eng, out+recommendedCalls("code_context_graph_neighbors", "code_context_docs_for"))), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_routes", Description: "List framework routes and their handlers discovered in indexed code"},
		func(ctx context.Context, req *mcp.CallToolRequest, args RoutesArgs) (*mcp.CallToolResult, any, error) {
			routes, err := eng.Routes(ctx, args.Query)
			if err != nil {
				return nil, nil, err
			}
			return textResult(withStaleWarning(ctx, eng, formatRoutesMarkdown(routes)+recommendedCalls("code_context_context", "code_context_callers", "code_context_snapshot"))), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_route_context", Description: "Analyze route-level impact using handlers, calls, docs, tests, and risk"},
		func(ctx context.Context, req *mcp.CallToolRequest, args RoutesArgs) (*mcp.CallToolResult, any, error) {
			if args.Query == "" {
				return nil, nil, fmt.Errorf("missing required parameter: query")
			}
			rc, err := eng.RouteContext(ctx, args.Query)
			if err != nil {
				return nil, nil, err
			}
			return textResult(withStaleWarning(ctx, eng, formatRouteContextMarkdown(rc)+recommendedCalls("code_context_symbol_impact", "code_context_docs_for", "code_context_test_impact"))), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_impact", Description: "Unified file or symbol impact analysis using imports, calls, routes, docs, tests, and risk"},
		func(ctx context.Context, req *mcp.CallToolRequest, args ImpactArgs) (*mcp.CallToolResult, any, error) {
			impact, err := runImpactTool(ctx, eng, args)
			if err != nil {
				return nil, nil, err
			}
			return textResult(withStaleWarning(ctx, eng, formatImpactMarkdown(impact)+recommendedCalls("code_context_graph_neighbors", "code_context_docs_for", "code_context_test_impact"))), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_impact_git", Description: "Unified impact analysis for files and symbols changed in local git state"},
		func(ctx context.Context, req *mcp.CallToolRequest, args GitImpactArgs) (*mcp.CallToolResult, any, error) {
			impact, err := runImpactGitTool(ctx, eng, args)
			if err != nil {
				return nil, nil, err
			}
			return textResult(withStaleWarning(ctx, eng, formatGitImpactMarkdown(impact)+recommendedCalls("code_context_impact", "code_context_test_impact", "code_context_review_context"))), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_docs_for", Description: "Show documents that reference a file, symbol, module, or text query"},
		func(ctx context.Context, req *mcp.CallToolRequest, args DocsForArgs) (*mcp.CallToolResult, any, error) {
			refs, err := eng.DocsFor(ctx, args.Query)
			if err != nil {
				return nil, nil, err
			}
			return textResult(formatDocsForMarkdown(refs) + recommendedCalls("code_context_explore", "code_context_snapshot")), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_doc_drift", Description: "Find stale document references to missing files, symbols, modules, or routes"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			report, err := eng.DocDrift(ctx)
			if err != nil {
				return nil, nil, err
			}
			return textResult(formatDocDriftMarkdown(report)), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_doc_coverage", Description: "Find indexed routes and public symbols that are not referenced by documentation"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			report, err := eng.DocCoverage(ctx)
			if err != nil {
				return nil, nil, err
			}
			return textResult(formatDocCoverageMarkdown(report)), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_review_context", Description: "Generate git-aware review context with changed symbols, routes, docs, tests, and risk"},
		func(ctx context.Context, req *mcp.CallToolRequest, args GitStateToolArgs) (*mcp.CallToolResult, any, error) {
			state, err := engine.ParseGitState(args.State)
			if err != nil {
				return nil, nil, err
			}
			r, err := eng.ReviewContext(ctx, state)
			if err != nil {
				return nil, nil, err
			}
			return textResult(formatReviewContextMarkdown(r)), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_test_impact", Description: "Recommend tests for git changed files and symbols"},
		func(ctx context.Context, req *mcp.CallToolRequest, args GitStateToolArgs) (*mcp.CallToolResult, any, error) {
			state, err := engine.ParseGitState(args.State)
			if err != nil {
				return nil, nil, err
			}
			t, err := eng.TestImpact(ctx, state)
			if err != nil {
				return nil, nil, err
			}
			return textResult(formatTestImpactMarkdown(t)), nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "code_context_symbol_impact", Description: "Analyze symbol-level impact using callers, callees, routes, docs, and tests"},
		func(ctx context.Context, req *mcp.CallToolRequest, args ContextArgs) (*mcp.CallToolResult, any, error) {
			impact, err := eng.SymbolImpact(ctx, args.Symbol)
			if err != nil {
				return nil, nil, err
			}
			return textResult(formatSymbolImpactMarkdown(impact)), nil, nil
		})
}

func registerTools(srv *mcp.Server, eng *engine.Engine) {
	registerAgentTools(srv, eng)
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

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "graph",
		Description: "Export repository graph JSON, optionally focused on a file or symbol",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GraphArgs) (*mcp.CallToolResult, any, error) {
		output, err := runGraphTool(ctx, eng, args)
		if err != nil {
			return nil, nil, fmt.Errorf("graph failed: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: output}}}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "graph_path",
		Description: "Find a file-level path through the graph between two files or symbols",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GraphPathArgs) (*mcp.CallToolResult, any, error) {
		output, err := runGraphPathTool(ctx, eng, args)
		if err != nil {
			return nil, nil, fmt.Errorf("graph_path failed: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: output}}}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "graph_neighbors",
		Description: "Show adjacent graph context for a file or symbol",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GraphNeighborsArgs) (*mcp.CallToolResult, any, error) {
		output, err := runGraphNeighborsTool(ctx, eng, args)
		if err != nil {
			return nil, nil, fmt.Errorf("graph_neighbors failed: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: output}}}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "graph_subgraph",
		Description: "Export a local graph around a file or symbol",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GraphSubgraphArgs) (*mcp.CallToolResult, any, error) {
		output, err := runGraphSubgraphTool(ctx, eng, args)
		if err != nil {
			return nil, nil, fmt.Errorf("graph_subgraph failed: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: output}}}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "graph_traverse",
		Description: "Run provider-backed graph traversal with a GraphTraversalQuery",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GraphTraverseArgs) (*mcp.CallToolResult, any, error) {
		output, err := runGraphTraverseTool(ctx, eng, args)
		if err != nil {
			return nil, nil, fmt.Errorf("graph_traverse failed: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: output}}}, nil, nil
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
		Name:        "impact",
		Description: "Unified impact analysis for a file or symbol",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ImpactArgs) (*mcp.CallToolResult, any, error) {
		impact, err := runImpactTool(ctx, eng, args)
		if err != nil {
			return nil, nil, fmt.Errorf("impact failed: %w", err)
		}
		out, err := marshalIndentedJSON(impact)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "impact_git",
		Description: "Unified impact analysis for files and symbols changed in local git state",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GitImpactArgs) (*mcp.CallToolResult, any, error) {
		impact, err := runImpactGitTool(ctx, eng, args)
		if err != nil {
			return nil, nil, fmt.Errorf("impact_git failed: %w", err)
		}
		out, err := marshalIndentedJSON(impact)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil, nil
	})

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

func runGraphTool(ctx context.Context, eng *engine.Engine, args GraphArgs) (string, error) {
	result, err := eng.ExportGraph(ctx, args.Focus)
	if err != nil {
		return "", err
	}
	return marshalIndentedJSON(result)
}

func runGraphPathTool(ctx context.Context, eng *engine.Engine, args GraphPathArgs) (string, error) {
	if args.From == "" || args.To == "" {
		return "", fmt.Errorf("missing required parameters: from and to")
	}
	result, err := eng.GraphPath(ctx, args.From, args.To)
	if err != nil {
		return "", err
	}
	return marshalIndentedJSON(result)
}

func runGraphNeighborsTool(ctx context.Context, eng *engine.Engine, args GraphNeighborsArgs) (string, error) {
	if args.Target == "" {
		return "", fmt.Errorf("missing required parameter: target")
	}
	result, err := eng.GraphNeighbors(ctx, args.Target, args.Limit)
	if err != nil {
		return "", err
	}
	return marshalIndentedJSON(result)
}

func runGraphSubgraphTool(ctx context.Context, eng *engine.Engine, args GraphSubgraphArgs) (string, error) {
	if args.Target == "" {
		return "", fmt.Errorf("missing required parameter: target")
	}
	result, err := eng.GraphSubgraph(ctx, args.Target, args.Depth)
	if err != nil {
		return "", err
	}
	return marshalIndentedJSON(result)
}

func runGraphTraverseTool(ctx context.Context, eng *engine.Engine, args GraphTraverseArgs) (string, error) {
	if args.Target == "" && args.Start.Kind == "" && args.Start.Path == "" && args.Start.Name == "" && args.Start.Value == "" && args.Start.RoutePath == "" {
		return "", fmt.Errorf("missing required parameter: start or target")
	}
	result, err := eng.TraverseGraph(ctx, store.GraphTraversalQuery{
		Start:        args.Start,
		Target:       args.Target,
		EdgeKinds:    args.EdgeKinds,
		Direction:    args.Direction,
		MaxDepth:     args.MaxDepth,
		Limit:        args.Limit,
		Filter:       args.Filter,
		IncludePaths: args.IncludePaths,
	})
	if err != nil {
		return "", err
	}
	return marshalIndentedJSON(result)
}

func runImpactTool(ctx context.Context, eng *engine.Engine, args ImpactArgs) (*engine.ImpactResult, error) {
	if args.Target == "" {
		return nil, fmt.Errorf("missing required parameter: target")
	}
	return eng.Impact(ctx, args.Target, args.Depth)
}

func runImpactGitTool(ctx context.Context, eng *engine.Engine, args GitImpactArgs) (*engine.GitImpact, error) {
	state, err := engine.ParseGitState(args.State)
	if err != nil {
		return nil, err
	}
	return eng.ImpactGit(ctx, state, args.Depth)
}

func marshalIndentedJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func withStaleWarning(ctx context.Context, eng *engine.Engine, text string) string {
	pending, err := eng.PendingFiles(ctx, 5)
	if err != nil || len(pending) == 0 {
		return text
	}
	warn := "⚠️ Index may be stale for pending files:\n"
	for _, f := range pending {
		warn += "- `" + f + "`\n"
	}
	warn += "Read pending files directly before editing.\n\n"
	return warn + text
}

func recommendedCalls(names ...string) string {
	if len(names) == 0 {
		return ""
	}
	out := "\n## Recommended Next Tool Calls\n"
	for i, name := range names {
		out += fmt.Sprintf("%d. %s\n", i+1, name)
	}
	return out
}

func formatCallsMarkdown(title string, calls []api.CallEdge) string {
	out := "# " + title + "\n\n"
	for _, c := range calls {
		out += fmt.Sprintf("- `%s:%d` `%s` -> `%s` [%s]\n", c.FromFile, c.Line, c.FromSymbol, c.ToName, c.Confidence)
	}
	out += fmt.Sprintf("\n%d calls\n", len(calls))
	return out + recommendedCalls("code_context_explore", "code_context_snapshot")
}

func formatRoutesMarkdown(routes []api.Route) string {
	out := "# Routes\n\n"
	for _, r := range routes {
		method := r.Method
		if method == "" {
			method = "*"
		}
		out += fmt.Sprintf("- `%s %s` -> `%s` in `%s:%d` [%s, %s]\n", method, r.Path, r.Handler, r.FilePath, r.Line, r.Framework, r.Confidence)
	}
	out += fmt.Sprintf("\n%d routes\n", len(routes))
	return out
}

func formatRouteContextMarkdown(rc *engine.RouteContext) string {
	out := fmt.Sprintf("# Route Context: `%s`\n\n%s\n\nRisk: %s (%d)\n", rc.Query, rc.Summary, rc.Risk.Level, rc.Risk.Score)
	for _, reason := range rc.Risk.Reasons {
		out += "- " + reason + "\n"
	}
	out += fmt.Sprintf("\n## Routes (%d)\n", len(rc.Routes))
	for _, r := range rc.Routes {
		out += fmt.Sprintf("- `%s %s` -> `%s` in `%s:%d` [%s]\n", r.Method, r.Path, r.Handler, r.FilePath, r.Line, r.Framework)
	}
	out += fmt.Sprintf("\n## Handlers (%d)\n", len(rc.Handlers))
	for _, h := range rc.Handlers {
		out += fmt.Sprintf("- `%s:%d` `%s` (%s)\n", h.FilePath, h.Line, h.Name, h.Kind)
	}
	out += fmt.Sprintf("\n## Callers (%d)\n", len(rc.Callers))
	for _, c := range rc.Callers {
		out += fmt.Sprintf("- `%s:%d` `%s` -> `%s`\n", c.FromFile, c.Line, c.FromSymbol, c.ToName)
	}
	out += fmt.Sprintf("\n## Callees (%d)\n", len(rc.Callees))
	for _, c := range rc.Callees {
		out += fmt.Sprintf("- `%s:%d` `%s` -> `%s`\n", c.FromFile, c.Line, c.FromSymbol, c.ToName)
	}
	out += fmt.Sprintf("\n## Related Docs (%d)\n", len(rc.RelatedDocs))
	for _, d := range rc.RelatedDocs {
		out += fmt.Sprintf("- `%s:%d` %s:%s\n", d.DocumentPath, d.Line, d.TargetType, d.TargetValue)
	}
	out += fmt.Sprintf("\n## Recommended Tests (%d)\n", len(rc.RecommendedTests))
	for _, t := range rc.RecommendedTests {
		out += "- `" + t + "`\n"
	}
	return out
}

func formatDocsForMarkdown(refs *api.DocReference) string {
	out := fmt.Sprintf("# Docs for `%s`\n\n", refs.Query)
	for _, link := range refs.Links {
		section := ""
		if link.SectionTitle != "" {
			section = "#" + link.SectionSlug
		}
		out += fmt.Sprintf("- `%s:%d%s` %s:%s `%s` (%.1f)\n", link.DocumentPath, link.Line, section, link.TargetType, link.TargetValue, link.Evidence, link.Confidence)
	}
	out += fmt.Sprintf("\n%d document references\n", len(refs.Links))
	return out
}

func formatDocDriftMarkdown(report *api.DocDriftReport) string {
	out := "# Documentation Drift\n\n" + report.Summary + "\n"
	for _, item := range report.Broken {
		section := ""
		if item.SectionTitle != "" {
			section = "#" + item.SectionSlug
		}
		out += fmt.Sprintf("- `%s:%d%s` %s:%s - %s\n", item.DocumentPath, item.Line, section, item.TargetType, item.TargetValue, item.Reason)
	}
	return out
}

func formatDocCoverageMarkdown(report *api.DocCoverageReport) string {
	out := "# Documentation Coverage\n\n" + report.Summary + "\n"
	out += fmt.Sprintf("\n## Missing Routes (%d)\n", len(report.MissingRoutes))
	for _, route := range report.MissingRoutes {
		method := route.Method
		if method == "" {
			method = "*"
		}
		out += fmt.Sprintf("- `%s %s` -> `%s` in `%s:%d` [%s]\n", method, route.Path, route.Handler, route.FilePath, route.Line, route.Framework)
	}
	out += fmt.Sprintf("\n## Missing Public Symbols (%d)\n", len(report.MissingSymbols))
	for _, sym := range report.MissingSymbols {
		out += fmt.Sprintf("- `%s:%d` `%s` (%s)\n", sym.FilePath, sym.Line, sym.Name, sym.Kind)
	}
	return out
}

func formatReviewContextMarkdown(r *engine.ReviewContext) string {
	out := fmt.Sprintf("# Review Context (%s)\n\n%s\n\n## Risk\n%s (%d)\n", r.State, r.Summary, r.Risk.Level, r.Risk.Score)
	for _, reason := range r.Risk.Reasons {
		out += "- " + reason + "\n"
	}
	out += fmt.Sprintf("\n## Changed Files (%d)\n", len(r.ChangedFiles))
	for _, f := range r.ChangedFiles {
		out += "- `" + f + "`\n"
	}
	out += fmt.Sprintf("\n## Changed Symbols (%d)\n", len(r.ChangedSymbols))
	for _, s := range r.ChangedSymbols {
		out += fmt.Sprintf("- `%s:%d` %s (%s)\n", s.FilePath, s.Line, s.Name, s.Kind)
	}
	out += fmt.Sprintf("\n## Routes (%d)\n", len(r.Routes))
	for _, route := range r.Routes {
		out += fmt.Sprintf("- `%s %s` -> `%s` in `%s:%d`\n", route.Method, route.Path, route.Handler, route.FilePath, route.Line)
	}
	out += fmt.Sprintf("\n## Related Docs (%d)\n", len(r.RelatedDocs))
	for _, d := range r.RelatedDocs {
		out += fmt.Sprintf("- `%s:%d` %s:%s\n", d.DocumentPath, d.Line, d.TargetType, d.TargetValue)
	}
	out += fmt.Sprintf("\n## Recommended Tests (%d)\n", len(r.RecommendedTests))
	for _, t := range r.RecommendedTests {
		out += "- `" + t + "`\n"
	}
	out += formatTestCommandsMarkdown(r.TestCommands)
	out += "\n## Suggested Review Order\n"
	for i, f := range r.SuggestedReviewOrder {
		out += fmt.Sprintf("%d. `%s`\n", i+1, f)
	}
	return out
}

func formatTestImpactMarkdown(t *engine.TestImpact) string {
	out := "# Test Impact\n\n" + t.Summary + "\n\n## Changed Symbols\n"
	for _, s := range t.ChangedSymbols {
		out += fmt.Sprintf("- `%s:%d` %s (%s)\n", s.FilePath, s.Line, s.Name, s.Kind)
	}
	out += "\n## Recommended Tests\n"
	for _, test := range t.RecommendedTests {
		out += "- `" + test + "`\n"
	}
	out += formatTestCommandsMarkdown(t.TestCommands)
	return out
}

func formatTestCommandsMarkdown(commands []engine.TestCommand) string {
	if len(commands) == 0 {
		return ""
	}
	out := fmt.Sprintf("\n## Recommended Test Commands (%d)\n", len(commands))
	for _, cmd := range commands {
		if cmd.Reason != "" {
			out += fmt.Sprintf("- `%s` - %s\n", cmd.Command, cmd.Reason)
		} else {
			out += fmt.Sprintf("- `%s`\n", cmd.Command)
		}
	}
	return out
}

func formatImpactMarkdown(impact *engine.ImpactResult) string {
	out := fmt.Sprintf("# Impact: `%s` (%s)\n\n%s\n", impact.Target, impact.Kind, impact.Summary)
	if impact.FileImpact != nil {
		d := impact.FileImpact
		out += fmt.Sprintf("\n## File Impact: `%s`\n", d.File)
		out += fmt.Sprintf("\n### Direct Imports (%d)\n", len(d.DirectDeps))
		for _, dep := range d.DirectDeps {
			out += "- `" + dep + "`\n"
		}
		out += fmt.Sprintf("\n### All Dependencies (%d)\n", len(d.AllDeps))
		for _, dep := range d.AllDeps {
			out += "- `" + dep + "`\n"
		}
		out += fmt.Sprintf("\n### Dependents (%d)\n", len(d.Dependents))
		for _, dep := range d.Dependents {
			out += "- `" + dep + "`\n"
		}
		out += fmt.Sprintf("\n### Recommended Tests (%d)\n", len(d.Recommends))
		for _, test := range d.Recommends {
			out += "- `" + test + "`\n"
		}
	}
	if impact.SymbolImpact != nil {
		out += "\n" + formatSymbolImpactMarkdown(impact.SymbolImpact)
	}
	return out
}

func formatGitImpactMarkdown(impact *engine.GitImpact) string {
	out := fmt.Sprintf("# Git Impact (%s)\n\n%s\n", impact.State, impact.Summary)
	out += fmt.Sprintf("\n## Risk\n%s (%d)\n", impact.Risk.Level, impact.Risk.Score)
	for _, reason := range impact.Risk.Reasons {
		out += "- " + reason + "\n"
	}
	out += fmt.Sprintf("\n## Changed Files (%d)\n", len(impact.ChangedFiles))
	for _, f := range impact.ChangedFiles {
		out += "- `" + f + "`\n"
	}
	out += fmt.Sprintf("\n## Changed Symbols (%d)\n", len(impact.ChangedSymbols))
	for _, s := range impact.ChangedSymbols {
		out += fmt.Sprintf("- `%s:%d` %s (%s)\n", s.FilePath, s.Line, s.Name, s.Kind)
	}
	out += fmt.Sprintf("\n## File Impacts (%d)\n", len(impact.FileImpacts))
	for _, f := range impact.FileImpacts {
		out += fmt.Sprintf("- `%s`: %d deps, %d dependents, %d tests\n", f.File, len(f.AllDeps), len(f.Dependents), len(f.Recommends))
	}
	out += fmt.Sprintf("\n## Symbol Impacts (%d)\n", len(impact.SymbolImpacts))
	for _, s := range impact.SymbolImpacts {
		out += fmt.Sprintf("- `%s`: risk %s, %d callers, %d routes\n", s.Symbol.Name, s.Risk.Level, len(s.Callers), len(s.Routes))
	}
	out += fmt.Sprintf("\n## Recommended Tests (%d)\n", len(impact.RecommendedTests))
	for _, test := range impact.RecommendedTests {
		out += "- `" + test + "`\n"
	}
	out += formatTestCommandsMarkdown(impact.TestCommands)
	return out
}

func formatSymbolImpactMarkdown(impact *engine.SymbolImpact) string {
	out := fmt.Sprintf("# Symbol Impact: `%s`\n\n%s\n\nDefinition: `%s:%d` (%s)\nRisk: %s (%d)\n", impact.Symbol.Name, impact.Summary, impact.Symbol.FilePath, impact.Symbol.Line, impact.Symbol.Kind, impact.Risk.Level, impact.Risk.Score)
	for _, reason := range impact.Risk.Reasons {
		out += "- " + reason + "\n"
	}
	out += fmt.Sprintf("\n## Callers (%d)\n", len(impact.Callers))
	for _, c := range impact.Callers {
		out += fmt.Sprintf("- `%s:%d` `%s` -> `%s`\n", c.FromFile, c.Line, c.FromSymbol, c.ToName)
	}
	out += fmt.Sprintf("\n## Callees (%d)\n", len(impact.Callees))
	for _, c := range impact.Callees {
		out += fmt.Sprintf("- `%s:%d` `%s` -> `%s`\n", c.FromFile, c.Line, c.FromSymbol, c.ToName)
	}
	out += fmt.Sprintf("\n## Routes (%d)\n", len(impact.Routes))
	for _, r := range impact.Routes {
		out += fmt.Sprintf("- `%s %s` in `%s:%d`\n", r.Method, r.Path, r.FilePath, r.Line)
	}
	out += fmt.Sprintf("\n## Related Docs (%d)\n", len(impact.RelatedDocs))
	for _, d := range impact.RelatedDocs {
		out += fmt.Sprintf("- `%s:%d` %s:%s\n", d.DocumentPath, d.Line, d.TargetType, d.TargetValue)
	}
	out += fmt.Sprintf("\n## Recommended Tests (%d)\n", len(impact.RecommendedTests))
	for _, t := range impact.RecommendedTests {
		out += "- `" + t + "`\n"
	}
	return out
}
