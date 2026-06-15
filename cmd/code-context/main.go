package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/config"
	"github.com/sjzsdu/code-context/internal/engine"
	"github.com/sjzsdu/code-context/internal/graphhtml"
	"github.com/sjzsdu/code-context/internal/search"
	"github.com/sjzsdu/code-context/internal/server"
	"github.com/sjzsdu/code-context/internal/store"
)

var (
	root           string
	dbPath         string
	storeBackend   string
	helixURL       string
	helixAPIKey    string
	helixAPIKeyEnv string
)

type runtimeConfig struct {
	serverPort    int
	watchEnabled  bool
	watchInterval time.Duration
	watchDebounce time.Duration
}

func main() {
	cmd := &cobra.Command{
		Use:   "code-context",
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
	cmd.PersistentFlags().StringVar(&storeBackend, "store-backend", "", "storage backend (sqlite|helix; default: sqlite)")
	cmd.PersistentFlags().StringVar(&helixURL, "helix-url", "", "HelixDB endpoint URL for --store-backend=helix")
	cmd.PersistentFlags().StringVar(&helixAPIKey, "helix-api-key", "", "HelixDB API key for --store-backend=helix")
	cmd.PersistentFlags().StringVar(&helixAPIKeyEnv, "helix-api-key-env", "", "environment variable containing the HelixDB API key")

	cmd.AddCommand(
		newIndexCmd(),
		newSearchCmd(),
		newFindDefCmd(),
		newGitFilesCmd(),
		newGitDiffCmd(),
		newFilesCmd(),
		newImportsCmd(),
		newImportersCmd(),
		newCallersCmd(),
		newCalleesCmd(),
		newRoutesCmd(),
		newRouteContextCmd(),
		newDocsForCmd(),
		newDocDriftCmd(),
		newDocCoverageCmd(),
		newStatsCmd(),
		newStatusCmd(),
		newFreshnessCmd(),
		newDoctorCmd(),
		newCICmd(),
		newRebuildCmd(),
		newMapCmd(),
		newGraphCmd(),
		newExplainCmd(),
		newContextCmd(),
		newSnapshotCmd(),
		newSnapshotGitCmd(),
		newReviewContextCmd(),
		newImpactCmd(),
		newImpactGitCmd(),
		newTestImpactCmd(),
		newSymbolImpactCmd(),
		newTraceCmd(),
		newDiffImpactCmd(),
		newDiffImpactGitCmd(),
		newWatchCmd(),
		newServeCmd(),
	)

	attachServeConfig(cmd)
	attachWatchConfig(cmd)
	attachDocsConfig(cmd)

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
		if loaded, err := config.Load(root); err == nil {
			switch {
			case loaded.Config.DB != "":
				dbPath = loaded.Config.DB
			case loaded.Config.Store.SQLite.DB != "":
				dbPath = loaded.Config.Store.SQLite.DB
			}
		}
	}
	if !cmd.Flags().Changed("store-backend") {
		if loaded, err := config.Load(root); err == nil && loaded.Config.Store.Backend != "" {
			storeBackend = loaded.Config.Store.Backend
		}
	}
	if !cmd.Flags().Changed("helix-url") {
		if loaded, err := config.Load(root); err == nil && loaded.Config.Store.Helix.URL != "" {
			helixURL = loaded.Config.Store.Helix.URL
		}
	}
	if !cmd.Flags().Changed("helix-api-key") {
		if loaded, err := config.Load(root); err == nil && loaded.Config.Store.Helix.APIKey != "" {
			helixAPIKey = loaded.Config.Store.Helix.APIKey
		}
	}
	if !cmd.Flags().Changed("helix-api-key-env") {
		if loaded, err := config.Load(root); err == nil && loaded.Config.Store.Helix.APIKeyEnv != "" {
			helixAPIKeyEnv = loaded.Config.Store.Helix.APIKeyEnv
		}
	}
	_ = cfg
}

func newEngine() (*engine.Engine, error) {
	return engine.NewWithStoreOptions(root, storeOptions())
}

func storeOptions() store.Options {
	return store.Options{
		Backend: store.Backend(storeBackend),
		SQLite:  store.SQLiteOptions{Path: dbPath},
		Helix: store.HelixOptions{
			URL:       helixURL,
			APIKey:    helixAPIKey,
			APIKeyEnv: helixAPIKeyEnv,
		},
	}
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
		if !cmd.Flags().Changed("port") && loaded.Config.Server.Port > 0 {
			if flag := cmd.Flags().Lookup("port"); flag != nil {
				_ = flag.Value.Set(fmt.Sprintf("%d", loaded.Config.Server.Port))
			}
		}
		if !cmd.Flags().Changed("watch") && loaded.Config.Watch.Enabled {
			if flag := cmd.Flags().Lookup("watch"); flag != nil {
				_ = flag.Value.Set("true")
			}
		}
		if !cmd.Flags().Changed("watch-interval") && loaded.Config.Watch.Interval > 0 {
			if flag := cmd.Flags().Lookup("watch-interval"); flag != nil {
				_ = flag.Value.Set(loaded.Config.Watch.Interval.String())
			}
		}
		if !cmd.Flags().Changed("watch-debounce") && loaded.Config.Watch.Debounce > 0 {
			if flag := cmd.Flags().Lookup("watch-debounce"); flag != nil {
				_ = flag.Value.Set(loaded.Config.Watch.Debounce.String())
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

func attachDocsConfig(rootCmd *cobra.Command) {
	if docDriftCmd, _, err := rootCmd.Find([]string{"doc-drift"}); err == nil && docDriftCmd != nil {
		prev := docDriftCmd.PreRunE
		docDriftCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
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
			if !cmd.Flags().Changed("fail-on-broken") && loaded.Config.Docs.FailOnBroken {
				if flag := cmd.Flags().Lookup("fail-on-broken"); flag != nil {
					_ = flag.Value.Set("true")
				}
			}
			return nil
		}
	}

	if docCoverageCmd, _, err := rootCmd.Find([]string{"doc-coverage"}); err == nil && docCoverageCmd != nil {
		prev := docCoverageCmd.PreRunE
		docCoverageCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
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
			if !cmd.Flags().Changed("min-route-coverage") && loaded.Config.Docs.MinRouteCoverage != nil {
				if flag := cmd.Flags().Lookup("min-route-coverage"); flag != nil {
					_ = flag.Value.Set(fmt.Sprintf("%g", *loaded.Config.Docs.MinRouteCoverage))
				}
			}
			if !cmd.Flags().Changed("min-symbol-coverage") && loaded.Config.Docs.MinSymbolCoverage != nil {
				if flag := cmd.Flags().Lookup("min-symbol-coverage"); flag != nil {
					_ = flag.Value.Set(fmt.Sprintf("%g", *loaded.Config.Docs.MinSymbolCoverage))
				}
			}
			return nil
		}
	}
}

func newIndexCmd() *cobra.Command {
	var incremental bool
	var verbose bool
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index the codebase",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
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

			if stats.TotalDocuments > 0 {
				fmt.Printf("\nDone: %d indexed (%d docs), %d skipped, %d failed — %d symbols, %d imports (%.1fs)\n",
					stats.IndexedFiles, stats.TotalDocuments, stats.SkippedFiles, stats.FailedFiles,
					stats.TotalSymbols, stats.TotalImports, stats.Duration)
			} else {
				fmt.Printf("\nDone: %d indexed, %d skipped, %d failed — %d symbols, %d imports (%.1fs)\n",
					stats.IndexedFiles, stats.SkippedFiles, stats.FailedFiles,
					stats.TotalSymbols, stats.TotalImports, stats.Duration)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&incremental, "incremental", false, "only reindex changed files")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print per-file indexing progress")
	return cmd
}

func newRebuildCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Clear the current index and rebuild it from disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			stats, err := eng.Rebuild(context.Background(), verbose)
			if err != nil {
				return err
			}
			fmt.Printf("Rebuilt index: %d files, %d symbols, %d imports, %d docs (%.1fs)\n", stats.IndexedFiles, stats.TotalSymbols, stats.TotalImports, stats.TotalDocuments, stats.Duration)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print per-file indexing progress")
	return cmd
}

func newDoctorCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check database schema, index freshness, and service health",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			report, err := eng.Doctor(context.Background())
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			fmt.Println(report.Summary)
			for _, c := range report.Checks {
				fmt.Printf("  [%s] %s: %s\n", c.Status, c.Name, c.Message)
			}
			if len(report.Schema.MissingTables) > 0 || len(report.Schema.MissingIndexes) > 0 {
				fmt.Printf("Missing tables: %s\n", strings.Join(report.Schema.MissingTables, ", "))
				fmt.Printf("Missing indexes: %s\n", strings.Join(report.Schema.MissingIndexes, ", "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print JSON report")
	return cmd
}

type ciReport struct {
	OK          bool                   `json:"ok"`
	Summary     string                 `json:"summary"`
	Doctor      *api.DoctorReport      `json:"doctor,omitempty"`
	DocDrift    *api.DocDriftReport    `json:"doc_drift,omitempty"`
	DocCoverage *api.DocCoverageReport `json:"doc_coverage,omitempty"`
	Failures    []string               `json:"failures,omitempty"`
}

func newCICmd() *cobra.Command {
	var jsonOut bool
	var failOnBroken bool
	var minRouteCoverage float64
	var minSymbolCoverage float64
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Run doctor and documentation health checks for CI",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := config.Load(root)
			if err != nil {
				if err == config.ErrNotFound {
					return nil
				}
				return err
			}
			if !cmd.Flags().Changed("fail-on-broken") && loaded.Config.Docs.FailOnBroken {
				failOnBroken = true
			}
			if !cmd.Flags().Changed("min-route-coverage") && loaded.Config.Docs.MinRouteCoverage != nil {
				minRouteCoverage = *loaded.Config.Docs.MinRouteCoverage
			}
			if !cmd.Flags().Changed("min-symbol-coverage") && loaded.Config.Docs.MinSymbolCoverage != nil {
				minSymbolCoverage = *loaded.Config.Docs.MinSymbolCoverage
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()

			report := &ciReport{OK: true}
			ctx := context.Background()
			report.Doctor, err = eng.Doctor(ctx)
			if err != nil {
				return err
			}
			if !report.Doctor.OK {
				report.OK = false
				report.Failures = append(report.Failures, "doctor failed")
			}
			report.DocDrift, err = eng.DocDrift(ctx)
			if err != nil {
				return err
			}
			if failOnBroken && len(report.DocDrift.Broken) > 0 {
				report.OK = false
				report.Failures = append(report.Failures, fmt.Sprintf("doc-drift found %d broken references", len(report.DocDrift.Broken)))
			}
			report.DocCoverage, err = eng.DocCoverage(ctx)
			if err != nil {
				return err
			}
			if minRouteCoverage > 100 || minSymbolCoverage > 100 {
				return fmt.Errorf("coverage thresholds must be between 0 and 100")
			}
			if coverageErr := docCoverageThresholdError(report.DocCoverage, minRouteCoverage, minSymbolCoverage); coverageErr != nil {
				report.OK = false
				report.Failures = append(report.Failures, coverageErr.Error())
			}
			if report.OK {
				report.Summary = "ci checks passed"
			} else {
				report.Summary = fmt.Sprintf("ci checks failed: %s", strings.Join(report.Failures, "; "))
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return err
				}
			} else {
				fmt.Println(report.Summary)
				fmt.Printf("  doctor: %s\n", report.Doctor.Summary)
				fmt.Printf("  doc-drift: %s\n", report.DocDrift.Summary)
				fmt.Printf("  doc-coverage: %s\n", report.DocCoverage.Summary)
			}
			if !report.OK {
				return fmt.Errorf("%s", report.Summary)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print JSON report")
	cmd.Flags().BoolVar(&failOnBroken, "fail-on-broken", false, "fail when broken document references are found")
	cmd.Flags().Float64Var(&minRouteCoverage, "min-route-coverage", -1, "fail when route doc coverage is below this percentage (0-100)")
	cmd.Flags().Float64Var(&minSymbolCoverage, "min-symbol-coverage", -1, "fail when public symbol doc coverage is below this percentage (0-100)")
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			if stats.TotalDocuments > 0 {
				fmt.Printf("Documents: %d\n", stats.TotalDocuments)
			}
			if stats.IndexVersion != "" {
				fmt.Printf("Index version: %s\n", stats.IndexVersion)
			}
			if stats.LastIndexedAt != "" {
				fmt.Printf("Last indexed:  %s\n", stats.LastIndexedAt)
			}
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show workflow and service status metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()

			status, err := eng.Status(context.Background())
			if err != nil {
				return err
			}
			fmt.Printf("Root:          %s\n", status.Root)
			fmt.Printf("Database:      %s\n", status.DatabasePath)
			fmt.Printf("Graph version: %s\n", status.GraphVersion)
			if status.Index != nil {
				fmt.Printf("Index version: %s\n", status.Index.IndexVersion)
				fmt.Printf("Files:         %d\n", status.Index.TotalFiles)
				fmt.Printf("Symbols:       %d\n", status.Index.TotalSymbols)
				fmt.Printf("Imports:       %d\n", status.Index.TotalImports)
				if status.Index.LastIndexedAt != "" {
					fmt.Printf("Last indexed:  %s\n", status.Index.LastIndexedAt)
				}
			}
			if status.Watch != nil {
				fmt.Printf("Watch enabled: %t\n", status.Watch.Enabled)
				fmt.Printf("Watch running: %t\n", status.Watch.Running)
				if status.Watch.Interval != "" {
					fmt.Printf("Watch interval: %s\n", status.Watch.Interval)
				}
				if status.Watch.Debounce != "" {
					fmt.Printf("Watch debounce: %s\n", status.Watch.Debounce)
				}
				if status.Watch.LastRefreshAt != "" {
					fmt.Printf("Last refresh:  %s\n", status.Watch.LastRefreshAt)
				}
				if status.Watch.LastRefreshStatus != "" {
					fmt.Printf("Refresh source: %s\n", status.Watch.LastRefreshStatus)
				}
				if status.Watch.LastRefreshSummary != "" {
					fmt.Printf("Refresh summary: %s\n", status.Watch.LastRefreshSummary)
				}
				if status.Watch.LastError != "" {
					fmt.Printf("Last error:    %s\n", status.Watch.LastError)
				}
				if status.Watch.Freshness != nil {
					fmt.Printf("Freshness:     %s\n", status.Watch.Freshness.Summary)
				}
				if status.Watch.Stale {
					fmt.Printf("Index stale:   true\n")
					fmt.Printf("Pending files: %d\n", len(status.Watch.PendingFiles))
					if status.Watch.Freshness != nil {
						for _, item := range status.Watch.Freshness.Items {
							fmt.Printf("  %s [%s %s]\n", item.Path, item.Kind, item.Reason)
						}
					} else {
						for _, f := range status.Watch.PendingFiles {
							fmt.Printf("  %s\n", f)
						}
					}
				}
			}
			return nil
		},
	}
}

func newFreshnessCmd() *cobra.Command {
	var limit int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "freshness",
		Short: "Show indexed files/documents that differ from the filesystem",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			report, err := eng.Freshness(context.Background(), limit)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			fmt.Println(report.Summary)
			for _, item := range report.Items {
				fmt.Printf("  %s [%s %s]\n", item.Path, item.Kind, item.Reason)
			}
			if report.Truncated {
				fmt.Printf("  ... truncated at %d items\n", len(report.Items))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "max pending items to print, 0 for all")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print JSON report")
	return cmd
}

func newCallersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "callers <symbol>",
		Short: "Show functions or methods that call a symbol name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			calls, err := eng.Callers(context.Background(), args[0])
			if err != nil {
				return err
			}
			printCalls(calls)
			return nil
		},
	}
}

func newCalleesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "callees <symbol>",
		Short: "Show symbols called by a function or method",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			calls, err := eng.Callees(context.Background(), args[0])
			if err != nil {
				return err
			}
			printCalls(calls)
			return nil
		},
	}
}

func printCalls(calls []api.CallEdge) {
	for _, c := range calls {
		fmt.Printf("  %s:%d  %s -> %s [%s]\n", c.FromFile, c.Line, c.FromSymbol, c.ToName, c.Confidence)
	}
	fmt.Printf("\n%d calls\n", len(calls))
}

func newRoutesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "routes [query]",
		Short: "List framework routes discovered in indexed code",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			routes, err := eng.Routes(context.Background(), query)
			if err != nil {
				return err
			}
			printRoutes(routes)
			return nil
		},
	}
}

func printRoutes(routes []api.Route) {
	for _, r := range routes {
		method := r.Method
		if method == "" {
			method = "*"
		}
		fmt.Printf("  %-7s %-30s %-18s %s:%d [%s]\n", method, r.Path, r.Handler, r.FilePath, r.Line, r.Framework)
	}
	fmt.Printf("\n%d routes\n", len(routes))
}

func newRouteContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "route-context <query>",
		Short: "Analyze route-level impact using handlers, calls, docs, tests, and risk",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			ctx, err := eng.RouteContext(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Route context: %s\n", ctx.Query)
			fmt.Printf("Summary: %s\n", ctx.Summary)
			fmt.Printf("Risk: %s (%d)\n", ctx.Risk.Level, ctx.Risk.Score)
			for _, reason := range ctx.Risk.Reasons {
				fmt.Printf("  - %s\n", reason)
			}
			fmt.Printf("\nRoutes (%d):\n", len(ctx.Routes))
			printRoutes(ctx.Routes)
			fmt.Printf("Handlers (%d):\n", len(ctx.Handlers))
			for _, h := range ctx.Handlers {
				fmt.Printf("  %s:%d %s (%s)\n", h.FilePath, h.Line, h.Name, h.Kind)
			}
			fmt.Printf("\nCallers (%d):\n", len(ctx.Callers))
			printCalls(ctx.Callers)
			fmt.Printf("Callees (%d):\n", len(ctx.Callees))
			printCalls(ctx.Callees)
			fmt.Printf("Related docs (%d):\n", len(ctx.RelatedDocs))
			for _, d := range ctx.RelatedDocs {
				fmt.Printf("  %s:%d %s:%s\n", d.DocumentPath, d.Line, d.TargetType, d.TargetValue)
			}
			fmt.Printf("Recommended tests (%d):\n", len(ctx.RecommendedTests))
			for _, t := range ctx.RecommendedTests {
				fmt.Printf("  %s\n", t)
			}
			return nil
		},
	}
}

func newDocsForCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs-for <query>",
		Short: "Show documents that reference a file, symbol, module, or text query",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			refs, err := eng.DocsFor(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Docs for %q:\n", refs.Query)
			for _, link := range refs.Links {
				section := ""
				if link.SectionTitle != "" {
					section = fmt.Sprintf(" #%s", link.SectionSlug)
				}
				fmt.Printf("  %s:%d%s  %s:%s  %s (%.1f)\n", link.DocumentPath, link.Line, section, link.TargetType, link.TargetValue, link.Evidence, link.Confidence)
			}
			fmt.Printf("\n%d document references\n", len(refs.Links))
			return nil
		},
	}
}

func newDocDriftCmd() *cobra.Command {
	var jsonOut bool
	var failOnBroken bool
	cmd := &cobra.Command{
		Use:   "doc-drift",
		Short: "Find stale document references to missing files, symbols, modules, or routes",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			report, err := eng.DocDrift(context.Background())
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return err
				}
				if failOnBroken && len(report.Broken) > 0 {
					return fmt.Errorf("doc-drift found %d broken references", len(report.Broken))
				}
				return nil
			}
			fmt.Println(report.Summary)
			for _, item := range report.Broken {
				section := ""
				if item.SectionTitle != "" {
					section = fmt.Sprintf(" #%s", item.SectionSlug)
				}
				fmt.Printf("  %s:%d%s  %s:%s  %s\n", item.DocumentPath, item.Line, section, item.TargetType, item.TargetValue, item.Reason)
			}
			if failOnBroken && len(report.Broken) > 0 {
				return fmt.Errorf("doc-drift found %d broken references", len(report.Broken))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print JSON report")
	cmd.Flags().BoolVar(&failOnBroken, "fail-on-broken", false, "exit non-zero when broken document references are found")
	return cmd
}

func newDocCoverageCmd() *cobra.Command {
	var jsonOut bool
	var minRouteCoverage float64
	var minSymbolCoverage float64
	cmd := &cobra.Command{
		Use:   "doc-coverage",
		Short: "Find indexed routes and public symbols that are not referenced by documentation",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			report, err := eng.DocCoverage(context.Background())
			if err != nil {
				return err
			}
			if minRouteCoverage > 100 || minSymbolCoverage > 100 {
				return fmt.Errorf("coverage thresholds must be between 0 and 100")
			}
			coverageErr := docCoverageThresholdError(report, minRouteCoverage, minSymbolCoverage)
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return err
				}
				return coverageErr
			}
			fmt.Println(report.Summary)
			if len(report.MissingRoutes) > 0 {
				fmt.Printf("\nMissing routes (%d):\n", len(report.MissingRoutes))
			}
			for _, route := range report.MissingRoutes {
				method := route.Method
				if method == "" {
					method = "*"
				}
				fmt.Printf("  %-7s %-30s %-18s %s:%d [%s]\n", method, route.Path, route.Handler, route.FilePath, route.Line, route.Framework)
			}
			if len(report.MissingSymbols) > 0 {
				fmt.Printf("\nMissing public symbols (%d):\n", len(report.MissingSymbols))
			}
			for _, sym := range report.MissingSymbols {
				fmt.Printf("  %-10s %-24s %s:%d\n", sym.Kind, sym.Name, sym.FilePath, sym.Line)
			}
			return coverageErr
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print JSON report")
	cmd.Flags().Float64Var(&minRouteCoverage, "min-route-coverage", -1, "exit non-zero when route doc coverage is below this percentage (0-100)")
	cmd.Flags().Float64Var(&minSymbolCoverage, "min-symbol-coverage", -1, "exit non-zero when public symbol doc coverage is below this percentage (0-100)")
	return cmd
}

func docCoverageThresholdError(report *api.DocCoverageReport, minRouteCoverage, minSymbolCoverage float64) error {
	var failures []string
	if minRouteCoverage >= 0 && report.RouteCoveragePercent < minRouteCoverage {
		failures = append(failures, fmt.Sprintf("route coverage %.1f%% below %.1f%%", report.RouteCoveragePercent, minRouteCoverage))
	}
	if minSymbolCoverage >= 0 && report.SymbolCoveragePercent < minSymbolCoverage {
		failures = append(failures, fmt.Sprintf("symbol coverage %.1f%% below %.1f%%", report.SymbolCoveragePercent, minSymbolCoverage))
	}
	if len(failures) == 0 {
		return nil
	}
	return fmt.Errorf("doc-coverage threshold failed: %s", strings.Join(failures, "; "))
}

func newMapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "map",
		Short: "Show project architecture overview",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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
		Use:   "snapshot [query]",
		Short: "Generate LLM context package for the project or an optional query",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()

			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			s, err := eng.Snapshot(context.Background(), query, limit)
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
			eng, err := newEngine()
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

func newReviewContextCmd() *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:   "review-context",
		Short: "Generate git-aware review context with risk, docs, routes, and tests",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			gitState, err := engine.ParseGitState(state)
			if err != nil {
				return err
			}
			r, err := eng.ReviewContext(context.Background(), gitState)
			if err != nil {
				return err
			}
			fmt.Printf("Review context (%s)\n%s\n", r.State, r.Summary)
			fmt.Printf("Risk: %s (%d)\n", r.Risk.Level, r.Risk.Score)
			for _, reason := range r.Risk.Reasons {
				fmt.Printf("  - %s\n", reason)
			}
			fmt.Printf("\nChanged files (%d):\n", len(r.ChangedFiles))
			for _, f := range r.ChangedFiles {
				fmt.Printf("  %s\n", f)
			}
			fmt.Printf("\nChanged symbols (%d):\n", len(r.ChangedSymbols))
			for _, s := range r.ChangedSymbols {
				fmt.Printf("  %s:%d %s (%s)\n", s.FilePath, s.Line, s.Name, s.Kind)
			}
			fmt.Printf("\nRoutes (%d):\n", len(r.Routes))
			printRoutes(r.Routes)
			fmt.Printf("Related docs (%d):\n", len(r.RelatedDocs))
			for _, d := range r.RelatedDocs {
				fmt.Printf("  %s:%d %s:%s\n", d.DocumentPath, d.Line, d.TargetType, d.TargetValue)
			}
			fmt.Printf("\nRecommended tests (%d):\n", len(r.RecommendedTests))
			for _, t := range r.RecommendedTests {
				fmt.Printf("  %s\n", t)
			}
			printTestCommands(r.TestCommands)
			fmt.Printf("\nSuggested review order:\n")
			for i, f := range r.SuggestedReviewOrder {
				fmt.Printf("  %d. %s\n", i+1, f)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "unstaged", "git change state: unstaged, staged, or all")
	return cmd
}

func newImpactCmd() *cobra.Command {
	var jsonOut bool
	var depth int
	cmd := &cobra.Command{
		Use:   "impact <file-or-symbol>",
		Short: "Analyze impact for a file or symbol using imports, calls, routes, docs, and tests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			impact, err := eng.Impact(context.Background(), args[0], depth)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(impact)
			}
			printImpact(impact)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print JSON report")
	cmd.Flags().IntVar(&depth, "depth", 3, "dependency depth for file impact")
	return cmd
}

func newImpactGitCmd() *cobra.Command {
	var jsonOut bool
	var depth int
	var state string
	cmd := &cobra.Command{
		Use:   "impact-git",
		Short: "Analyze impact for files and symbols changed in local git state",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			gitState, err := engine.ParseGitState(state)
			if err != nil {
				return err
			}
			impact, err := eng.ImpactGit(context.Background(), gitState, depth)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(impact)
			}
			printGitImpact(impact)
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "unstaged", "git change state: unstaged, staged, or all")
	cmd.Flags().IntVar(&depth, "depth", 3, "dependency depth for file impact")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print JSON report")
	return cmd
}

func printGitImpact(impact *engine.GitImpact) {
	fmt.Printf("Git impact (%s)\n%s\n", impact.State, impact.Summary)
	fmt.Printf("Risk: %s (%d)\n", impact.Risk.Level, impact.Risk.Score)
	for _, reason := range impact.Risk.Reasons {
		fmt.Printf("  - %s\n", reason)
	}
	fmt.Printf("\nChanged files (%d):\n", len(impact.ChangedFiles))
	for _, f := range impact.ChangedFiles {
		fmt.Printf("  %s\n", f)
	}
	fmt.Printf("\nChanged symbols (%d):\n", len(impact.ChangedSymbols))
	for _, s := range impact.ChangedSymbols {
		fmt.Printf("  %s:%d %s (%s)\n", s.FilePath, s.Line, s.Name, s.Kind)
	}
	fmt.Printf("\nFile impacts (%d):\n", len(impact.FileImpacts))
	for _, f := range impact.FileImpacts {
		fmt.Printf("  %s: %d deps, %d dependents, %d tests\n", f.File, len(f.AllDeps), len(f.Dependents), len(f.Recommends))
	}
	fmt.Printf("\nSymbol impacts (%d):\n", len(impact.SymbolImpacts))
	for _, s := range impact.SymbolImpacts {
		fmt.Printf("  %s: %s, %d callers, %d routes\n", s.Symbol.Name, s.Risk.Level, len(s.Callers), len(s.Routes))
	}
	fmt.Printf("\nRecommended tests (%d):\n", len(impact.RecommendedTests))
	for _, t := range impact.RecommendedTests {
		fmt.Printf("  %s\n", t)
	}
	printTestCommands(impact.TestCommands)
}

func printImpact(impact *engine.ImpactResult) {
	fmt.Printf("Impact: %s (%s)\n", impact.Target, impact.Kind)
	fmt.Printf("Summary: %s\n", impact.Summary)
	if impact.FileImpact != nil {
		printFileImpact(impact.FileImpact)
	}
	if impact.SymbolImpact != nil {
		printSymbolImpact(impact.SymbolImpact)
	}
}

func printFileImpact(d *engine.DiffImpact) {
	fmt.Printf("\nFile: %s\n", d.File)
	fmt.Printf("Direct imports (%d):\n", len(d.DirectDeps))
	for _, dep := range d.DirectDeps {
		fmt.Printf("  %s\n", dep)
	}
	fmt.Printf("All dependencies (%d):\n", len(d.AllDeps))
	for _, dep := range d.AllDeps {
		fmt.Printf("  %s\n", dep)
	}
	fmt.Printf("Dependents (%d):\n", len(d.Dependents))
	for _, dep := range d.Dependents {
		fmt.Printf("  %s\n", dep)
	}
	if len(d.Recommends) > 0 {
		fmt.Printf("Recommended tests (%d):\n", len(d.Recommends))
		for _, r := range d.Recommends {
			fmt.Printf("  %s\n", r)
		}
	}
}

func printSymbolImpact(impact *engine.SymbolImpact) {
	fmt.Printf("\nSymbol: %s (%s) at %s:%d\n", impact.Symbol.Name, impact.Symbol.Kind, impact.Symbol.FilePath, impact.Symbol.Line)
	fmt.Printf("Risk: %s (%d)\n", impact.Risk.Level, impact.Risk.Score)
	for _, reason := range impact.Risk.Reasons {
		fmt.Printf("  - %s\n", reason)
	}
	fmt.Printf("Direct imports (%d):\n", len(impact.DirectDeps))
	for _, dep := range impact.DirectDeps {
		fmt.Printf("  %s\n", dep)
	}
	fmt.Printf("Dependents (%d):\n", len(impact.Dependents))
	for _, dep := range impact.Dependents {
		fmt.Printf("  %s\n", dep)
	}
	fmt.Printf("Callers (%d):\n", len(impact.Callers))
	printCalls(impact.Callers)
	fmt.Printf("Callees (%d):\n", len(impact.Callees))
	printCalls(impact.Callees)
	fmt.Printf("Routes (%d):\n", len(impact.Routes))
	printRoutes(impact.Routes)
	fmt.Printf("Related docs (%d):\n", len(impact.RelatedDocs))
	for _, d := range impact.RelatedDocs {
		fmt.Printf("  %s:%d %s:%s\n", d.DocumentPath, d.Line, d.TargetType, d.TargetValue)
	}
	fmt.Printf("Recommended tests (%d):\n", len(impact.RecommendedTests))
	for _, t := range impact.RecommendedTests {
		fmt.Printf("  %s\n", t)
	}
}

func newTestImpactCmd() *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:   "test-impact",
		Short: "Recommend tests for git changed files and symbols",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			gitState, err := engine.ParseGitState(state)
			if err != nil {
				return err
			}
			t, err := eng.TestImpact(context.Background(), gitState)
			if err != nil {
				return err
			}
			fmt.Println(t.Summary)
			fmt.Printf("Changed symbols (%d):\n", len(t.ChangedSymbols))
			for _, s := range t.ChangedSymbols {
				fmt.Printf("  %s:%d %s (%s)\n", s.FilePath, s.Line, s.Name, s.Kind)
			}
			fmt.Printf("Recommended tests (%d):\n", len(t.RecommendedTests))
			for _, r := range t.RecommendedTests {
				fmt.Printf("  %s\n", r)
			}
			printTestCommands(t.TestCommands)
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "unstaged", "git change state: unstaged, staged, or all")
	return cmd
}

func newSymbolImpactCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "symbol-impact <symbol>",
		Short: "Analyze symbol-level impact using callers, callees, routes, docs, and tests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()
			impact, err := eng.SymbolImpact(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Symbol impact: %s (%s) at %s:%d\n", impact.Symbol.Name, impact.Symbol.Kind, impact.Symbol.FilePath, impact.Symbol.Line)
			fmt.Printf("Summary: %s\n", impact.Summary)
			fmt.Printf("Risk: %s (%d)\n", impact.Risk.Level, impact.Risk.Score)
			for _, reason := range impact.Risk.Reasons {
				fmt.Printf("  - %s\n", reason)
			}
			fmt.Printf("\nCallers (%d):\n", len(impact.Callers))
			printCalls(impact.Callers)
			fmt.Printf("Callees (%d):\n", len(impact.Callees))
			printCalls(impact.Callees)
			fmt.Printf("Routes (%d):\n", len(impact.Routes))
			printRoutes(impact.Routes)
			fmt.Printf("Related docs (%d):\n", len(impact.RelatedDocs))
			for _, d := range impact.RelatedDocs {
				fmt.Printf("  %s:%d %s:%s\n", d.DocumentPath, d.Line, d.TargetType, d.TargetValue)
			}
			fmt.Printf("Recommended tests (%d):\n", len(impact.RecommendedTests))
			for _, t := range impact.RecommendedTests {
				fmt.Printf("  %s\n", t)
			}
			return nil
		},
	}
}

func printTestCommands(commands []engine.TestCommand) {
	if len(commands) == 0 {
		return
	}
	fmt.Printf("Recommended test commands (%d):\n", len(commands))
	for _, cmd := range commands {
		if cmd.Reason != "" {
			fmt.Printf("  %s  # %s\n", cmd.Command, cmd.Reason)
		} else {
			fmt.Printf("  %s\n", cmd.Command)
		}
	}
}

func newTraceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace <from> <to>",
		Short: "Trace call chain between two symbols",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
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
			eng, err := newEngine()
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
			eng, err := newEngine()
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

			eng, err := newEngine()
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
	return graphhtml.Render(w, root, graph)
}

func newServeCmd() *cobra.Command {
	var port int
	var watch bool
	var watchInterval time.Duration
	var watchDebounce time.Duration
	var verbose bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			defer eng.Close()

			eng.SetWatchConfiguration(watch, watchInterval, watchDebounce)
			if watch {
				if err := eng.StartBackgroundWatch(watchInterval, watchDebounce, verbose); err != nil {
					return err
				}
			}

			srv := server.New(eng, port)
			return srv.Run()
		},
	}
	cmd.Flags().IntVar(&port, "port", 9090, "HTTP port")
	cmd.Flags().BoolVar(&watch, "watch", false, "enable background watch refresh while serving")
	cmd.Flags().DurationVar(&watchInterval, "watch-interval", 2*time.Second, "polling interval for background watch refresh")
	cmd.Flags().DurationVar(&watchDebounce, "watch-debounce", 250*time.Millisecond, "minimum delay between follow-up background refreshes")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print per-file indexing progress for background watch")
	return cmd
}
