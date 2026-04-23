package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	serverPort int
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
		newServeCmd(),
	)

	attachServeConfig(cmd)

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
		serverPort: loaded.Config.Server.Port,
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
	cmd.AddCommand(newGraphPathCmd(), newGraphNeighborsCmd(), newGraphSubgraphCmd())
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
			fmt.Printf("Summary: %s\n\n", s.Summary)

			for _, f := range s.Files {
				fmt.Printf("--- %s ---\n", f.Path)
				fmt.Printf("Language: %s\n", f.Language)
				fmt.Printf("Symbols (%d):\n", len(f.Symbols))
				symLimit := min(len(f.Symbols), 5)
				for _, sym := range f.Symbols[:symLimit] {
					fmt.Printf("  %s (%s)\n", sym.Name, sym.Kind)
				}
				if len(f.Symbols) > 5 {
					fmt.Printf("  ... and %d more\n", len(f.Symbols)-5)
				}
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
	if len(analysis.RecommendedFiles) > 0 {
		fmt.Printf("  Recommended files: %s\n", strings.Join(analysis.RecommendedFiles, ", "))
	}
}

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
