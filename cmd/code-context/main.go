package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/config"
	"github.com/sjzsdu/code-context/internal/engine"
	"github.com/sjzsdu/code-context/internal/search"
	"github.com/sjzsdu/code-context/internal/server"
)

var (
	root   string
	dbPath string
)

type runtimeConfig struct {
	serverPort    int
	watchEnabled  bool
	watchInterval time.Duration
	watchDebounce time.Duration
}

func main() {
	cmd := &cobra.Command{
		Use:   "github.com/sjzsdu/code-context",
		Short: "A code memory system for intelligent codebase indexing and search",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadRuntimeConfig(root)
			if err != nil {
				return err
			}
			applyPersistentDefaults(cmd, cfg)
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&root, "root", "r", ".", "codebase root directory")
	cmd.PersistentFlags().StringVar(&dbPath, "db", "", "database path (default: <root>/.code-context/index.db)")

	cmd.AddCommand(
		newIndexCmd(),
		newSearchCmd(),
		newFindDefCmd(),
		newGitFilesCmd(),
		newGitDiffCmd(),
		newFilesCmd(),
		newImportsCmd(),
		newImportersCmd(),
		newStatsCmd(),
		newMapCmd(),
		newGraphCmd(),
		newExplainCmd(),
		newContextCmd(),
		newSnapshotCmd(),
		newSnapshotGitCmd(),
		newTraceCmd(),
		newDiffImpactCmd(),
		newDiffImpactGitCmd(),
		newWatchCmd(),
		newServeCmd(),
	)

	attachServeConfig(cmd)
	attachWatchConfig(cmd)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadRuntimeConfig(startDir string) (*runtimeConfig, error) {
	loaded, err := config.Load(startDir)
	if err != nil {
		if err == config.ErrNotFound {
			return &runtimeConfig{}, nil
		}
		return nil, err
	}

	return &runtimeConfig{
		serverPort:    loaded.Config.Server.Port,
		watchEnabled:  loaded.Config.Watch.Enabled,
		watchInterval: loaded.Config.Watch.Interval,
		watchDebounce: loaded.Config.Watch.Debounce,
	}, nil
}

func applyPersistentDefaults(cmd *cobra.Command, cfg *runtimeConfig) {
	if !cmd.Flags().Changed("root") {
		if loaded, err := config.Load(root); err == nil && loaded.Config.Root != "" {
			root = loaded.Config.Root
		}
	}
	if !cmd.Flags().Changed("db") {
		if loaded, err := config.Load(root); err == nil && loaded.Config.DB != "" {
			dbPath = loaded.Config.DB
		}
	}
	_ = cfg
}

func attachServeConfig(rootCmd *cobra.Command) {
	serveCmd, _, err := rootCmd.Find([]string{"serve"})
	if err != nil || serveCmd == nil {
		return
	}
	prev := serveCmd.PreRunE
	serveCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		if cmd.Flags().Changed("port") {
			return nil
		}
		loaded, err := config.Load(root)
		if err != nil {
			if err == config.ErrNotFound {
				return nil
			}
			return err
		}
		if loaded.Config.Server.Port > 0 {
			flag := cmd.Flags().Lookup("port")
			if flag != nil {
				_ = flag.Value.Set(fmt.Sprintf("%d", loaded.Config.Server.Port))
			}
		}
		return nil
	}
}

func attachWatchConfig(rootCmd *cobra.Command) {
	watchCmd, _, err := rootCmd.Find([]string{"watch"})
	if err != nil || watchCmd == nil {
		return
	}
	prev := watchCmd.PreRunE
	watchCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		loaded, err := config.Load(root)
		if err != nil {
			if err == config.ErrNotFound {
				return nil
			}
			return err
		}
		if !cmd.Flags().Changed("interval") && loaded.Config.Watch.Interval > 0 {
			if flag := cmd.Flags().Lookup("interval"); flag != nil {
				_ = flag.Value.Set(loaded.Config.Watch.Interval.String())
			}
		}
		if !cmd.Flags().Changed("debounce") && loaded.Config.Watch.Debounce > 0 {
			if flag := cmd.Flags().Lookup("debounce"); flag != nil {
				_ = flag.Value.Set(loaded.Config.Watch.Debounce.String())
			}
		}
		if !cmd.Flags().Changed("enabled") && loaded.Config.Watch.Enabled {
			if flag := cmd.Flags().Lookup("enabled"); flag != nil {
				_ = flag.Value.Set("true")
			}
		}
		return nil
	}
}

func newIndexCmd() *cobra.Command {
	var incremental bool
	var verbose bool
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index the codebase",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			var stats *api.IndexStats
			if incremental {
				stats, err = eng.IndexIncremental(context.Background(), verbose)
			} else {
				stats, err = eng.Index(context.Background(), verbose)
			}
			if err != nil {
				return err
			}

			fmt.Printf("\nDone: %d indexed, %d skipped, %d failed — %d symbols, %d imports (%.1fs)\n",
				stats.IndexedFiles, stats.SkippedFiles, stats.FailedFiles,
				stats.TotalSymbols, stats.TotalImports, stats.Duration)
			return nil
		},
	}
	cmd.Flags().BoolVar(&incremental, "incremental", false, "only reindex changed files")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print per-file indexing progress")
	return cmd
}

func newSearchCmd() *cobra.Command {
	var kind string
	var limit int
	var hybrid bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search symbols by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			var k *api.SymbolKind
			if kind != "" {
				v := api.SymbolKind(kind)
				k = &v
			}
			var results []api.Symbol
			if hybrid {
				results, err = eng.SearchSymbolsHybrid(context.Background(), args[0], k, limit)
			} else {
				results, err = eng.SearchSymbols(context.Background(), args[0], k, limit)
			}
			if err != nil {
				return err
			}
			fmt.Println(search.FormatSymbols(results))
			fmt.Printf("\n%d results\n", len(results))
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind (function,method,class,type,interface)")
	cmd.Flags().IntVar(&limit, "limit", 50, "max results")
	cmd.Flags().BoolVar(&hybrid, "hybrid", false, "use hybrid retrieval (FTS5 + semantic ranking)")
	return cmd
}

func newFindDefCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "find-def <name>",
		Short: "Find definition of a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			results, err := eng.FindDef(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Println(search.FormatSymbols(results))
			fmt.Printf("\n%d results\n", len(results))
			return nil
		},
	}
}

func newGitFilesCmd() *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:   "git-files",
		Short: "List files changed in local git state",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			gitState, err := engine.ParseGitState(state)
			if err != nil {
				return err
			}

			files, err := eng.GitChangedFiles(context.Background(), gitState)
			if err != nil {
				return err
			}

			for _, f := range files {
				fmt.Printf("  %s\n", f)
			}
			fmt.Printf("\n%d changed files (%s)\n", len(files), gitState)
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "unstaged", "git change state: unstaged, staged, or all")
	return cmd
}

func newGitDiffCmd() *cobra.Command {
	var state string
	var contextLines int
	cmd := &cobra.Command{
		Use:   "git-diff",
		Short: "Show git diff hunks with line-level changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			gitState, err := engine.ParseGitState(state)
			if err != nil {
				return err
			}

			diffs, err := eng.GitDiff(context.Background(), gitState, contextLines)
			if err != nil {
				return err
			}

			for _, d := range diffs {
				fmt.Printf("File: %s\n", d.Path)
				for _, h := range d.Hunks {
					fmt.Printf("  @@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
					if h.Content != "" {
						for _, line := range strings.Split(h.Content, "\n") {
							fmt.Printf("    %s\n", line)
						}
					}
				}
				if len(d.Snippets) > 0 {
					fmt.Printf("  snippets (%d):\n", len(d.Snippets))
					for i, snippet := range d.Snippets {
						fmt.Printf("    [%d]\n", i+1)
						for _, line := range strings.Split(snippet, "\n") {
							fmt.Printf("      %s\n", line)
						}
					}
				}
				fmt.Println()
			}

			totalHunks := 0
			for _, d := range diffs {
				totalHunks += len(d.Hunks)
			}
			fmt.Printf("%d changed files, %d hunks (%s, context=%d)\n", len(diffs), totalHunks, gitState, contextLines)
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "unstaged", "git change state: unstaged, staged, or all")
	cmd.Flags().IntVar(&contextLines, "context", 3, "context lines around changed lines")
	return cmd
}

func newFilesCmd() *cobra.Command {
	var lang string
	cmd := &cobra.Command{
		Use:   "files",
		Short: "List indexed files",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			var l *api.Language
			if lang != "" {
				v := api.Language(lang)
				l = &v
			}
			files, err := eng.ListFiles(context.Background(), l)
			if err != nil {
				return err
			}
			for _, f := range files {
				fmt.Printf("  %-6s  %s\n", f.Language, f.Path)
			}
			fmt.Printf("\n%d files\n", len(files))
			return nil
		},
	}
	cmd.Flags().StringVar(&lang, "lang", "", "filter by language (go,typescript,python,rust,java)")
	return cmd
}

func newImportsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "imports <file>",
		Short: "Show imports of a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			results, err := eng.Imports(context.Background(), args[0])
			if err != nil {
				return err
			}
			for _, e := range results {
				fmt.Printf("  %s:%d  %s\n", e.FromFile, e.Line, e.ToSource)
			}
			fmt.Printf("\n%d imports\n", len(results))
			return nil
		},
	}
}

func newImportersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "importers <source>",
		Short: "Show files that import a given source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			results, err := eng.Importers(context.Background(), args[0])
			if err != nil {
				return err
			}
			for _, e := range results {
				fmt.Printf("  %s:%d\n", e.FromFile, e.Line)
			}
			fmt.Printf("\n%d importers\n", len(results))
			return nil
		},
	}
}

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show index statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			stats, err := eng.Stats(context.Background())
			if err != nil {
				return err
			}
			fmt.Printf("Files:   %d\n", stats.TotalFiles)
			fmt.Printf("Symbols: %d\n", stats.TotalSymbols)
			fmt.Printf("Imports: %d\n", stats.TotalImports)
			return nil
		},
	}
}

func newMapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "map",
		Short: "Show project architecture overview",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			m, err := eng.Map(context.Background())
			if err != nil {
				return err
			}
			printMap(m, 0)
			if m.Analysis != nil {
				printGraphAnalysis(m.Analysis)
			}
			return nil
		},
	}
	return cmd
}

func newGraphCmd() *cobra.Command {
	var focus string
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Export repository graph as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			g, err := eng.ExportGraph(context.Background(), focus)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(g)
		},
	}
	cmd.Flags().StringVar(&focus, "focus", "", "limit graph export to a file path or symbol name")
	cmd.AddCommand(newGraphHTMLCmd(), newGraphPathCmd(), newGraphNeighborsCmd(), newGraphSubgraphCmd())
	return cmd
}

func newGraphHTMLCmd() *cobra.Command {
	var focus string
	cmd := &cobra.Command{
		Use:   "html",
		Short: "Render repository graph as an interactive HTML page",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			g, err := eng.ExportGraph(context.Background(), focus)
			if err != nil {
				return err
			}
			return renderGraphHTML(os.Stdout, g)
		},
	}
	cmd.Flags().StringVar(&focus, "focus", "", "limit graph HTML to a file path or symbol name")
	return cmd
}

func newGraphPathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path <from> <to>",
		Short: "Find a file-level path through the graph",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			result, err := eng.GraphPath(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}

			fmt.Printf("Graph path: %s -> %s\n", result.From, result.To)
			fmt.Printf("Resolved: %s -> %s\n", result.FromFile, result.ToFile)
			if result.Resolution != "" {
				fmt.Printf("Details: %s\n", result.Resolution)
			}
			fmt.Printf("Summary: %s\n", result.Summary)
			if len(result.Files) > 0 {
				fmt.Printf("\nFiles (%d):\n", len(result.Files))
				for i, file := range result.Files {
					fmt.Printf("  %d. %s\n", i+1, file)
				}
			}
			return nil
		},
	}
	return cmd
}

func newGraphNeighborsCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "neighbors <target>",
		Short: "Show adjacent graph context for a file or symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			result, err := eng.GraphNeighbors(context.Background(), args[0], limit)
			if err != nil {
				return err
			}

			fmt.Printf("Graph neighbors: %s\n", result.Target)
			fmt.Printf("Resolved file: %s\n", result.ResolvedFile)
			if result.Resolution != "" {
				fmt.Printf("Details: %s\n", result.Resolution)
			}
			fmt.Printf("Summary: %s\n", result.Summary)
			if len(result.Symbols) > 0 {
				fmt.Printf("\nSymbols (%d):\n", len(result.Symbols))
				for _, sym := range result.Symbols {
					fmt.Printf("  - %s\n", sym)
				}
			}
			if len(result.Imports) > 0 {
				fmt.Printf("\nImports (%d):\n", len(result.Imports))
				for _, imp := range result.Imports {
					fmt.Printf("  - %s\n", imp)
				}
			}
			if len(result.RelatedFiles) > 0 {
				fmt.Printf("\nRelated files (%d):\n", len(result.RelatedFiles))
				for _, file := range result.RelatedFiles {
					fmt.Printf("  - %s\n", file)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 5, "max symbols, imports, and related files to show")
	return cmd
}

func newGraphSubgraphCmd() *cobra.Command {
	var depth int
	cmd := &cobra.Command{
		Use:   "subgraph <target>",
		Short: "Export a local graph around a file or symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			result, err := eng.GraphSubgraph(context.Background(), args[0], depth)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 1, "graph neighborhood depth to include")
	return cmd
}

func newExplainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <file>",
		Short: "Show file summary with symbols and dependencies",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			s, err := eng.Explain(context.Background(), args[0])
			if err != nil {
				return err
			}

			fmt.Printf("File: %s\n", s.Path)
			fmt.Printf("Language: %s\n", s.Language)
			fmt.Printf("\nSymbols (%d):\n", len(s.Symbols))
			fmt.Println(search.FormatSymbols(s.Symbols))
			fmt.Printf("\nImports (%d):\n", len(s.Imports))
			for _, imp := range s.Imports {
				fmt.Printf("  %s (line %d)\n", imp.ToSource, imp.Line)
			}
			fmt.Printf("\nImporters (%d):\n", len(s.Importers))
			for _, imp := range s.Importers {
				fmt.Printf("  %s (line %d)\n", imp.FromFile, imp.Line)
			}
			return nil
		},
	}
	return cmd
}

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context <symbol>",
		Short: "Show symbol profile with definition and related symbols",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			c, err := eng.Context(context.Background(), args[0])
			if err != nil {
				return err
			}

			d := c.Definition
			fmt.Printf("Definition: %s (%s) at %s:%d\n", d.Name, d.Kind, d.FilePath, d.Line)
			if d.Signature != "" {
				fmt.Printf("  Signature: %s\n", d.Signature)
			}
			if len(c.Methods) > 0 {
				fmt.Printf("\nMethods (%d):\n", len(c.Methods))
				for _, m := range c.Methods {
					fmt.Printf("  %s at %s:%d\n", m.Name, m.FilePath, m.Line)
				}
			}
			if len(c.Related) > 0 {
				fmt.Printf("\nRelated (%d):\n", len(c.Related))
				n := min(len(c.Related), 10)
				fmt.Println(search.FormatSymbols(c.Related[:n]))
			}
			if c.GraphSummary != "" {
				fmt.Printf("\nGraph: %s\n", c.GraphSummary)
			}
			if len(c.RelatedFiles) > 0 {
				fmt.Printf("Related files: %s\n", strings.Join(c.RelatedFiles, ", "))
			}
			if len(c.RecommendedFiles) > 0 {
				fmt.Printf("Recommended next files: %s\n", strings.Join(c.RecommendedFiles, ", "))
			}
			if c.Analysis != nil {
				printGraphAnalysis(c.Analysis)
			}
			return nil
		},
	}
	return cmd
}

func newSnapshotCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "snapshot <query>",
		Short: "Generate LLM context package for a query",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			s, err := eng.Snapshot(context.Background(), args[0], limit)
			if err != nil {
				return err
			}

			fmt.Println("=== Code Snapshot ===")
			fmt.Printf("Query: %s\n", s.Query)
			fmt.Printf("Summary: %s\n", s.Summary)
			if len(s.RecommendedFiles) > 0 {
				fmt.Printf("Recommended next files: %s\n", strings.Join(s.RecommendedFiles, ", "))
			}
			fmt.Println()

			for _, f := range s.Files {
				fmt.Printf("--- %s ---\n", f.Path)
				fmt.Printf("Language: %s\n", f.Language)
				if f.GraphSummary != "" {
					fmt.Printf("Graph: %s\n", f.GraphSummary)
				}
				if len(f.RecommendedFiles) > 0 {
					fmt.Printf("Recommended: %s\n", strings.Join(f.RecommendedFiles, ", "))
				}
				fmt.Printf("Symbols (%d):\n", len(f.Symbols))
				symLimit := min(len(f.Symbols), 5)
				for _, sym := range f.Symbols[:symLimit] {
					fmt.Printf("  %s (%s)\n", sym.Name, sym.Kind)
				}
				if len(f.Symbols) > 5 {
					fmt.Printf("  ... and %d more\n", len(f.Symbols)-5)
				}
			}
			if s.Analysis != nil {
				fmt.Println()
				printGraphAnalysis(s.Analysis)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 5, "max files")
	return cmd
}

func newSnapshotGitCmd() *cobra.Command {
	var limit int
	var state string
	cmd := &cobra.Command{
		Use:   "snapshot-git",
		Short: "Generate context snapshot from git changed files",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			gitState, err := engine.ParseGitState(state)
			if err != nil {
				return err
			}

			s, err := eng.SnapshotGit(context.Background(), gitState, limit)
			if err != nil {
				return err
			}

			fmt.Println("=== Code Snapshot (Git) ===")
			fmt.Printf("Query: %s\n", s.Query)
			fmt.Printf("Summary: %s\n", s.Summary)
			if len(s.RecommendedFiles) > 0 {
				fmt.Printf("Recommended next files: %s\n", strings.Join(s.RecommendedFiles, ", "))
			}
			fmt.Println()

			for _, f := range s.Files {
				fmt.Printf("--- %s ---\n", f.Path)
				fmt.Printf("Language: %s\n", f.Language)
				if f.GraphSummary != "" {
					fmt.Printf("Graph: %s\n", f.GraphSummary)
				}
				if len(f.RecommendedFiles) > 0 {
					fmt.Printf("Recommended: %s\n", strings.Join(f.RecommendedFiles, ", "))
				}
				fmt.Printf("Symbols (%d):\n", len(f.Symbols))
				symLimit := min(len(f.Symbols), 5)
				for _, sym := range f.Symbols[:symLimit] {
					fmt.Printf("  %s (%s)\n", sym.Name, sym.Kind)
				}
				if len(f.Symbols) > 5 {
					fmt.Printf("  ... and %d more\n", len(f.Symbols)-5)
				}
			}
			if s.Analysis != nil {
				fmt.Println()
				printGraphAnalysis(s.Analysis)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "unstaged", "git change state: unstaged, staged, or all")
	cmd.Flags().IntVar(&limit, "limit", 5, "max files")
	return cmd
}

func newTraceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace <from> <to>",
		Short: "Trace call chain between two symbols",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			t, err := eng.Trace(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}

			fmt.Printf("Trace: %s -> %s\n", t.From, t.To)
			fmt.Printf("Path length: %d files\n\n", len(t.Files))
			for i, f := range t.Files {
				fmt.Printf("  %d. %s\n", i+1, f)
			}
			if len(t.Path) > 0 {
				fmt.Printf("\nKey points:\n")
				for _, p := range t.Path {
					fmt.Printf("  %s\n", p)
				}
			}
			fmt.Printf("\n%s\n", t.Metadata)
			return nil
		},
	}
	return cmd
}

func newDiffImpactCmd() *cobra.Command {
	var depth int
	cmd := &cobra.Command{
		Use:   "diff-impact <file>",
		Short: "Analyze change impact for a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			d, err := eng.DiffImpact(context.Background(), args[0], depth)
			if err != nil {
				return err
			}

			fmt.Printf("File: %s\n\n", d.File)
			fmt.Printf("Direct imports (%d):\n", len(d.DirectDeps))
			for _, dep := range d.DirectDeps {
				fmt.Printf("  %s\n", dep)
			}
			fmt.Printf("\nAll dependencies (%d):\n", len(d.AllDeps))
			for _, dep := range d.AllDeps {
				fmt.Printf("  %s\n", dep)
			}
			fmt.Printf("\nDependents - files that import this (%d):\n", len(d.Dependents))
			for _, dep := range d.Dependents {
				fmt.Printf("  %s\n", dep)
			}
			if len(d.Recommends) > 0 {
				fmt.Printf("\nRecommended test files to run:\n")
				for _, r := range d.Recommends {
					fmt.Printf("  %s\n", r)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 3, "dependency depth")
	return cmd
}

func newDiffImpactGitCmd() *cobra.Command {
	var depth int
	var state string
	cmd := &cobra.Command{
		Use:   "diff-impact-git",
		Short: "Analyze impact for files changed in local git state",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			gitState, err := engine.ParseGitState(state)
			if err != nil {
				return err
			}

			impacts, err := eng.DiffImpactGit(context.Background(), gitState, depth)
			if err != nil {
				return err
			}

			fmt.Printf("Analyzed %d changed files (%s)\n\n", len(impacts), gitState)
			for _, d := range impacts {
				fmt.Printf("File: %s\n", d.File)
				fmt.Printf("Direct imports (%d):\n", len(d.DirectDeps))
				for _, dep := range d.DirectDeps {
					fmt.Printf("  %s\n", dep)
				}
				fmt.Printf("All dependencies (%d):\n", len(d.AllDeps))
				for _, dep := range d.AllDeps {
					fmt.Printf("  %s\n", dep)
				}
				fmt.Printf("Dependents - files that import this (%d):\n", len(d.Dependents))
				for _, dep := range d.Dependents {
					fmt.Printf("  %s\n", dep)
				}
				if len(d.Recommends) > 0 {
					fmt.Printf("Recommended test files to run:\n")
					for _, r := range d.Recommends {
						fmt.Printf("  %s\n", r)
					}
				}
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "unstaged", "git change state: unstaged, staged, or all")
	cmd.Flags().IntVar(&depth, "depth", 3, "dependency depth")
	return cmd
}

func printMap(m *engine.ModuleMap, indent int) {
	prefix := strings.Repeat("  ", indent)
	if m.Path == "" {
		fmt.Printf("%s[root]\n", prefix)
	} else {
		fmt.Printf("%s%s/\n", prefix, m.Path)
	}
	if m.Files > 0 {
		fmt.Printf("%s  files: %d, symbols: %d (func: %d, type: %d, method: %d)\n",
			prefix, m.Files, m.Symbols, m.Functions, m.Types, m.Methods)
	}
	for _, c := range m.Children {
		printMap(&c, indent+1)
	}
}

func printGraphAnalysis(analysis *api.GraphAnalysis) {
	if analysis == nil {
		return
	}
	fmt.Println("\nGraph analysis:")
	if len(analysis.TopImports) > 0 {
		fmt.Println("  Top imports:")
		for _, item := range analysis.TopImports {
			fmt.Printf("    - %s (%d)\n", item.Name, item.Count)
		}
	}
	if len(analysis.MostConnectedFiles) > 0 {
		fmt.Println("  Most connected files:")
		for _, item := range analysis.MostConnectedFiles {
			fmt.Printf("    - %s (%d)\n", item.Name, item.Count)
		}
	}
	if len(analysis.BridgeFiles) > 0 {
		fmt.Println("  Bridge files:")
		for _, item := range analysis.BridgeFiles {
			fmt.Printf("    - %s (%d)\n", item.Name, item.Count)
		}
	}
	if len(analysis.HotspotFiles) > 0 {
		fmt.Println("  Hotspot files:")
		for _, item := range analysis.HotspotFiles {
			fmt.Printf("    - %s (%d)\n", item.Name, item.Count)
		}
	}
	if len(analysis.RelationHighlights) > 0 {
		fmt.Println("  Relation highlights:")
		for _, item := range analysis.RelationHighlights {
			fmt.Printf("    - %s\n", item)
		}
	}
	if len(analysis.ReadingPaths) > 0 {
		fmt.Println("  Reading paths:")
		for _, item := range analysis.ReadingPaths {
			fmt.Printf("    - %s: %s\n", item.Entry, strings.Join(item.Path, " -> "))
			if item.Reason != "" {
				fmt.Printf("      %s\n", item.Reason)
			}
		}
	}
	if len(analysis.RecommendedFiles) > 0 {
		fmt.Printf("  Recommended files: %s\n", strings.Join(analysis.RecommendedFiles, ", "))
	}
}

func newWatchCmd() *cobra.Command {
	var interval time.Duration
	var debounce time.Duration
	var verbose bool
	var enabled bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Continuously refresh the index with incremental reindexing",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !enabled {
				return fmt.Errorf("watch mode is disabled; pass --enabled or set watch.enabled=true in config")
			}
			if interval <= 0 {
				return fmt.Errorf("watch interval must be greater than zero")
			}
			if debounce < 0 {
				return fmt.Errorf("watch debounce must be zero or greater")
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			fmt.Printf("Starting watch mode for %s\n", root)
			stats, err := eng.IndexIncremental(ctx, verbose)
			if err != nil {
				return err
			}
			fmt.Printf("Initial sync: %d indexed, %d skipped, %d failed — %d symbols, %d imports (%.1fs)\n",
				stats.IndexedFiles, stats.SkippedFiles, stats.FailedFiles,
				stats.TotalSymbols, stats.TotalImports, stats.Duration)
			fmt.Printf("Watching every %s with %s debounce. Press Ctrl+C to stop.\n", interval, debounce)

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			var nextAllowed time.Time
			for {
				select {
				case <-ctx.Done():
					fmt.Println("Watch stopped.")
					return nil
				case <-ticker.C:
					if time.Now().Before(nextAllowed) {
						continue
					}
					stats, err := eng.IndexIncremental(ctx, verbose)
					if err != nil {
						fmt.Fprintf(os.Stderr, "watch reindex failed: %v\n", err)
						nextAllowed = time.Now().Add(debounce)
						continue
					}
					if stats.IndexedFiles == 0 && stats.FailedFiles == 0 {
						continue
					}
					fmt.Printf("Refresh: %d indexed, %d skipped, %d failed — %d symbols, %d imports (%.1fs)\n",
						stats.IndexedFiles, stats.SkippedFiles, stats.FailedFiles,
						stats.TotalSymbols, stats.TotalImports, stats.Duration)
					nextAllowed = time.Now().Add(debounce)
				}
			}
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "polling interval for incremental reindexing")
	cmd.Flags().DurationVar(&debounce, "debounce", 250*time.Millisecond, "minimum delay between follow-up refreshes after a change")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "enable watch mode explicitly or via config")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print per-file indexing progress")
	return cmd
}

func renderGraphHTML(w *os.File, graph *api.GraphExport) error {
	return writeGraphHTML(w, graph)
}

func writeGraphHTML(w interface{ Write([]byte) (int, error) }, graph *api.GraphExport) error {
	payload, err := json.Marshal(graph)
	if err != nil {
		return err
	}
	view := struct {
		Title       string
		Focus       string
		Summary     string
		NodeCount   int
		EdgeCount   int
		GraphJSON   template.JS
		HasAnalysis bool
		Analysis    *api.GraphAnalysis
	}{
		Title:       "code-context graph view",
		Focus:       graph.Focus,
		Summary:     graph.Summary,
		NodeCount:   len(graph.Nodes),
		EdgeCount:   len(graph.Edges),
		GraphJSON:   template.JS(payload),
		HasAnalysis: graph.Analysis != nil,
		Analysis:    graph.Analysis,
	}
	return graphHTMLTemplate.Execute(w, view)
}

var graphHTMLTemplate = template.Must(template.New("graph-html").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>{{.Title}}</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 0; background: #0b1020; color: #e5e7eb; }
    header { padding: 20px 24px; border-bottom: 1px solid #1f2937; background: #111827; }
    main { display: grid; grid-template-columns: 360px 1fr; min-height: calc(100vh - 89px); }
    aside { padding: 20px 24px; border-right: 1px solid #1f2937; background: #0f172a; overflow: auto; }
    section { padding: 20px 24px; overflow: auto; }
    h1, h2, h3 { margin-top: 0; }
    .meta { color: #94a3b8; font-size: 14px; }
    .pill { display: inline-block; margin: 4px 6px 0 0; padding: 4px 8px; border-radius: 999px; background: #1f2937; color: #cbd5e1; font-size: 12px; }
    .toolbar { display: flex; gap: 12px; align-items: center; margin-bottom: 16px; flex-wrap: wrap; }
    input, select { padding: 8px 10px; border-radius: 8px; border: 1px solid #334155; background: #020617; color: #e5e7eb; }
    .card { padding: 14px 16px; border: 1px solid #1f2937; border-radius: 12px; background: #111827; margin-bottom: 12px; }
    .node { cursor: pointer; }
    .node:hover { background: #172554; }
    ul { padding-left: 18px; }
    code { color: #93c5fd; }
    pre { background: #020617; padding: 12px; border-radius: 10px; overflow: auto; }
    .muted { color: #94a3b8; }
  </style>
</head>
<body>
  <header>
    <h1>{{.Title}}</h1>
    <div class="meta">{{.Summary}}</div>
    <div class="meta">Nodes: {{.NodeCount}} · Edges: {{.EdgeCount}}{{if .Focus}} · Focus: <code>{{.Focus}}</code>{{end}}</div>
  </header>
  <main>
    <aside>
      <div class="card">
        <h2>Filters</h2>
        <div class="toolbar">
          <input id="search" type="search" placeholder="Search nodes">
          <select id="typeFilter">
            <option value="">All node types</option>
          </select>
        </div>
        <div class="meta">Click a node to inspect its connected edges.</div>
      </div>
      {{if .HasAnalysis}}
      <div class="card">
        <h2>Graph analysis</h2>
        {{if .Analysis.TopImports}}
        <h3>Top imports</h3>
        <ul>{{range .Analysis.TopImports}}<li>{{.Name}} ({{.Count}})</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.MostConnectedFiles}}
        <h3>Most connected files</h3>
        <ul>{{range .Analysis.MostConnectedFiles}}<li>{{.Name}} ({{.Count}})</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.BridgeFiles}}
        <h3>Bridge files</h3>
        <ul>{{range .Analysis.BridgeFiles}}<li>{{.Name}} ({{.Count}})</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.HotspotFiles}}
        <h3>Hotspot files</h3>
        <ul>{{range .Analysis.HotspotFiles}}<li>{{.Name}} ({{.Count}})</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.RelationHighlights}}
        <h3>Relation highlights</h3>
        <ul>{{range .Analysis.RelationHighlights}}<li>{{.}}</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.ReadingPaths}}
        <h3>Reading paths</h3>
        <ul>{{range .Analysis.ReadingPaths}}<li><strong>{{.Entry}}</strong>: {{range $i, $part := .Path}}{{if $i}} → {{end}}{{$part}}{{end}}{{if .Reason}}<div class="meta">{{.Reason}}</div>{{end}}</li>{{end}}</ul>
        {{end}}
        {{if .Analysis.RecommendedFiles}}
        <h3>Recommended files</h3>
        <ul>{{range .Analysis.RecommendedFiles}}<li>{{.}}</li>{{end}}</ul>
        {{end}}
      </div>
      {{end}}
      <div id="nodeList"></div>
    </aside>
    <section>
      <div class="card">
        <h2>Selected node</h2>
        <div id="details" class="muted">Pick a node from the list to inspect its attributes and edges.</div>
      </div>
      <div class="card">
        <h2>Graph payload</h2>
        <pre id="raw"></pre>
      </div>
    </section>
  </main>
  <script>
    const graph = {{.GraphJSON}};
    const search = document.getElementById('search');
    const typeFilter = document.getElementById('typeFilter');
    const nodeList = document.getElementById('nodeList');
    const details = document.getElementById('details');
    const raw = document.getElementById('raw');
    raw.textContent = JSON.stringify(graph, null, 2);

    const types = [...new Set(graph.nodes.map(node => node.type))].sort();
    for (const type of types) {
      const option = document.createElement('option');
      option.value = type;
      option.textContent = type;
      typeFilter.appendChild(option);
    }

    function renderList() {
      const q = search.value.trim().toLowerCase();
      const type = typeFilter.value;
      nodeList.innerHTML = '';
      const filtered = graph.nodes.filter(node => {
        if (type && node.type !== type) return false;
        if (!q) return true;
        return [node.label, node.name, node.file].filter(Boolean).join(' ').toLowerCase().includes(q);
      });
      if (!filtered.length) {
        nodeList.innerHTML = '<div class="card muted">No nodes match the current filters.</div>';
        return;
      }
      for (const node of filtered) {
        const el = document.createElement('div');
        el.className = 'card node';
        const label = node.label || node.id;
        const meta = node.type + (node.file ? ' · ' + node.file : '');
        el.innerHTML = '<strong>' + label + '</strong><div class="meta">' + meta + '</div>';
        el.addEventListener('click', () => renderDetails(node));
        nodeList.appendChild(el);
      }
    }

    function renderDetails(node) {
      const incoming = graph.edges.filter(edge => edge.target === node.id);
      const outgoing = graph.edges.filter(edge => edge.source === node.id);
      details.innerHTML = '';
      const title = document.createElement('div');
      title.innerHTML = '<h3>' + (node.label || node.id) + '</h3><div class="meta">' + node.type + '</div>';
      details.appendChild(title);

      const attrs = document.createElement('div');
      attrs.innerHTML = [
        node.file ? '<span class="pill">file: ' + node.file + '</span>' : '',
        node.name ? '<span class="pill">name: ' + node.name + '</span>' : '',
        node.kind ? '<span class="pill">kind: ' + node.kind + '</span>' : '',
        node.language ? '<span class="pill">language: ' + node.language + '</span>' : '',
        node.line ? '<span class="pill">line: ' + node.line + '</span>' : ''
      ].join('');
      details.appendChild(attrs);

      const edges = document.createElement('div');
      const outgoingHTML = outgoing.map(edge => '<li>' + edge.type + ' → ' + edge.target + '</li>').join('');
      const incomingHTML = incoming.map(edge => '<li>' + edge.type + ' ← ' + edge.source + '</li>').join('');
      edges.innerHTML = '<h3>Outgoing (' + outgoing.length + ')</h3><ul>' + outgoingHTML + '</ul><h3>Incoming (' + incoming.length + ')</h3><ul>' + incomingHTML + '</ul>';
      details.appendChild(edges);
    }

    search.addEventListener('input', renderList);
    typeFilter.addEventListener('change', renderList);
    renderList();
  </script>
</body>
</html>`))

func newServeCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := engine.New(root, dbPath)
			if err != nil {
				return err
			}
			defer eng.Close()

			srv := server.New(eng, port)
			return srv.Run()
		},
	}
	cmd.Flags().IntVar(&port, "port", 9090, "HTTP port")
	return cmd
}
