package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	answerpkg "github.com/sjzsdu/code-context/internal/answer"
	"github.com/sjzsdu/code-context/internal/api"
	embeddingpkg "github.com/sjzsdu/code-context/internal/embedding"
	"github.com/sjzsdu/code-context/internal/graph"
	"github.com/sjzsdu/code-context/internal/indexer"
	"github.com/sjzsdu/code-context/internal/lang"
	"github.com/sjzsdu/code-context/internal/parser"
	"github.com/sjzsdu/code-context/internal/search"
	"github.com/sjzsdu/code-context/internal/store"
)

type Engine struct {
	root        string
	dbPath      string
	store       store.Store
	embedder    store.Embedder
	answerer    store.Answerer
	options     Options
	parser      parser.Parser
	indexer     *indexer.Indexer
	search      *search.Searcher
	graph       *graph.Graph
	watchMu     sync.RWMutex
	watchStatus api.WatchStatus
	watchCancel context.CancelFunc
}

const (
	graphExportVersion = "graph-export.v2"
)

var ErrCapabilityUnsupported = errors.New("capability unsupported")

func New(root string, dbPath string) (*Engine, error) {
	return NewWithStoreOptions(root, store.Options{
		Backend: store.BackendSQLite,
		SQLite:  store.SQLiteOptions{Path: dbPath},
	})
}

func NewWithStoreOptions(root string, storeOpts store.Options) (*Engine, error) {
	return NewWithOptions(root, Options{Store: storeOpts})
}

type Options struct {
	Store     store.Options
	Embedding embeddingpkg.Options
	Answer    answerpkg.Options
}

func NewWithOptions(root string, opts Options) (*Engine, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	root, _ = filepath.Abs(root)

	storeLocation := ""
	switch opts.Store.BackendOrDefault() {
	case store.BackendSQLite:
		if opts.Store.SQLite.Path == "" {
			opts.Store.SQLite.Path = filepath.Join(root, ".code-context", "index.db")
		}
		if err := os.MkdirAll(filepath.Dir(opts.Store.SQLite.Path), 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite store directory: %w", err)
		}
		storeLocation = opts.Store.SQLite.Path
	case store.BackendHelix:
		if strings.TrimSpace(opts.Store.Helix.ProjectID) == "" {
			opts.Store.Helix.ProjectID = root
		}
		storeLocation = opts.Store.Helix.URL
	}

	reg := lang.NewRegistry()
	p := parser.NewTreeSitterParser(reg)
	s, err := store.New(opts.Store)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	embedder, err := embeddingpkg.New(opts.Embedding)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("configure embedding provider: %w", err)
	}
	answerer, err := answerpkg.New(opts.Answer)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("configure answer provider: %w", err)
	}

	if err := s.Init(context.Background()); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	idx := indexer.New(p, s, root)
	idx.SetEmbedder(embedder)
	sr := search.New(s, root)
	g := graph.New(s)

	return &Engine{
		root:     root,
		dbPath:   storeLocation,
		store:    s,
		embedder: embedder,
		answerer: answerer,
		options:  opts,
		parser:   p,
		indexer:  idx,
		search:   sr,
		graph:    g,
	}, nil
}

func (e *Engine) Index(ctx context.Context, verbose bool) (*api.IndexStats, error) {
	stats, err := e.indexer.IndexAll(ctx, verbose)
	e.recordRefresh(stats, err, "manual-full")
	return stats, err
}

func (e *Engine) IndexIncremental(ctx context.Context, verbose bool) (*api.IndexStats, error) {
	stats, err := e.indexer.IndexIncremental(ctx, verbose)
	e.recordRefresh(stats, err, "manual-incremental")
	return stats, err
}

func (e *Engine) SearchSymbols(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error) {
	return e.search.SearchSymbols(ctx, query, kind, limit)
}

func (e *Engine) SearchSymbolsHybrid(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error) {
	return e.search.SearchSymbolsHybrid(ctx, query, kind, limit)
}

func (e *Engine) FindDef(ctx context.Context, name string) ([]api.Symbol, error) {
	return e.search.FindDefinition(ctx, name)
}

func (e *Engine) FindRefs(ctx context.Context, name string) ([]api.Symbol, error) {
	return e.search.FindReferences(ctx, name)
}

func (e *Engine) FileSymbols(ctx context.Context, path string) ([]api.Symbol, error) {
	return e.search.GetFileSymbols(ctx, path)
}

func (e *Engine) SearchText(ctx context.Context, query string, filePattern string, limit int) ([]api.SearchMatch, error) {
	return e.search.SearchText(ctx, query, filePattern, limit)
}

func (e *Engine) Imports(ctx context.Context, file string) ([]api.ImportEdge, error) {
	return e.store.GetImports(ctx, file)
}

func (e *Engine) Importers(ctx context.Context, source string) ([]api.ImportEdge, error) {
	return e.store.GetImporters(ctx, source)
}

func (e *Engine) Callers(ctx context.Context, name string) ([]api.CallEdge, error) {
	return e.store.GetCallers(ctx, strings.TrimSpace(name))
}

func (e *Engine) Callees(ctx context.Context, name string) ([]api.CallEdge, error) {
	return e.store.GetCallees(ctx, strings.TrimSpace(name))
}

func (e *Engine) Routes(ctx context.Context, query string) ([]api.Route, error) {
	return e.store.ListRoutes(ctx, strings.TrimSpace(query))
}

func (e *Engine) DocsFor(ctx context.Context, query string) (*api.DocReference, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("docs-for requires a non-empty query")
	}
	docs, err := e.store.ListDocuments(ctx)
	if err != nil {
		return nil, err
	}
	var result []api.DocumentLink
	for _, doc := range docs {
		links, err := e.store.GetDocumentLinks(ctx, doc.Path)
		if err != nil {
			continue
		}
		for _, link := range links {
			if docLinkMatches(query, link) {
				link.DocumentPath = doc.Path
				result = append(result, link)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].DocumentPath != result[j].DocumentPath {
			return result[i].DocumentPath < result[j].DocumentPath
		}
		return result[i].Line < result[j].Line
	})
	return &api.DocReference{Query: query, Links: result}, nil
}

func (e *Engine) DocDrift(ctx context.Context) (*api.DocDriftReport, error) {
	docs, err := e.store.ListDocuments(ctx)
	if err != nil {
		return nil, err
	}
	var broken []api.DocDriftItem
	total := 0
	for _, doc := range docs {
		links, err := e.store.GetDocumentLinks(ctx, doc.Path)
		if err != nil {
			continue
		}
		for _, link := range links {
			total++
			if reason := e.docLinkDriftReason(ctx, link); reason != "" {
				broken = append(broken, api.DocDriftItem{DocumentPath: doc.Path, TargetType: link.TargetType, TargetValue: link.TargetValue, Line: link.Line, SectionTitle: link.SectionTitle, SectionSlug: link.SectionSlug, SectionLine: link.SectionLine, Evidence: link.Evidence, Reason: reason})
			}
		}
	}
	summary := fmt.Sprintf("Checked %d document links; %d broken references found", total, len(broken))
	return &api.DocDriftReport{TotalLinks: total, Broken: broken, Summary: summary}, nil
}

func (e *Engine) DocCoverage(ctx context.Context) (*api.DocCoverageReport, error) {
	routes, err := e.store.ListRoutes(ctx, "")
	if err != nil {
		return nil, err
	}
	symbols, err := e.publicSymbols(ctx)
	if err != nil {
		return nil, err
	}
	documentedRoutes, err := e.documentedRouteTargets(ctx)
	if err != nil {
		return nil, err
	}
	documentedSymbols, err := e.documentedSymbolTargets(ctx)
	if err != nil {
		return nil, err
	}

	var missingRoutes []api.Route
	documentedRouteCount := 0
	for _, route := range routes {
		if routeDocumented(route, documentedRoutes) {
			documentedRouteCount++
			continue
		}
		missingRoutes = append(missingRoutes, route)
	}
	routeCoverage := 0.0
	if len(routes) > 0 {
		routeCoverage = float64(documentedRouteCount) * 100 / float64(len(routes))
	}

	var missingSymbols []api.Symbol
	documentedSymbolCount := 0
	for _, sym := range symbols {
		if symbolDocumented(sym, documentedSymbols) {
			documentedSymbolCount++
			continue
		}
		missingSymbols = append(missingSymbols, sym)
	}
	symbolCoverage := 0.0
	if len(symbols) > 0 {
		symbolCoverage = float64(documentedSymbolCount) * 100 / float64(len(symbols))
	}

	summary := fmt.Sprintf("Route doc coverage %.1f%% (%d/%d documented); symbol doc coverage %.1f%% (%d/%d documented)", routeCoverage, documentedRouteCount, len(routes), symbolCoverage, documentedSymbolCount, len(symbols))
	return &api.DocCoverageReport{TotalRoutes: len(routes), DocumentedRoutes: documentedRouteCount, MissingRoutes: missingRoutes, RouteCoveragePercent: routeCoverage, TotalSymbols: len(symbols), DocumentedSymbols: documentedSymbolCount, MissingSymbols: missingSymbols, SymbolCoveragePercent: symbolCoverage, Summary: summary}, nil
}

func (e *Engine) publicSymbols(ctx context.Context) ([]api.Symbol, error) {
	files, err := e.store.ListFiles(ctx, nil)
	if err != nil {
		return nil, err
	}
	var result []api.Symbol
	for _, file := range files {
		symbols, err := e.store.GetFileSymbols(ctx, file.Path)
		if err != nil {
			continue
		}
		for _, sym := range symbols {
			if isDocCoverableSymbol(sym) {
				result = append(result, sym)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].FilePath != result[j].FilePath {
			return result[i].FilePath < result[j].FilePath
		}
		return result[i].Line < result[j].Line
	})
	return result, nil
}

func isDocCoverableSymbol(sym api.Symbol) bool {
	switch sym.Kind {
	case api.Function, api.Method, api.Class, api.Type, api.Interface:
	default:
		return false
	}
	name := strings.TrimSpace(sym.Name)
	if name == "" || name == "main" || name == "init" || strings.Contains(sym.FilePath, "_test.") {
		return false
	}
	if strings.HasPrefix(name, "_") {
		return false
	}
	first := []rune(name)[0]
	return first >= 'A' && first <= 'Z'
}

func (e *Engine) documentedRouteTargets(ctx context.Context) (map[string]map[string]bool, error) {
	docs, err := e.store.ListDocuments(ctx)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]map[string]bool)
	for _, doc := range docs {
		links, err := e.store.GetDocumentLinks(ctx, doc.Path)
		if err != nil {
			continue
		}
		for _, link := range links {
			if link.TargetType != "route" {
				continue
			}
			method, path := parseDocumentRouteTarget(link.TargetValue)
			if path == "" {
				continue
			}
			if targets[path] == nil {
				targets[path] = make(map[string]bool)
			}
			if method == "" {
				method = "*"
			}
			targets[path][method] = true
		}
	}
	return targets, nil
}

func routeDocumented(route api.Route, documented map[string]map[string]bool) bool {
	methods := documented[route.Path]
	if len(methods) == 0 {
		return false
	}
	method := strings.ToUpper(strings.TrimSpace(route.Method))
	if method == "" {
		return true
	}
	return methods["*"] || methods[method]
}

func (e *Engine) documentedSymbolTargets(ctx context.Context) (map[string]bool, error) {
	docs, err := e.store.ListDocuments(ctx)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]bool)
	for _, doc := range docs {
		links, err := e.store.GetDocumentLinks(ctx, doc.Path)
		if err != nil {
			continue
		}
		for _, link := range links {
			if link.TargetType != "symbol" {
				continue
			}
			name := normalizeSymbolCoverageName(link.TargetValue)
			if name != "" {
				targets[name] = true
			}
		}
	}
	return targets, nil
}

func symbolDocumented(sym api.Symbol, documented map[string]bool) bool {
	name := normalizeSymbolCoverageName(sym.Name)
	if name == "" {
		return false
	}
	if documented[name] {
		return true
	}
	if sym.Parent != "" && documented[normalizeSymbolCoverageName(sym.Parent+"."+sym.Name)] {
		return true
	}
	return false
}

func normalizeSymbolCoverageName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "` \t\n\r()[]{}.,;:")
	if idx := strings.LastIndex(name, "."); idx >= 0 && idx < len(name)-1 {
		name = name[idx+1:]
	}
	return name
}

func docLinkMatches(query string, link api.DocumentLink) bool {
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(link.TargetValue), q) || strings.Contains(strings.ToLower(link.Evidence), q) || strings.EqualFold(link.TargetType+":"+link.TargetValue, query)
}

func (e *Engine) docLinkDriftReason(ctx context.Context, link api.DocumentLink) string {
	switch link.TargetType {
	case "file":
		if f, err := e.store.GetFile(ctx, link.TargetValue); err == nil && f != nil {
			return ""
		}
		if d, err := e.store.GetDocument(ctx, link.TargetValue); err == nil && d != nil {
			return ""
		}
		if _, err := os.Stat(filepath.Join(e.root, link.TargetValue)); err == nil {
			return ""
		}
		return "referenced file is not indexed or present"
	case "symbol":
		if defs, err := e.search.FindDefinition(ctx, link.TargetValue); err == nil && len(defs) > 0 {
			return ""
		}
		if matches, err := e.search.SearchSymbols(ctx, link.TargetValue, nil, 1); err == nil && len(matches) > 0 {
			return ""
		}
		return "referenced symbol was not found"
	case "module":
		files, err := e.store.ListFiles(ctx, nil)
		if err != nil {
			return "could not check module"
		}
		prefix := strings.TrimSuffix(link.TargetValue, "/") + "/"
		for _, f := range files {
			if f.Path == link.TargetValue || strings.HasPrefix(f.Path, prefix) {
				return ""
			}
		}
		return "referenced module has no indexed files"
	case "route":
		return e.routeLinkDriftReason(ctx, link.TargetValue)
	default:
		return "unknown document link target type"
	}
}

func (e *Engine) routeLinkDriftReason(ctx context.Context, target string) string {
	method, path := parseDocumentRouteTarget(target)
	if path == "" {
		return "referenced route is malformed"
	}
	routes, err := e.store.ListRoutes(ctx, path)
	if err != nil {
		return "could not check route"
	}
	hasPath := false
	for _, route := range routes {
		if route.Path != path {
			continue
		}
		hasPath = true
		indexedMethod := strings.ToUpper(strings.TrimSpace(route.Method))
		if method == "" || method == "*" || indexedMethod == "" || indexedMethod == method {
			return ""
		}
	}
	if hasPath {
		return "referenced route method was not found"
	}
	return "referenced route was not found"
}

func parseDocumentRouteTarget(target string) (method, path string) {
	fields := strings.Fields(strings.TrimSpace(target))
	if len(fields) == 0 {
		return "", ""
	}
	if len(fields) == 1 {
		return "", strings.TrimSpace(fields[0])
	}
	return strings.ToUpper(strings.TrimSpace(fields[0])), strings.TrimSpace(fields[1])
}

func (e *Engine) BuildGraph(ctx context.Context) error {
	return e.graph.Build(ctx)
}

func (e *Engine) GraphDeps(file string, depth int) []string {
	return e.graph.Dependencies(file, depth)
}

func (e *Engine) GraphRelated(file string, topN int) []string {
	return e.graph.Related(file, topN)
}

func (e *Engine) ExportGraph(ctx context.Context, focus string) (*api.GraphExport, error) {
	focus = strings.TrimSpace(focus)
	focusSet := make(map[string]bool)
	if focus != "" {
		resolvedFiles, err := e.resolveGraphFocusFiles(ctx, focus)
		if err != nil {
			return nil, err
		}
		for _, file := range resolvedFiles {
			focusSet[file] = true
		}
	}
	return e.exportGraphWithFocusSet(ctx, focus, focusSet)
}

func (e *Engine) GraphSubgraph(ctx context.Context, target string, depth int) (*api.GraphSubgraphResult, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("graph subgraph requires a non-empty target")
	}
	if depth <= 0 {
		depth = 1
	}
	if err := e.graph.Build(ctx); err != nil {
		return nil, err
	}

	resolvedFile, resolution, err := e.resolveGraphNavigationTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	files := e.graph.SubgraphFiles(resolvedFile, depth)
	focusSet := make(map[string]bool, len(files))
	for _, file := range files {
		focusSet[file] = true
	}
	graphExport, err := e.exportGraphWithFocusSet(ctx, target, focusSet)
	if err != nil {
		return nil, err
	}
	return &api.GraphSubgraphResult{
		Target:       target,
		ResolvedFile: resolvedFile,
		Resolution:   resolution,
		Depth:        depth,
		Graph:        graphExport,
		Files:        files,
		Summary:      fmt.Sprintf("Exported subgraph for %s across %d files at depth %d", resolvedFile, len(files), depth),
	}, nil
}

func (e *Engine) TraverseGraph(ctx context.Context, query store.GraphTraversalQuery) (*store.GraphTraversalResult, error) {
	traverser, ok := e.store.(store.GraphTraverser)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, store.CapabilityGraphTraversal)
	}
	return traverser.TraverseGraph(ctx, query)
}

func (e *Engine) Embed(ctx context.Context, inputs []store.EmbeddingInput) ([]store.EmbeddingVector, error) {
	if e.embedder == nil {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, store.CapabilityEmbedding)
	}
	return e.embedder.Embed(ctx, inputs)
}

type AnswerOptions struct {
	Query               string                `json:"query,omitempty"`
	Question            string                `json:"question,omitempty"`
	Profile             string                `json:"profile,omitempty"`
	Template            string                `json:"template,omitempty"`
	SystemPrompt        string                `json:"system_prompt,omitempty"`
	Messages            []store.AnswerMessage `json:"messages,omitempty"`
	Filter              store.SearchFilter    `json:"filter,omitempty"`
	Limit               int                   `json:"limit,omitempty"`
	TextWeight          float64               `json:"text_weight,omitempty"`
	VectorWeight        float64               `json:"vector_weight,omitempty"`
	GraphWeight         float64               `json:"graph_weight,omitempty"`
	ExpandFrom          []store.TargetRef     `json:"expand_from,omitempty"`
	ExpandMaxDepth      int                   `json:"expand_max_depth,omitempty"`
	ContextOnly         bool                  `json:"context_only,omitempty"`
	RequireCitations    bool                  `json:"require_citations,omitempty"`
	MinCitationCoverage float64               `json:"min_citation_coverage,omitempty"`
	MaxTokens           int                   `json:"max_tokens,omitempty"`
	Temperature         *float64              `json:"temperature,omitempty"`
}

type AnswerSource struct {
	Citation string             `json:"citation"`
	Title    string             `json:"title,omitempty"`
	Target   store.TargetRef    `json:"target,omitempty"`
	Source   store.SearchSource `json:"source,omitempty"`
	Score    float64            `json:"score,omitempty"`
	Metadata map[string]string  `json:"metadata,omitempty"`
}

type AnswerResult struct {
	Question    string                `json:"question"`
	Answer      string                `json:"answer,omitempty"`
	Provider    string                `json:"provider,omitempty"`
	Model       string                `json:"model,omitempty"`
	Profile     string                `json:"profile,omitempty"`
	Template    string                `json:"template,omitempty"`
	ContextOnly bool                  `json:"context_only,omitempty"`
	Context     []store.AnswerContext `json:"context,omitempty"`
	Sources     []AnswerSource        `json:"sources,omitempty"`
	Hits        []store.SearchHit     `json:"hits,omitempty"`
	Grounding   *AnswerGrounding      `json:"grounding,omitempty"`
	Usage       *store.AnswerUsage    `json:"usage,omitempty"`
	Summary     string                `json:"summary"`
}

type AnswerGrounding struct {
	Required         bool     `json:"required,omitempty"`
	MinCoverage      float64  `json:"min_coverage,omitempty"`
	SourceCount      int      `json:"source_count"`
	HasCitations     bool     `json:"has_citations"`
	Grounded         bool     `json:"grounded"`
	Passed           bool     `json:"passed"`
	Coverage         float64  `json:"coverage,omitempty"`
	ValidCitations   []string `json:"valid_citations,omitempty"`
	MissingCitations []string `json:"missing_citations,omitempty"`
	UncitedCitations []string `json:"uncited_citations,omitempty"`
	Summary          string   `json:"summary"`
}

type AnswerTemplateInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt,omitempty"`
}

type AnswerProfileInfo struct {
	Name                string             `json:"name"`
	Description         string             `json:"description"`
	Template            string             `json:"template,omitempty"`
	Filter              store.SearchFilter `json:"filter,omitempty"`
	Limit               int                `json:"limit,omitempty"`
	TextWeight          float64            `json:"text_weight,omitempty"`
	VectorWeight        float64            `json:"vector_weight,omitempty"`
	GraphWeight         float64            `json:"graph_weight,omitempty"`
	ExpandMaxDepth      int                `json:"expand_max_depth,omitempty"`
	RequireCitations    bool               `json:"require_citations,omitempty"`
	MinCitationCoverage float64            `json:"min_citation_coverage,omitempty"`
}

func FormatAnswerMarkdown(result *AnswerResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Answer\n")
	if question := strings.TrimSpace(result.Question); question != "" {
		fmt.Fprintf(&b, "\n**Question:** %s\n", question)
	}
	if summary := strings.TrimSpace(result.Summary); summary != "" {
		fmt.Fprintf(&b, "\n> %s\n", summary)
	}
	if answer := strings.TrimSpace(result.Answer); answer != "" {
		fmt.Fprintf(&b, "\n%s\n", answer)
	} else if result.ContextOnly {
		b.WriteString("\n_Context-only: answer provider was not called._\n")
	} else {
		b.WriteString("\n_No answer was generated._\n")
	}

	sources := result.Sources
	if len(sources) == 0 {
		sources = answerSourcesFromContext(result.Context)
	}
	if len(sources) > 0 {
		b.WriteString("\n## Sources\n")
		snippets := answerContextSnippetByCitation(result.Context)
		for i, source := range sources {
			citation := strings.TrimSpace(source.Citation)
			if citation == "" {
				citation = answerCitationLabel(i + 1)
			}
			title := strings.TrimSpace(source.Title)
			if title == "" {
				title = answerTargetLabel(source.Target)
			}
			fmt.Fprintf(&b, "- %s `%s`", citation, title)
			if source.Source != "" {
				fmt.Fprintf(&b, " (%s", source.Source)
				if source.Score != 0 {
					fmt.Fprintf(&b, ", %.4f", source.Score)
				}
				b.WriteString(")")
			} else if source.Score != 0 {
				fmt.Fprintf(&b, " (%.4f)", source.Score)
			}
			if snippet := snippets[citation]; snippet != "" {
				fmt.Fprintf(&b, "\n  - %s", snippet)
			}
			b.WriteByte('\n')
		}
	}
	if result.Grounding != nil {
		b.WriteString("\n## Grounding\n")
		fmt.Fprintf(&b, "- %s\n", result.Grounding.Summary)
		if result.Grounding.MinCoverage > 0 {
			fmt.Fprintf(&b, "- min coverage: %.0f%%\n", result.Grounding.MinCoverage*100)
		}
		if len(result.Grounding.ValidCitations) > 0 {
			fmt.Fprintf(&b, "- cited: %s\n", strings.Join(result.Grounding.ValidCitations, ", "))
		}
		if len(result.Grounding.MissingCitations) > 0 {
			fmt.Fprintf(&b, "- unknown citations: %s\n", strings.Join(result.Grounding.MissingCitations, ", "))
		}
		if len(result.Grounding.UncitedCitations) > 0 {
			fmt.Fprintf(&b, "- uncited sources: %s\n", strings.Join(result.Grounding.UncitedCitations, ", "))
		}
	}
	if result.Usage != nil && (result.Usage.PromptTokens > 0 || result.Usage.CompletionTokens > 0 || result.Usage.TotalTokens > 0) {
		fmt.Fprintf(&b, "\n## Usage\n- prompt_tokens: %d\n- completion_tokens: %d\n- total_tokens: %d\n",
			result.Usage.PromptTokens,
			result.Usage.CompletionTokens,
			result.Usage.TotalTokens,
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (e *Engine) Answer(ctx context.Context, opts AnswerOptions) (*AnswerResult, error) {
	question := answerQuestion(opts)
	if question == "" {
		return nil, fmt.Errorf("answer question is required")
	}
	profile, opts, err := applyAnswerProfile(opts)
	if err != nil {
		return nil, err
	}
	template, systemPrompt, err := resolveAnswerPrompt(opts.Template, opts.SystemPrompt)
	if err != nil {
		return nil, err
	}
	minCitationCoverage, err := normalizeAnswerMinCitationCoverage(opts.MinCitationCoverage)
	if err != nil {
		return nil, err
	}

	contextItems, hits, err := e.AnswerContextWithOptions(ctx, opts)
	if err != nil {
		return nil, err
	}
	result := &AnswerResult{
		Question:    question,
		Profile:     profile,
		Template:    template,
		ContextOnly: opts.ContextOnly,
		Context:     contextItems,
		Sources:     answerSourcesFromContext(contextItems),
		Hits:        hits,
		Summary:     fmt.Sprintf("Prepared %d retrieved context items for question %q", len(contextItems), question),
	}
	if opts.ContextOnly {
		result.Summary += " (context-only; answer provider was not called)"
		return result, nil
	}
	if e.answerer == nil {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, store.CapabilityAnswer)
	}
	info := e.answerer.AnswerModel()
	response, err := e.answerer.Answer(ctx, store.AnswerRequest{
		Question:     question,
		SystemPrompt: systemPrompt,
		Messages:     opts.Messages,
		Context:      contextItems,
		MaxTokens:    opts.MaxTokens,
		Temperature:  opts.Temperature,
	})
	if err != nil {
		return nil, err
	}
	result.Answer = response.Answer
	result.Provider = info.Provider
	result.Model = response.Model
	if result.Model == "" {
		result.Model = info.Model
	}
	result.Grounding = auditAnswerGrounding(result.Answer, contextItems, opts.RequireCitations, minCitationCoverage)
	result.Usage = response.Usage
	result.Summary = fmt.Sprintf("Answered question %q using %d retrieved context items", question, len(contextItems))
	return result, nil
}

func (e *Engine) AnswerContext(ctx context.Context, question string, limit int) ([]store.AnswerContext, []store.SearchHit, error) {
	return e.AnswerContextWithOptions(ctx, AnswerOptions{Question: question, Limit: limit})
}

func (e *Engine) AnswerContextWithOptions(ctx context.Context, opts AnswerOptions) ([]store.AnswerContext, []store.SearchHit, error) {
	question := answerQuestion(opts)
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, nil, fmt.Errorf("answer question is required")
	}
	var err error
	_, opts, err = applyAnswerProfile(opts)
	if err != nil {
		return nil, nil, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 8
	}
	hits, err := e.SearchHybrid(ctx, store.HybridSearchQuery{
		Query:          question,
		Filter:         opts.Filter,
		Limit:          limit,
		TextWeight:     opts.TextWeight,
		VectorWeight:   opts.VectorWeight,
		GraphWeight:    opts.GraphWeight,
		ExpandFrom:     opts.ExpandFrom,
		ExpandMaxDepth: opts.ExpandMaxDepth,
	})
	if err != nil {
		return nil, nil, err
	}
	contextItems := make([]store.AnswerContext, 0, len(hits))
	for i, hit := range hits {
		content := strings.TrimSpace(hit.Evidence)
		if content == "" {
			content = answerContentFromHighlights(hit.Highlights)
		}
		if content == "" {
			content = answerTargetLabel(hit.Target)
		}
		citation := answerCitationLabel(i + 1)
		metadata := map[string]string{"rank": fmt.Sprint(i + 1), "citation": citation}
		for k, v := range hit.Metadata {
			metadata[k] = v
		}
		contextItems = append(contextItems, store.AnswerContext{
			Citation: citation,
			Target:   hit.Target,
			Source:   hit.Source,
			Score:    hit.Score,
			Title:    answerTargetLabel(hit.Target),
			Content:  content,
			Evidence: hit.Evidence,
			Metadata: metadata,
		})
	}
	return contextItems, hits, nil
}

func answerQuestion(opts AnswerOptions) string {
	question := strings.TrimSpace(opts.Question)
	if question == "" {
		question = strings.TrimSpace(opts.Query)
	}
	return question
}

const (
	AnswerTemplateGeneral = "general"
	AnswerTemplateExplain = "explain"
	AnswerTemplateReview  = "review"
	AnswerTemplatePlan    = "plan"
)

const (
	AnswerProfileGeneral            = "general"
	AnswerProfileExplainCode        = "explain-code"
	AnswerProfileReviewChange       = "review-change"
	AnswerProfilePlanImplementation = "plan-implementation"
	AnswerProfileRiskAnalysis       = "risk-analysis"
	AnswerProfileTestPlan           = "test-plan"
)

func AnswerTemplates() []string {
	infos := AnswerTemplateCatalog(false)
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	return names
}

func AnswerTemplateCatalog(includePrompts bool) []AnswerTemplateInfo {
	templates := []string{
		AnswerTemplateGeneral,
		AnswerTemplateExplain,
		AnswerTemplateReview,
		AnswerTemplatePlan,
	}
	infos := make([]AnswerTemplateInfo, 0, len(templates))
	for _, template := range templates {
		description, _ := answerTemplateDescription(template)
		info := AnswerTemplateInfo{Name: template, Description: description}
		if includePrompts {
			info.Prompt, _ = answerTemplatePrompt(template)
		}
		infos = append(infos, info)
	}
	return infos
}

func AnswerProfiles() []string {
	infos := AnswerProfileCatalog()
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	return names
}

func AnswerProfileCatalog() []AnswerProfileInfo {
	return []AnswerProfileInfo{
		{
			Name:        AnswerProfileGeneral,
			Description: "General evidence-grounded answer with balanced default retrieval.",
			Template:    AnswerTemplateGeneral,
			Limit:       8,
		},
		{
			Name:                AnswerProfileExplainCode,
			Description:         "Explain code behavior, location, and interactions with slightly broader context.",
			Template:            AnswerTemplateExplain,
			Filter:              store.SearchFilter{TargetKinds: []store.TargetKind{store.TargetSymbol, store.TargetFile, store.TargetDocument, store.TargetText}},
			Limit:               10,
			TextWeight:          0.50,
			VectorWeight:        0.30,
			GraphWeight:         0.20,
			ExpandMaxDepth:      1,
			MinCitationCoverage: 0.10,
		},
		{
			Name:                AnswerProfileReviewChange,
			Description:         "Review correctness, risk, tests, docs, and follow-up actions using code/doc/route evidence.",
			Template:            AnswerTemplateReview,
			Filter:              store.SearchFilter{TargetKinds: []store.TargetKind{store.TargetSymbol, store.TargetFile, store.TargetRoute, store.TargetDocument}},
			Limit:               12,
			TextWeight:          0.55,
			VectorWeight:        0.20,
			GraphWeight:         0.25,
			ExpandMaxDepth:      2,
			RequireCitations:    true,
			MinCitationCoverage: 0.10,
		},
		{
			Name:                AnswerProfilePlanImplementation,
			Description:         "Plan implementation steps with likely files/symbols, risks, and validation commands.",
			Template:            AnswerTemplatePlan,
			Filter:              store.SearchFilter{TargetKinds: []store.TargetKind{store.TargetSymbol, store.TargetFile, store.TargetRoute, store.TargetDocument}},
			Limit:               12,
			TextWeight:          0.45,
			VectorWeight:        0.25,
			GraphWeight:         0.30,
			ExpandMaxDepth:      2,
			RequireCitations:    true,
			MinCitationCoverage: 0.10,
		},
		{
			Name:                AnswerProfileRiskAnalysis,
			Description:         "Analyze implementation risk using code, route, graph, and documentation evidence.",
			Template:            AnswerTemplateReview,
			Filter:              store.SearchFilter{TargetKinds: []store.TargetKind{store.TargetSymbol, store.TargetFile, store.TargetRoute, store.TargetDocument}},
			Limit:               12,
			TextWeight:          0.45,
			VectorWeight:        0.20,
			GraphWeight:         0.35,
			ExpandMaxDepth:      2,
			RequireCitations:    true,
			MinCitationCoverage: 0.10,
		},
		{
			Name:                AnswerProfileTestPlan,
			Description:         "Produce a test plan from code and documentation evidence.",
			Template:            AnswerTemplatePlan,
			Filter:              store.SearchFilter{TargetKinds: []store.TargetKind{store.TargetSymbol, store.TargetFile, store.TargetDocument}},
			Limit:               10,
			TextWeight:          0.60,
			VectorWeight:        0.20,
			GraphWeight:         0.20,
			ExpandMaxDepth:      1,
			RequireCitations:    true,
			MinCitationCoverage: 0.10,
		},
	}
}

func applyAnswerProfile(opts AnswerOptions) (string, AnswerOptions, error) {
	profileName := strings.TrimSpace(strings.ToLower(strings.ReplaceAll(opts.Profile, "_", "-")))
	if profileName == "" {
		return "", opts, nil
	}
	for _, profile := range AnswerProfileCatalog() {
		if profile.Name != profileName {
			continue
		}
		opts.Profile = profileName
		opts = applyAnswerProfileDefaults(opts, profile)
		return profileName, opts, nil
	}
	return "", opts, fmt.Errorf("unsupported answer profile %q (supported: %s)", profileName, strings.Join(AnswerProfiles(), ", "))
}

func applyAnswerProfileDefaults(opts AnswerOptions, profile AnswerProfileInfo) AnswerOptions {
	if strings.TrimSpace(opts.Template) == "" {
		opts.Template = profile.Template
	}
	if opts.Limit <= 0 && profile.Limit > 0 {
		opts.Limit = profile.Limit
	}
	if opts.TextWeight == 0 && opts.VectorWeight == 0 && opts.GraphWeight == 0 {
		opts.TextWeight = profile.TextWeight
		opts.VectorWeight = profile.VectorWeight
		opts.GraphWeight = profile.GraphWeight
	}
	if opts.ExpandMaxDepth <= 0 && profile.ExpandMaxDepth > 0 {
		opts.ExpandMaxDepth = profile.ExpandMaxDepth
	}
	if len(opts.Filter.TargetKinds) == 0 && len(profile.Filter.TargetKinds) > 0 {
		opts.Filter.TargetKinds = append([]store.TargetKind(nil), profile.Filter.TargetKinds...)
	}
	if strings.TrimSpace(opts.Filter.FilePattern) == "" {
		opts.Filter.FilePattern = profile.Filter.FilePattern
	}
	if len(profile.Filter.Metadata) > 0 {
		if opts.Filter.Metadata == nil {
			opts.Filter.Metadata = map[string]string{}
		}
		for k, v := range profile.Filter.Metadata {
			if _, ok := opts.Filter.Metadata[k]; !ok {
				opts.Filter.Metadata[k] = v
			}
		}
	}
	if !opts.RequireCitations {
		opts.RequireCitations = profile.RequireCitations
	}
	if opts.MinCitationCoverage == 0 && profile.MinCitationCoverage > 0 {
		opts.MinCitationCoverage = profile.MinCitationCoverage
	}
	return opts
}

func resolveAnswerPrompt(template string, systemPrompt string) (string, string, error) {
	systemPrompt = strings.TrimSpace(systemPrompt)
	template = strings.TrimSpace(strings.ToLower(template))
	if template == "" {
		if systemPrompt != "" {
			return "", systemPrompt, nil
		}
		return "", "", nil
	}
	prompt, ok := answerTemplatePrompt(template)
	if !ok {
		return "", "", fmt.Errorf("unsupported answer template %q (supported: %s)", template, strings.Join(AnswerTemplates(), ", "))
	}
	if systemPrompt != "" {
		return template, systemPrompt, nil
	}
	return template, prompt, nil
}

func answerTemplatePrompt(template string) (string, bool) {
	switch template {
	case AnswerTemplateGeneral:
		return "Answer using the supplied code-context evidence. Cite sources with the provided labels such as [1]. If the evidence is insufficient, say what is missing instead of inventing.", true
	case AnswerTemplateExplain:
		return "Explain the relevant code behavior using only the supplied code-context evidence. Organize the response around what it does, where it lives, and how the pieces interact. Cite sources with the provided labels such as [1]. If evidence is missing, state the gap.", true
	case AnswerTemplateReview:
		return "Review the supplied code-context evidence for correctness, risk, test impact, documentation impact, and follow-up actions. Be concrete and cite sources with the provided labels such as [1]. If evidence is insufficient for a claim, say so.", true
	case AnswerTemplatePlan:
		return "Create an implementation plan from the supplied code-context evidence. Include ordered steps, files or symbols likely involved, risks, and validation commands when evidence supports them. Cite sources with the provided labels such as [1].", true
	default:
		return "", false
	}
}

func answerTemplateDescription(template string) (string, bool) {
	switch template {
	case AnswerTemplateGeneral:
		return "General evidence-grounded answer with citation labels.", true
	case AnswerTemplateExplain:
		return "Explain code behavior, location, and interactions using retrieved evidence.", true
	case AnswerTemplateReview:
		return "Review correctness, risk, test impact, documentation impact, and follow-up actions.", true
	case AnswerTemplatePlan:
		return "Create an implementation plan with likely files/symbols, risks, and validation commands.", true
	default:
		return "", false
	}
}

var answerCitationPattern = regexp.MustCompile(`\[(\d+)\]`)

func normalizeAnswerMinCitationCoverage(coverage float64) (float64, error) {
	if coverage < 0 || coverage > 1 {
		return 0, fmt.Errorf("min citation coverage must be between 0 and 1")
	}
	return coverage, nil
}

func auditAnswerGrounding(answer string, contextItems []store.AnswerContext, required bool, minCoverage float64) *AnswerGrounding {
	answer = strings.TrimSpace(answer)
	report := &AnswerGrounding{
		Required:    required || minCoverage > 0,
		MinCoverage: minCoverage,
		SourceCount: len(contextItems),
	}
	known := make(map[string]bool, len(contextItems))
	orderedKnown := make([]string, 0, len(contextItems))
	for i, item := range contextItems {
		citation := strings.TrimSpace(item.Citation)
		if citation == "" {
			citation = answerCitationLabel(i + 1)
		}
		if citation == "" || known[citation] {
			continue
		}
		known[citation] = true
		orderedKnown = append(orderedKnown, citation)
	}

	seen := map[string]bool{}
	for _, match := range answerCitationPattern.FindAllStringSubmatch(answer, -1) {
		if len(match) < 2 {
			continue
		}
		citation := "[" + match[1] + "]"
		if seen[citation] {
			continue
		}
		seen[citation] = true
		report.HasCitations = true
		if known[citation] {
			report.ValidCitations = append(report.ValidCitations, citation)
		} else {
			report.MissingCitations = append(report.MissingCitations, citation)
		}
	}
	for _, citation := range orderedKnown {
		if !seen[citation] {
			report.UncitedCitations = append(report.UncitedCitations, citation)
		}
	}
	if report.SourceCount > 0 {
		report.Coverage = float64(len(report.ValidCitations)) / float64(report.SourceCount)
	}
	report.Grounded = len(report.ValidCitations) > 0 && len(report.MissingCitations) == 0
	report.Passed = report.Grounded && (minCoverage <= 0 || report.Coverage >= minCoverage)
	report.Summary = answerGroundingSummary(report, answer)
	return report
}

func answerGroundingSummary(report *AnswerGrounding, answer string) string {
	switch {
	case strings.TrimSpace(answer) == "":
		return "No answer text was available for citation audit."
	case report.SourceCount == 0 && !report.HasCitations:
		return "No retrieved sources were available for citation audit."
	case !report.HasCitations:
		return "Answer did not cite any retrieved sources."
	case len(report.MissingCitations) > 0:
		return fmt.Sprintf("Answer cited unknown sources: %s.", strings.Join(report.MissingCitations, ", "))
	case report.MinCoverage > 0 && report.Coverage < report.MinCoverage:
		return fmt.Sprintf("Answer cited %.0f%% of retrieved sources, below required %.0f%%.", report.Coverage*100, report.MinCoverage*100)
	case len(report.ValidCitations) > 0:
		return fmt.Sprintf("Answer cited %d of %d retrieved sources.", len(report.ValidCitations), report.SourceCount)
	default:
		return "Answer citation audit found no valid retrieved-source citations."
	}
}

func answerCitationLabel(rank int) string {
	if rank <= 0 {
		rank = 1
	}
	return fmt.Sprintf("[%d]", rank)
}

func answerSourcesFromContext(items []store.AnswerContext) []AnswerSource {
	sources := make([]AnswerSource, 0, len(items))
	for i, item := range items {
		citation := strings.TrimSpace(item.Citation)
		if citation == "" {
			citation = answerCitationLabel(i + 1)
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = answerTargetLabel(item.Target)
		}
		sources = append(sources, AnswerSource{
			Citation: citation,
			Title:    title,
			Target:   item.Target,
			Source:   item.Source,
			Score:    item.Score,
			Metadata: item.Metadata,
		})
	}
	return sources
}

func answerContextSnippetByCitation(items []store.AnswerContext) map[string]string {
	out := map[string]string{}
	for i, item := range items {
		citation := strings.TrimSpace(item.Citation)
		if citation == "" {
			citation = answerCitationLabel(i + 1)
		}
		content := strings.TrimSpace(item.Content)
		if content == "" {
			content = strings.TrimSpace(item.Evidence)
		}
		if content == "" {
			continue
		}
		out[citation] = answerMarkdownSnippet(content, 180)
	}
	return out
}

func answerMarkdownSnippet(content string, max int) string {
	fields := strings.Fields(strings.TrimSpace(content))
	if len(fields) == 0 {
		return ""
	}
	snippet := strings.Join(fields, " ")
	if max > 0 && len(snippet) > max {
		if max <= len("...") {
			return snippet[:max]
		}
		snippet = snippet[:max-len("...")] + "..."
	}
	return snippet
}

func answerContentFromHighlights(highlights []store.SearchHighlight) string {
	if len(highlights) == 0 {
		return ""
	}
	var snippets []string
	for _, highlight := range highlights {
		snippet := strings.TrimSpace(highlight.Snippet)
		if snippet == "" {
			continue
		}
		if highlight.Line > 0 {
			snippet = fmt.Sprintf("%d: %s", highlight.Line, snippet)
		}
		snippets = append(snippets, snippet)
	}
	return strings.Join(snippets, "\n")
}

func answerTargetLabel(target store.TargetRef) string {
	switch {
	case target.Path != "" && target.Name != "" && target.Line > 0:
		return fmt.Sprintf("%s:%d %s", target.Path, target.Line, target.Name)
	case target.Path != "" && target.Line > 0:
		return fmt.Sprintf("%s:%d", target.Path, target.Line)
	case target.Path != "" && target.Name != "":
		return target.Path + " " + target.Name
	case target.Path != "":
		return target.Path
	case target.Method != "" && target.RoutePath != "":
		return strings.TrimSpace(target.Method + " " + target.RoutePath)
	case target.Name != "":
		return target.Name
	case target.Value != "":
		return target.Value
	case target.Kind != "":
		return string(target.Kind)
	default:
		return "unknown"
	}
}

func (e *Engine) SearchVector(ctx context.Context, query store.VectorSearchQuery) ([]store.SearchHit, error) {
	searcher, ok := e.store.(store.VectorSearcher)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, store.CapabilityVectorSearch)
	}
	return searcher.SearchVector(ctx, query)
}

func (e *Engine) SearchVectorText(ctx context.Context, text string, query store.VectorSearchQuery) ([]store.SearchHit, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("vector search query text is required")
	}
	if e.embedder == nil {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, store.CapabilityEmbedding)
	}

	vectors, err := e.embedder.Embed(ctx, []store.EmbeddingInput{{
		ID:   "query",
		Text: text,
		Kind: store.EmbeddingInputQuery,
	}})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 || len(vectors[0].Values) == 0 {
		return nil, fmt.Errorf("embedding provider returned no query vector")
	}

	info := e.embedder.EmbeddingModel()
	vector := vectors[0]
	query.QueryText = text
	query.Vector = vector.Values
	if query.Model == "" {
		query.Model = strings.TrimSpace(vector.Model)
		if query.Model == "" {
			query.Model = strings.TrimSpace(info.Model)
		}
	}
	if query.Dimensions <= 0 {
		query.Dimensions = vector.Dimensions
	}
	if query.Dimensions <= 0 {
		query.Dimensions = info.Dimensions
	}
	if query.Dimensions <= 0 {
		query.Dimensions = len(vector.Values)
	}
	return e.SearchVector(ctx, query)
}

func (e *Engine) SearchHybrid(ctx context.Context, query store.HybridSearchQuery) ([]store.SearchHit, error) {
	if hybrid, ok := e.store.(store.HybridSearcher); ok {
		return hybrid.SearchHybrid(ctx, query)
	}
	return e.searchHybridFallback(ctx, query)
}

func (e *Engine) searchHybridFallback(ctx context.Context, query store.HybridSearchQuery) ([]store.SearchHit, error) {
	query.Query = strings.TrimSpace(query.Query)
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}
	searchLimit := limit * 4
	if searchLimit < 20 {
		searchLimit = 20
	}

	textWeight, vectorWeight, graphWeight := hybridWeights(query)
	candidates := newHybridCandidates()
	hadSearchPath := false

	if query.Query != "" && textWeight > 0 {
		textHits, err := e.hybridTextHits(ctx, query.Query, query.Filter, searchLimit)
		if err != nil {
			return nil, err
		}
		hadSearchPath = true
		candidates.addSourceHits(textHits, store.SearchSourceText, textWeight)
	}

	if vectorWeight > 0 && (len(query.Vector) > 0 || (query.Query != "" && e.embedder != nil)) {
		vectorQuery := store.VectorSearchQuery{
			Vector:     query.Vector,
			QueryText:  query.Query,
			Model:      query.Model,
			Dimensions: query.Dimensions,
			Filter:     query.Filter,
			Limit:      searchLimit,
		}
		var (
			vectorHits []store.SearchHit
			err        error
		)
		if len(query.Vector) > 0 {
			vectorHits, err = e.SearchVector(ctx, vectorQuery)
		} else {
			vectorHits, err = e.SearchVectorText(ctx, query.Query, vectorQuery)
		}
		if err != nil {
			if !(errors.Is(err, ErrCapabilityUnsupported) && query.Query != "") {
				return nil, err
			}
		} else {
			hadSearchPath = true
			candidates.addSourceHits(vectorHits, store.SearchSourceVector, vectorWeight)
		}
	}

	if graphWeight > 0 {
		graphHits, used, err := e.hybridGraphHits(ctx, query, candidates.topTargets(3), searchLimit)
		if err != nil {
			if !errors.Is(err, ErrCapabilityUnsupported) {
				return nil, err
			}
		} else if used {
			hadSearchPath = true
			candidates.addSourceHits(graphHits, store.SearchSourceGraph, graphWeight)
		}
	}

	if !hadSearchPath && query.Query == "" && len(query.Vector) == 0 && len(query.ExpandFrom) == 0 {
		return nil, fmt.Errorf("hybrid search requires query, vector, or expand_from")
	}

	hits := candidates.results()
	if query.Offset > 0 {
		if query.Offset >= len(hits) {
			return nil, nil
		}
		hits = hits[query.Offset:]
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (e *Engine) hybridTextHits(ctx context.Context, query string, filter store.SearchFilter, limit int) ([]store.SearchHit, error) {
	if advanced, ok := e.store.(store.TextSearcher); ok {
		return advanced.SearchText(ctx, store.TextSearchQuery{
			Query:  query,
			Filter: filter,
			Limit:  limit,
		})
	}
	matches, err := e.search.SearchText(ctx, query, filter.FilePattern, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]store.SearchHit, 0, len(matches))
	for _, match := range matches {
		target := store.TargetRef{
			Kind: store.TargetText,
			Path: match.FilePath,
			Line: match.Line,
		}
		if match.Kind != "" {
			target.Kind = store.TargetKind(match.Kind)
		}
		if !hybridTargetFilterAllows(filter, target) {
			continue
		}
		hits = append(hits, store.SearchHit{
			Target:   target,
			Score:    1,
			Source:   store.SearchSourceText,
			Evidence: strings.TrimSpace(match.Content),
			Highlights: []store.SearchHighlight{{
				Line:    match.Line,
				Snippet: strings.TrimSpace(match.Content),
			}},
			Metadata: map[string]string{"backend": "local"},
		})
	}
	return hits, nil
}

func (e *Engine) hybridGraphHits(ctx context.Context, query store.HybridSearchQuery, candidateTargets []store.TargetRef, limit int) ([]store.SearchHit, bool, error) {
	if _, ok := e.store.(store.GraphTraverser); !ok {
		return nil, false, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, store.CapabilityGraphTraversal)
	}
	depth := query.ExpandMaxDepth
	if depth <= 0 {
		depth = 1
	}
	if depth > 2 {
		depth = 2
	}

	starts := make([]store.TargetRef, 0, len(query.ExpandFrom)+len(candidateTargets)+1)
	seen := map[string]struct{}{}
	addStart := func(target store.TargetRef) {
		if target.Kind == "" {
			target = store.ParseTargetRef(hybridTargetDisplay(target))
		}
		if target.Kind == "" {
			return
		}
		key := hybridTargetKey(target)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		starts = append(starts, target)
	}
	for _, target := range query.ExpandFrom {
		addStart(target)
	}
	if len(query.ExpandFrom) == 0 {
		if query.Query != "" {
			addStart(store.TargetRef{Kind: store.TargetText, Value: query.Query})
		}
		for _, target := range candidateTargets {
			addStart(target)
			if len(starts) >= 3 {
				break
			}
		}
	}
	if len(starts) == 0 {
		return nil, false, nil
	}

	hits := make([]store.SearchHit, 0, limit)
	for _, start := range starts {
		result, err := e.TraverseGraph(ctx, store.GraphTraversalQuery{
			Start:        start,
			Direction:    store.GraphOutbound,
			MaxDepth:     depth,
			Limit:        limit,
			Filter:       query.Filter,
			IncludePaths: false,
		})
		if err != nil {
			return nil, true, err
		}
		if result == nil {
			continue
		}
		for _, node := range result.Nodes {
			if node.Root {
				continue
			}
			if !hybridTargetFilterAllows(query.Filter, node.Target) {
				continue
			}
			score := node.Score
			if score <= 0 {
				score = 1 / float64(node.Depth+1)
			}
			hits = append(hits, store.SearchHit{
				Target:   node.Target,
				Score:    score,
				Source:   store.SearchSourceGraph,
				Evidence: fmt.Sprintf("graph expansion from %s", hybridTargetDisplay(start)),
				Metadata: map[string]string{
					"backend":     "graph",
					"graph_start": hybridTargetDisplay(start),
					"graph_depth": fmt.Sprint(node.Depth),
				},
			})
			if len(hits) >= limit {
				return hits, true, nil
			}
		}
	}
	return hits, true, nil
}

func hybridWeights(query store.HybridSearchQuery) (float64, float64, float64) {
	textWeight := query.TextWeight
	vectorWeight := query.VectorWeight
	graphWeight := query.GraphWeight
	if textWeight <= 0 && vectorWeight <= 0 && graphWeight <= 0 {
		return 0.45, 0.45, 0.10
	}
	if textWeight < 0 {
		textWeight = 0
	}
	if vectorWeight < 0 {
		vectorWeight = 0
	}
	if graphWeight < 0 {
		graphWeight = 0
	}
	return textWeight, vectorWeight, graphWeight
}

type hybridCandidates struct {
	entries map[string]*hybridCandidate
	order   int
}

type hybridCandidate struct {
	hit              store.SearchHit
	score            float64
	scores           map[store.SearchSource]float64
	normalizedScores map[store.SearchSource]float64
	contributions    map[store.SearchSource]float64
	weights          map[store.SearchSource]float64
	ranks            map[store.SearchSource]int
	order            int
}

func newHybridCandidates() *hybridCandidates {
	return &hybridCandidates{entries: map[string]*hybridCandidate{}}
}

func (c *hybridCandidates) addSourceHits(hits []store.SearchHit, source store.SearchSource, weight float64) {
	if weight <= 0 {
		return
	}
	maxScore := 0.0
	for _, hit := range hits {
		if !hybridHitHasTarget(hit) {
			continue
		}
		score := hybridSourceRawScore(hit.Score)
		if score > maxScore {
			maxScore = score
		}
	}
	if maxScore <= 0 {
		maxScore = 1
	}
	for i, hit := range hits {
		rawScore := hybridSourceRawScore(hit.Score)
		normalizedScore := rawScore / maxScore
		if normalizedScore < 0 {
			normalizedScore = 0
		}
		c.add(hit, source, rawScore, normalizedScore, weight, i+1)
	}
}

func hybridSourceRawScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score == 0 {
		return 1
	}
	return score
}

func (c *hybridCandidates) add(hit store.SearchHit, source store.SearchSource, rawScore, normalizedScore, weight float64, rank int) {
	if !hybridHitHasTarget(hit) {
		return
	}
	if weight <= 0 {
		return
	}
	if rawScore < 0 {
		rawScore = 0
	}
	if normalizedScore < 0 {
		normalizedScore = 0
	}
	key := hybridTargetKey(hit.Target)
	entry, ok := c.entries[key]
	if !ok {
		entry = &hybridCandidate{
			hit:              hit,
			scores:           map[store.SearchSource]float64{},
			normalizedScores: map[store.SearchSource]float64{},
			contributions:    map[store.SearchSource]float64{},
			weights:          map[store.SearchSource]float64{},
			ranks:            map[store.SearchSource]int{},
			order:            c.order,
		}
		entry.hit.Source = store.SearchSourceHybrid
		if entry.hit.Metadata == nil {
			entry.hit.Metadata = map[string]string{}
		}
		c.order++
		c.entries[key] = entry
	} else {
		entry.hit.Target = mergeHybridTarget(entry.hit.Target, hit.Target)
		if entry.hit.Evidence == "" {
			entry.hit.Evidence = hit.Evidence
		}
		if len(entry.hit.Highlights) == 0 {
			entry.hit.Highlights = hit.Highlights
		}
		if entry.hit.Metadata == nil {
			entry.hit.Metadata = map[string]string{}
		}
		for k, v := range hit.Metadata {
			if _, exists := entry.hit.Metadata[k]; !exists {
				entry.hit.Metadata[k] = v
			}
		}
	}
	contribution := weight * normalizedScore
	if contribution > entry.contributions[source] {
		entry.score += contribution - entry.contributions[source]
		entry.contributions[source] = contribution
		entry.normalizedScores[source] = normalizedScore
		entry.weights[source] = weight
	}
	if rank > 0 && (entry.ranks[source] == 0 || rank < entry.ranks[source]) {
		entry.ranks[source] = rank
	}
	if rawScore > entry.scores[source] {
		entry.scores[source] = rawScore
	}
}

func hybridHitHasTarget(hit store.SearchHit) bool {
	return hit.Target.Kind != "" || hit.Target.Path != "" || hit.Target.Name != "" || hit.Target.Value != "" || hit.Target.RoutePath != ""
}

func (c *hybridCandidates) results() []store.SearchHit {
	items := make([]*hybridCandidate, 0, len(c.entries))
	for _, entry := range c.entries {
		entry.hit.Score = entry.score
		sources := make([]string, 0, len(entry.scores))
		for source, score := range entry.scores {
			if source == "" {
				continue
			}
			sources = append(sources, string(source))
			entry.hit.Metadata["hybrid_"+string(source)+"_score"] = fmt.Sprintf("%.4f", score)
			entry.hit.Metadata["hybrid_"+string(source)+"_normalized_score"] = fmt.Sprintf("%.4f", entry.normalizedScores[source])
			entry.hit.Metadata["hybrid_"+string(source)+"_weight"] = fmt.Sprintf("%.4f", entry.weights[source])
			entry.hit.Metadata["hybrid_"+string(source)+"_contribution"] = fmt.Sprintf("%.4f", entry.contributions[source])
			if rank := entry.ranks[source]; rank > 0 {
				entry.hit.Metadata["hybrid_"+string(source)+"_rank"] = fmt.Sprint(rank)
			}
		}
		sort.Strings(sources)
		entry.hit.Metadata["sources"] = strings.Join(sources, ",")
		entry.hit.Metadata["hybrid_score"] = fmt.Sprintf("%.4f", entry.hit.Score)
		entry.hit.Metadata["hybrid_fusion"] = "weighted_normalized_sum"
		items = append(items, entry)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].hit.Score != items[j].hit.Score {
			return items[i].hit.Score > items[j].hit.Score
		}
		return items[i].order < items[j].order
	})
	out := make([]store.SearchHit, 0, len(items))
	for _, item := range items {
		out = append(out, item.hit)
	}
	return out
}

func (c *hybridCandidates) topTargets(limit int) []store.TargetRef {
	results := c.results()
	if limit <= 0 || limit > len(results) {
		limit = len(results)
	}
	targets := make([]store.TargetRef, 0, limit)
	for i := 0; i < limit; i++ {
		targets = append(targets, results[i].Target)
	}
	return targets
}

func hybridTargetFilterAllows(filter store.SearchFilter, target store.TargetRef) bool {
	if len(filter.TargetKinds) > 0 {
		ok := false
		for _, kind := range filter.TargetKinds {
			if target.Kind == kind {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if filter.FilePattern != "" && target.Path != "" {
		matched, _ := filepath.Match(filter.FilePattern, target.Path)
		if !matched && !strings.Contains(target.Path, filter.FilePattern) {
			return false
		}
	}
	for key, value := range filter.Metadata {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "kind", "target_kind":
			if string(target.Kind) != value {
				return false
			}
		case "type":
			if target.Type != value {
				return false
			}
		case "name":
			if target.Name != value {
				return false
			}
		case "path":
			if target.Path != value {
				return false
			}
		}
	}
	return true
}

func mergeHybridTarget(a, b store.TargetRef) store.TargetRef {
	if a.ProjectID == "" {
		a.ProjectID = b.ProjectID
	}
	if a.Kind == "" {
		a.Kind = b.Kind
	}
	if a.Path == "" {
		a.Path = b.Path
	}
	if a.Name == "" {
		a.Name = b.Name
	}
	if a.Type == "" {
		a.Type = b.Type
	}
	if a.Line == 0 {
		a.Line = b.Line
	}
	if a.EndLine == 0 {
		a.EndLine = b.EndLine
	}
	if a.Method == "" {
		a.Method = b.Method
	}
	if a.RoutePath == "" {
		a.RoutePath = b.RoutePath
	}
	if a.Value == "" {
		a.Value = b.Value
	}
	return a
}

func hybridTargetKey(target store.TargetRef) string {
	return strings.Join([]string{
		string(target.Kind),
		target.ProjectID,
		target.Path,
		target.Name,
		target.Type,
		fmt.Sprint(target.Line),
		target.Method,
		target.RoutePath,
		target.Value,
	}, "\x00")
}

func hybridTargetDisplay(target store.TargetRef) string {
	switch {
	case target.Kind == store.TargetText && target.Value != "":
		return "text:" + target.Value
	case target.Kind == store.TargetRoute && target.RoutePath != "":
		if target.Method != "" {
			return strings.TrimSpace(target.Method + " " + target.RoutePath)
		}
		return target.RoutePath
	case target.Kind == store.TargetSymbol && target.Name != "":
		if target.Path != "" {
			if target.Line > 0 {
				return fmt.Sprintf("symbol:%s@%s:%d", target.Name, target.Path, target.Line)
			}
			return fmt.Sprintf("symbol:%s@%s", target.Name, target.Path)
		}
		return "symbol:" + target.Name
	case target.Path != "":
		if target.Line > 0 {
			return fmt.Sprintf("%s:%d", target.Path, target.Line)
		}
		return target.Path
	case target.Name != "":
		return target.Name
	case target.Value != "":
		return target.Value
	default:
		return ""
	}
}

func (e *Engine) bestEffortHybridSearch(ctx context.Context, query string, limit int, expandFrom []store.TargetRef) []store.SearchHit {
	query = strings.TrimSpace(query)
	if query == "" && len(expandFrom) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	hits, err := e.SearchHybrid(ctx, store.HybridSearchQuery{
		Query:          query,
		Limit:          limit,
		ExpandFrom:     expandFrom,
		ExpandMaxDepth: 1,
	})
	if err != nil {
		return nil
	}
	return hits
}

func (e *Engine) bestEffortGraphTraversal(ctx context.Context, query store.GraphTraversalQuery) *store.GraphTraversalResult {
	result, err := e.TraverseGraph(ctx, query)
	if err != nil {
		return nil
	}
	if result == nil || len(result.Edges) == 0 {
		return nil
	}
	return result
}

func (e *Engine) graphTraversalForFile(ctx context.Context, filePath string, depth int) *store.GraphTraversalResult {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return nil
	}
	return e.bestEffortGraphTraversal(ctx, store.GraphTraversalQuery{
		Start:        store.TargetRef{Kind: store.TargetFile, Path: filePath},
		EdgeKinds:    []store.GraphEdgeKind{store.GraphEdgeKind("code"), store.GraphEdgeKind("docs")},
		Direction:    store.GraphBoth,
		MaxDepth:     graphTraversalDepth(depth),
		Limit:        50,
		IncludePaths: true,
	})
}

func (e *Engine) graphTraversalForSymbol(ctx context.Context, sym api.Symbol, depth int) *store.GraphTraversalResult {
	name := strings.TrimSpace(sym.Name)
	if name == "" {
		return nil
	}
	return e.bestEffortGraphTraversal(ctx, store.GraphTraversalQuery{
		Start:        symbolTargetRef(sym),
		EdgeKinds:    []store.GraphEdgeKind{store.GraphEdgeCalls, store.GraphEdgeRoutes, store.GraphEdgeDocuments, store.GraphEdgeReferences},
		Direction:    store.GraphBoth,
		MaxDepth:     graphTraversalDepth(depth),
		Limit:        50,
		IncludePaths: true,
	})
}

func symbolTargetRef(sym api.Symbol) store.TargetRef {
	return store.TargetRef{
		Kind:    store.TargetSymbol,
		Path:    sym.FilePath,
		Name:    strings.TrimSpace(sym.Name),
		Type:    string(sym.Kind),
		Line:    sym.Line,
		EndLine: sym.EndLine,
	}
}

func (e *Engine) graphTraversalForRoute(ctx context.Context, route api.Route, depth int) *store.GraphTraversalResult {
	if strings.TrimSpace(route.Path) == "" && strings.TrimSpace(route.Handler) == "" {
		return nil
	}
	return e.bestEffortGraphTraversal(ctx, store.GraphTraversalQuery{
		Start: store.TargetRef{
			Kind:      store.TargetRoute,
			Path:      route.FilePath,
			Name:      route.Handler,
			Type:      route.Framework,
			Line:      route.Line,
			Method:    route.Method,
			RoutePath: route.Path,
		},
		EdgeKinds:    []store.GraphEdgeKind{store.GraphEdgeKind("entrypoints"), store.GraphEdgeKind("docs")},
		Direction:    store.GraphBoth,
		MaxDepth:     graphTraversalDepth(depth),
		Limit:        50,
		IncludePaths: true,
	})
}

func graphTraversalDepth(depth int) int {
	if depth <= 0 {
		return 2
	}
	if depth > 3 {
		return 3
	}
	return depth
}

func (e *Engine) exportGraphWithFocusSet(ctx context.Context, focus string, focusSet map[string]bool) (*api.GraphExport, error) {
	files, err := e.store.ListFiles(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := e.graph.Build(ctx); err != nil {
		return nil, err
	}

	nodeMap := make(map[string]api.GraphNode)
	edgeMap := make(map[string]api.GraphEdge)
	includedFiles := 0
	symbolCount := 0
	importEdgeCount := 0

	fileByPath := make(map[string]*api.FileInfo, len(files))
	packageNamesByFile := make(map[string][]string, len(files))
	filesByPackage := make(map[string][]string)
	filesByModule := make(map[string][]string)
	moduleSet := make(map[string]bool)
	packageSet := make(map[string]bool)

	for _, f := range files {
		fileByPath[f.Path] = f
		modulePath := graphModulePath(f.Path)
		moduleSet[modulePath] = true
		filesByModule[modulePath] = append(filesByModule[modulePath], f.Path)

		syms, err := e.store.GetFileSymbols(ctx, f.Path)
		if err != nil {
			return nil, err
		}
		for _, sym := range syms {
			if sym.Kind != api.Package && sym.Kind != api.Module {
				continue
			}
			packageKey := graphPackageKey(sym.Name, modulePath)
			if !containsString(packageNamesByFile[f.Path], sym.Name) {
				packageNamesByFile[f.Path] = append(packageNamesByFile[f.Path], sym.Name)
			}
			packageSet[packageKey] = true
			filesByPackage[packageKey] = append(filesByPackage[packageKey], f.Path)
		}
	}

	for modulePath := range moduleSet {
		moduleNodeID := graphModuleNodeID(modulePath)
		label := modulePath
		if modulePath == "." {
			label = "root"
		}
		nodeMap[moduleNodeID] = api.GraphNode{
			ID:    moduleNodeID,
			Type:  "module",
			Label: label,
			Name:  label,
			Kind:  string(api.Module),
		}
	}

	for packageKey := range packageSet {
		packageName, packageModule := splitGraphPackageKey(packageKey)
		packageNodeID := graphPackageNodeID(packageKey)
		nodeMap[packageNodeID] = api.GraphNode{
			ID:    packageNodeID,
			Type:  "package",
			Label: packageName,
			Name:  packageName,
			Kind:  string(api.Package),
		}
		moduleNodeID := graphModuleNodeID(packageModule)
		edgeKey := packageNodeID + "->" + moduleNodeID + "#belongs_to"
		edgeMap[edgeKey] = api.GraphEdge{
			Source:     packageNodeID,
			Target:     moduleNodeID,
			Type:       "belongs_to",
			Evidence:   fmt.Sprintf("package %s grouped under module %s", packageName, packageModule),
			Confidence: "INFERRED",
		}
	}

	internalImportTargets := buildInternalImportTargets(fileByPath, packageNamesByFile, filesByPackage, filesByModule)

	for _, f := range files {
		if len(focusSet) > 0 && !focusSet[f.Path] {
			continue
		}
		includedFiles++

		fileNodeID := "file:" + f.Path
		nodeMap[fileNodeID] = api.GraphNode{
			ID:       fileNodeID,
			Type:     "file",
			Label:    f.Path,
			FilePath: f.Path,
			Language: f.Language,
		}

		modulePath := graphModulePath(f.Path)
		moduleNodeID := graphModuleNodeID(modulePath)
		edgeMap[fileNodeID+"->"+moduleNodeID+"#belongs_to"] = api.GraphEdge{
			Source:     fileNodeID,
			Target:     moduleNodeID,
			Type:       "belongs_to",
			Evidence:   fmt.Sprintf("%s is stored under %s", f.Path, modulePath),
			Confidence: "EXTRACTED",
		}

		for _, packageName := range packageNamesByFile[f.Path] {
			packageNodeID := graphPackageNodeID(graphPackageKey(packageName, modulePath))
			edgeMap[fileNodeID+"->"+packageNodeID+"#declares_package"] = api.GraphEdge{
				Source:     fileNodeID,
				Target:     packageNodeID,
				Type:       "declares_package",
				Evidence:   fmt.Sprintf("%s declares package %s", f.Path, packageName),
				Confidence: "EXTRACTED",
			}
		}

		imports, err := e.store.GetImports(ctx, f.Path)
		if err != nil {
			return nil, err
		}
		for _, imp := range imports {
			importNodeID := "import:" + imp.ToSource
			if _, ok := nodeMap[importNodeID]; !ok {
				nodeMap[importNodeID] = api.GraphNode{
					ID:    importNodeID,
					Type:  "import",
					Label: imp.ToSource,
					Name:  imp.ToSource,
					Kind:  string(api.Import),
				}
			}
			edgeKey := fileNodeID + "->" + importNodeID + "#imports#" + fmt.Sprintf("%d", imp.Line)
			edgeMap[edgeKey] = api.GraphEdge{
				Source:     fileNodeID,
				Target:     importNodeID,
				Type:       "imports",
				Evidence:   fmt.Sprintf("%s:%d", imp.FromFile, imp.Line),
				Confidence: "EXTRACTED",
				Line:       imp.Line,
			}
			for _, targetFile := range internalImportTargets[normalizeImportSource(imp.ToSource)] {
				if targetFile == f.Path {
					continue
				}
				if len(focusSet) > 0 && !focusSet[targetFile] {
					continue
				}
				resolvedNodeID := "file:" + targetFile
				edgeMap[importNodeID+"->"+resolvedNodeID+"#resolves_to"] = api.GraphEdge{
					Source:     importNodeID,
					Target:     resolvedNodeID,
					Type:       "resolves_to",
					Evidence:   fmt.Sprintf("%s matches indexed file %s", imp.ToSource, targetFile),
					Confidence: importResolutionConfidence(imp.ToSource, targetFile),
				}
			}
			importEdgeCount++
		}

		syms, err := e.store.GetFileSymbols(ctx, f.Path)
		if err != nil {
			return nil, err
		}
		symbolNodeByName := make(map[string]string)
		for _, sym := range syms {
			symbolNodeID := fmt.Sprintf("symbol:%s:%s:%d", sym.FilePath, sym.Name, sym.Line)
			if _, exists := symbolNodeByName[sym.Name]; !exists {
				symbolNodeByName[sym.Name] = symbolNodeID
			}
			nodeMap[symbolNodeID] = api.GraphNode{
				ID:       symbolNodeID,
				Type:     "symbol",
				Label:    sym.Name,
				FilePath: sym.FilePath,
				Name:     sym.Name,
				Kind:     string(sym.Kind),
				Language: f.Language,
				Line:     sym.Line,
			}
			edgeKey := fileNodeID + "->" + symbolNodeID + "#defines"
			edgeMap[edgeKey] = api.GraphEdge{
				Source:     fileNodeID,
				Target:     symbolNodeID,
				Type:       "defines",
				Evidence:   fmt.Sprintf("%s:%d", sym.FilePath, sym.Line),
				Confidence: "EXTRACTED",
				Line:       sym.Line,
			}
			edgeMap[symbolNodeID+"->"+fileNodeID+"#belongs_to"] = api.GraphEdge{
				Source:     symbolNodeID,
				Target:     fileNodeID,
				Type:       "belongs_to",
				Evidence:   fmt.Sprintf("%s is defined in %s", sym.Name, sym.FilePath),
				Confidence: "EXTRACTED",
				Line:       sym.Line,
			}
			if moduleNodeID != "" {
				edgeMap[symbolNodeID+"->"+moduleNodeID+"#belongs_to_module"] = api.GraphEdge{
					Source:     symbolNodeID,
					Target:     moduleNodeID,
					Type:       "belongs_to",
					Evidence:   fmt.Sprintf("%s is part of module %s", sym.Name, modulePath),
					Confidence: "INFERRED",
					Line:       sym.Line,
				}
			}
			if sym.Kind == api.Package || sym.Kind == api.Module {
				packageNodeID := graphPackageNodeID(graphPackageKey(sym.Name, modulePath))
				edgeMap[symbolNodeID+"->"+packageNodeID+"#represents"] = api.GraphEdge{
					Source:     symbolNodeID,
					Target:     packageNodeID,
					Type:       "represents",
					Evidence:   fmt.Sprintf("%s declares package/module %s", sym.FilePath, sym.Name),
					Confidence: "EXTRACTED",
					Line:       sym.Line,
				}
			}
			symbolCount++
		}

		for _, sym := range syms {
			if sym.Kind != api.Function && sym.Kind != api.Method {
				continue
			}
			fromNodeID := symbolNodeByName[sym.Name]
			if fromNodeID == "" {
				continue
			}
			calls, err := e.store.GetCallees(ctx, sym.Name)
			if err != nil {
				continue
			}
			for _, call := range calls {
				if call.FromFile != f.Path {
					continue
				}
				targetNodeID := symbolNodeByName[call.ToName]
				if targetNodeID == "" {
					targetNodeID = "call:" + call.ToName
					if _, ok := nodeMap[targetNodeID]; !ok {
						nodeMap[targetNodeID] = api.GraphNode{ID: targetNodeID, Type: "symbol", Label: call.ToName, Name: call.ToName, Kind: "call-target", Language: f.Language, Line: call.Line}
					}
				}
				edgeMap[fmt.Sprintf("%s->%s#calls#%d", fromNodeID, targetNodeID, call.Line)] = api.GraphEdge{Source: fromNodeID, Target: targetNodeID, Type: "calls", Evidence: fmt.Sprintf("%s:%d", call.FromFile, call.Line), Confidence: call.Confidence, Line: call.Line}
			}
		}

		routes, err := e.store.ListRoutes(ctx, f.Path)
		if err == nil {
			for _, route := range routes {
				if route.FilePath != f.Path {
					continue
				}
				routeNodeID := fmt.Sprintf("route:%s:%s:%s:%d", route.Method, route.Path, route.FilePath, route.Line)
				label := strings.TrimSpace(route.Method + " " + route.Path)
				if label == "" {
					label = route.Path
				}
				nodeMap[routeNodeID] = api.GraphNode{ID: routeNodeID, Type: "route", Label: label, FilePath: route.FilePath, Name: route.Path, Kind: route.Framework, Language: f.Language, Line: route.Line}
				edgeMap[fileNodeID+"->"+routeNodeID+"#declares_route"] = api.GraphEdge{Source: fileNodeID, Target: routeNodeID, Type: "declares_route", Evidence: fmt.Sprintf("%s:%d", route.FilePath, route.Line), Confidence: route.Confidence, Line: route.Line}
				if route.Handler != "" {
					handlerNodeID := "handler:" + route.Handler
					if _, ok := nodeMap[handlerNodeID]; !ok {
						nodeMap[handlerNodeID] = api.GraphNode{ID: handlerNodeID, Type: "symbol", Label: route.Handler, FilePath: route.FilePath, Name: route.Handler, Kind: "handler", Language: f.Language, Line: route.Line}
					}
					edgeMap[routeNodeID+"->"+handlerNodeID+"#handles_route"] = api.GraphEdge{Source: routeNodeID, Target: handlerNodeID, Type: "handles_route", Evidence: route.Framework, Confidence: route.Confidence, Line: route.Line}
				}
			}
		}
	}

	docs, err := e.store.ListDocuments(ctx)
	if err == nil && len(docs) > 0 {
		for _, doc := range docs {
			docNodeID := "doc:" + doc.Path
			nodeMap[docNodeID] = api.GraphNode{
				ID:       docNodeID,
				Type:     "document",
				Label:    doc.Title,
				FilePath: doc.Path,
				Name:     doc.Title,
				Kind:     "document",
			}

			modulePath := graphModulePath(doc.Path)
			moduleNodeID := graphModuleNodeID(modulePath)
			edgeMap[docNodeID+"->"+moduleNodeID+"#describes"] = api.GraphEdge{
				Source:     docNodeID,
				Target:     moduleNodeID,
				Type:       "describes",
				Evidence:   "document in module directory",
				Confidence: "INFERRED",
			}

			links, err := e.store.GetDocumentLinks(ctx, doc.Path)
			if err == nil {
				for _, link := range links {
					var targetNodeID string
					switch link.TargetType {
					case "file":
						targetNodeID = "file:" + link.TargetValue
					case "symbol":
						targetNodeID = "symbol:" + link.TargetValue
					case "module":
						targetNodeID = "module:" + link.TargetValue
					default:
						targetNodeID = link.TargetType + ":" + link.TargetValue
					}
					edgeMap[docNodeID+"->"+targetNodeID+"#"+link.TargetType] = api.GraphEdge{
						Source:     docNodeID,
						Target:     targetNodeID,
						Type:       "mentions_" + link.TargetType,
						Evidence:   link.Evidence,
						Confidence: fmt.Sprintf("%.1f", link.Confidence),
						Line:       link.Line,
					}
				}
			}
		}
	}

	nodes := make([]api.GraphNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Type != nodes[j].Type {
			return nodes[i].Type < nodes[j].Type
		}
		return nodes[i].ID < nodes[j].ID
	})

	edges := make([]api.GraphEdge, 0, len(edgeMap))
	for _, edge := range edgeMap {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Type != edges[j].Type {
			return edges[i].Type < edges[j].Type
		}
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Line < edges[j].Line
	})

	scope := "entire repository"
	if len(focusSet) > 0 {
		scope = fmt.Sprintf("focus '%s'", focus)
	}

	return &api.GraphExport{
		Version:  graphExportVersion,
		Focus:    focus,
		Nodes:    nodes,
		Edges:    edges,
		Summary:  fmt.Sprintf("Exported %d files, %d symbols, %d import edges, %d modules, and %d packages for %s", includedFiles, symbolCount, importEdgeCount, len(moduleSet), len(packageSet), scope),
		Analysis: buildGraphAnalysis(nodes, edges, focusSet),
	}, nil
}

func (e *Engine) GraphNeighbors(ctx context.Context, target string, limit int) (*api.GraphNeighborsResult, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("graph neighbors requires a non-empty target")
	}
	if limit <= 0 {
		limit = 5
	}
	if err := e.graph.Build(ctx); err != nil {
		return nil, err
	}

	resolvedFile, resolution, err := e.resolveGraphNavigationTarget(ctx, target)
	if err != nil {
		return nil, err
	}

	syms, err := e.store.GetFileSymbols(ctx, resolvedFile)
	if err != nil {
		return nil, err
	}
	imports, err := e.store.GetImports(ctx, resolvedFile)
	if err != nil {
		return nil, err
	}

	symbolNames := make([]string, 0, minInt(len(syms), limit))
	for i, sym := range syms {
		if i >= limit {
			break
		}
		symbolNames = append(symbolNames, fmt.Sprintf("%s (%s)", sym.Name, sym.Kind))
	}

	importNames := make([]string, 0, minInt(len(imports), limit))
	seenImports := make(map[string]bool)
	for _, imp := range imports {
		if seenImports[imp.ToSource] {
			continue
		}
		seenImports[imp.ToSource] = true
		importNames = append(importNames, imp.ToSource)
		if len(importNames) >= limit {
			break
		}
	}

	related := e.graph.FileNeighbors(resolvedFile)
	if len(related) > limit {
		related = related[:limit]
	}
	if len(related) == 0 && len(importNames) > 0 {
		related = e.relatedFilesFromImports(ctx, resolvedFile, importNames, limit)
	}
	return &api.GraphNeighborsResult{
		Target:       target,
		ResolvedFile: resolvedFile,
		Resolution:   resolution,
		Symbols:      symbolNames,
		Imports:      importNames,
		RelatedFiles: related,
		Summary: fmt.Sprintf("Resolved %s with %d symbols, %d direct imports, and %d related files",
			resolvedFile, len(syms), len(importNames), len(related)),
	}, nil
}

func (e *Engine) relatedFilesFromImports(ctx context.Context, resolvedFile string, importNames []string, limit int) []string {
	scores := make(map[string]int)
	for _, imp := range importNames {
		importers, err := e.store.GetImporters(ctx, imp)
		if err != nil {
			continue
		}
		seen := make(map[string]bool)
		for _, importer := range importers {
			if importer.FromFile == resolvedFile || seen[importer.FromFile] {
				continue
			}
			seen[importer.FromFile] = true
			scores[importer.FromFile]++
		}
	}
	items := topGraphScores(scores, limit)
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Name)
	}
	return result
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func graphModulePath(filePath string) string {
	dir := filepath.Dir(filePath)
	if dir == "." || dir == string(filepath.Separator) {
		return "."
	}
	return filepath.Clean(dir)
}

func graphModuleNodeID(modulePath string) string {
	return "module:" + modulePath
}

func graphPackageKey(name, modulePath string) string {
	return modulePath + "::" + name
}

func splitGraphPackageKey(key string) (string, string) {
	parts := strings.SplitN(key, "::", 2)
	if len(parts) != 2 {
		return key, "."
	}
	return parts[1], parts[0]
}

func graphPackageNodeID(key string) string {
	return "package:" + key
}

func normalizeImportSource(source string) string {
	source = strings.TrimSpace(strings.Trim(source, "\"'`"))
	source = strings.TrimPrefix(source, "./")
	source = strings.TrimPrefix(source, "/")
	source = filepath.Clean(source)
	if source == "." {
		return ""
	}
	return strings.TrimSpace(source)
}

func buildInternalImportTargets(fileByPath map[string]*api.FileInfo, packageNamesByFile map[string][]string, filesByPackage map[string][]string, filesByModule map[string][]string) map[string][]string {
	result := make(map[string][]string)
	add := func(key, file string) {
		key = normalizeImportSource(key)
		if key == "" {
			return
		}
		result[key] = appendIfMissing(result[key], file)
	}
	for filePath := range fileByPath {
		modulePath := graphModulePath(filePath)
		base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		add(filePath, filePath)
		add(modulePath, filePath)
		add(base, filePath)
		for _, packageName := range packageNamesByFile[filePath] {
			add(packageName, filePath)
			add(modulePath+"/"+packageName, filePath)
		}
		for moduleKey := range parentModuleKeys(modulePath) {
			add(moduleKey, filePath)
		}
	}
	for packageKey, files := range filesByPackage {
		packageName, modulePath := splitGraphPackageKey(packageKey)
		for _, file := range files {
			add(packageName, file)
			add(modulePath+"/"+packageName, file)
		}
	}
	for modulePath, files := range filesByModule {
		for _, file := range files {
			add(modulePath, file)
		}
	}
	for key, files := range result {
		sort.Strings(files)
		result[key] = dedupeStrings(files)
	}
	return result
}

func parentModuleKeys(modulePath string) map[string]bool {
	result := make(map[string]bool)
	current := normalizeImportSource(modulePath)
	for current != "" {
		result[current] = true
		next := filepath.Dir(current)
		if next == "." || next == current {
			break
		}
		current = normalizeImportSource(next)
	}
	return result
}

func appendIfMissing(items []string, target string) []string {
	for _, item := range items {
		if item == target {
			return items
		}
	}
	return append(items, target)
}

func importResolutionConfidence(source, targetFile string) string {
	normalized := normalizeImportSource(source)
	modulePath := normalizeImportSource(graphModulePath(targetFile))
	base := normalizeImportSource(strings.TrimSuffix(filepath.Base(targetFile), filepath.Ext(targetFile)))
	if normalized == normalizeImportSource(targetFile) || normalized == modulePath || normalized == base {
		return "INFERRED"
	}
	if strings.HasSuffix(normalized, "/"+base) || strings.HasSuffix(normalized, "/"+modulePath) {
		return "INFERRED"
	}
	return "AMBIGUOUS"
}

func buildGraphAnalysis(nodes []api.GraphNode, edges []api.GraphEdge, focusSet map[string]bool) *api.GraphAnalysis {
	importCounts := make(map[string]int)
	fileImports := make(map[string]map[string]bool)
	fileNodes := make(map[string]string)
	for _, node := range nodes {
		if node.Type == "file" {
			fileNodes[node.ID] = node.FilePath
		}
	}
	for _, edge := range edges {
		if edge.Type != "imports" {
			continue
		}
		importName := strings.TrimPrefix(edge.Target, "import:")
		importCounts[importName]++
		if _, ok := fileImports[edge.Source]; !ok {
			fileImports[edge.Source] = make(map[string]bool)
		}
		fileImports[edge.Source][importName] = true
	}

	allFiles := make([]string, 0, len(fileNodes))
	for _, file := range fileNodes {
		allFiles = append(allFiles, file)
	}
	sort.Strings(allFiles)

	fileCounts := make(map[string]int)
	bridgeCounts := make(map[string]int)
	sharedScores := make(map[string]map[string]int)
	importToFiles := make(map[string][]string)
	for sourceID, imports := range fileImports {
		sourceFile := fileNodes[sourceID]
		for imp := range imports {
			importToFiles[imp] = append(importToFiles[imp], sourceFile)
		}
		for otherID, otherImports := range fileImports {
			if sourceID >= otherID {
				continue
			}
			shared := 0
			for imp := range imports {
				if otherImports[imp] {
					shared++
				}
			}
			if shared > 0 {
				otherFile := fileNodes[otherID]
				fileCounts[sourceFile] += shared
				fileCounts[otherFile] += shared
				if sharedScores[sourceFile] == nil {
					sharedScores[sourceFile] = make(map[string]int)
				}
				if sharedScores[otherFile] == nil {
					sharedScores[otherFile] = make(map[string]int)
				}
				sharedScores[sourceFile][otherFile] += shared
				sharedScores[otherFile][sourceFile] += shared
			}
		}
	}
	for imp, files := range importToFiles {
		uniqueFiles := dedupeStrings(files)
		if len(uniqueFiles) <= 1 {
			continue
		}
		for _, file := range uniqueFiles {
			bridgeCounts[file] += len(uniqueFiles) - 1
		}
		_ = imp
	}

	recommendedScores := make(map[string]int)
	if len(focusSet) == 0 {
		for file, count := range fileCounts {
			recommendedScores[file] = count
		}
	} else {
		for focusFile := range focusSet {
			for otherFile, count := range sharedScores[focusFile] {
				if otherFile != focusFile {
					recommendedScores[otherFile] += count
				}
			}
		}
	}

	topImports := topGraphScores(importCounts, 3)
	mostConnected := topGraphScores(fileCounts, 3)
	bridgeFiles := topGraphScores(bridgeCounts, 3)
	hotspotFiles := topGraphScores(sumGraphCounts(fileCounts, bridgeCounts), 3)
	recommendedItems := topGraphScores(recommendedScores, 3)
	recommended := make([]string, 0, len(recommendedItems))
	for _, item := range recommendedItems {
		recommended = append(recommended, item.Name)
	}
	relationHighlights := buildRelationHighlights(sharedScores, focusSet)
	readingPaths := buildReadingPaths(allFiles, sharedScores, focusSet, recommended)
	if len(topImports) == 0 && len(mostConnected) == 0 && len(bridgeFiles) == 0 && len(hotspotFiles) == 0 && len(recommended) == 0 && len(relationHighlights) == 0 && len(readingPaths) == 0 {
		return nil
	}
	return &api.GraphAnalysis{
		TopImports:         topImports,
		MostConnectedFiles: mostConnected,
		BridgeFiles:        bridgeFiles,
		HotspotFiles:       hotspotFiles,
		RecommendedFiles:   recommended,
		RelationHighlights: relationHighlights,
		ReadingPaths:       readingPaths,
	}
}

func sumGraphCounts(a, b map[string]int) map[string]int {
	result := make(map[string]int)
	for key, value := range a {
		result[key] += value
	}
	for key, value := range b {
		result[key] += value
	}
	return result
}

func buildRelationHighlights(sharedScores map[string]map[string]int, focusSet map[string]bool) []string {
	type relation struct {
		from  string
		to    string
		score int
	}
	var relations []relation
	seen := make(map[string]bool)
	for from, related := range sharedScores {
		if len(focusSet) > 0 && !focusSet[from] {
			continue
		}
		for to, score := range related {
			keyA, keyB := from, to
			if keyA > keyB {
				keyA, keyB = keyB, keyA
			}
			key := keyA + "->" + keyB
			if seen[key] {
				continue
			}
			seen[key] = true
			relations = append(relations, relation{from: from, to: to, score: score})
		}
	}
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].score != relations[j].score {
			return relations[i].score > relations[j].score
		}
		if relations[i].from != relations[j].from {
			return relations[i].from < relations[j].from
		}
		return relations[i].to < relations[j].to
	})
	if len(relations) > 3 {
		relations = relations[:3]
	}
	result := make([]string, 0, len(relations))
	for _, rel := range relations {
		result = append(result, fmt.Sprintf("%s ↔ %s share %d graph links", rel.from, rel.to, rel.score))
	}
	return result
}

func buildReadingPaths(allFiles []string, sharedScores map[string]map[string]int, focusSet map[string]bool, recommended []string) []api.GraphReadingPath {
	entries := make([]string, 0)
	if len(focusSet) > 0 {
		for file := range focusSet {
			entries = append(entries, file)
		}
		sort.Strings(entries)
	} else if len(allFiles) > 0 {
		entries = append(entries, allFiles[0])
	}
	result := make([]api.GraphReadingPath, 0, len(entries))
	for _, entry := range entries {
		path := []string{entry}
		seen := map[string]bool{entry: true}
		current := entry
		for len(path) < 3 {
			next, ok := strongestUnseenNeighbor(sharedScores[current], seen)
			if !ok {
				break
			}
			path = append(path, next)
			seen[next] = true
			current = next
		}
		if len(path) == 1 && len(recommended) > 0 {
			for _, candidate := range recommended {
				if !seen[candidate] {
					path = append(path, candidate)
					break
				}
			}
		}
		if len(path) <= 1 {
			continue
		}
		result = append(result, api.GraphReadingPath{
			Entry:  entry,
			Path:   path,
			Reason: fmt.Sprintf("Start at %s and follow the strongest neighboring files", entry),
		})
	}
	return result
}

func strongestUnseenNeighbor(neighbors map[string]int, seen map[string]bool) (string, bool) {
	bestName := ""
	bestScore := -1
	for name, score := range neighbors {
		if seen[name] {
			continue
		}
		if score > bestScore || (score == bestScore && (bestName == "" || name < bestName)) {
			bestName = name
			bestScore = score
		}
	}
	if bestName == "" {
		return "", false
	}
	return bestName, true
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

func topGraphScores(scores map[string]int, limit int) []api.GraphScoreItem {
	if limit <= 0 {
		limit = 3
	}
	type pair struct {
		name  string
		count int
	}
	items := make([]pair, 0, len(scores))
	for name, count := range scores {
		items = append(items, pair{name: name, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].name < items[j].name
	})
	if len(items) > limit {
		items = items[:limit]
	}
	result := make([]api.GraphScoreItem, 0, len(items))
	for _, item := range items {
		result = append(result, api.GraphScoreItem{Name: item.name, Count: item.count})
	}
	return result
}

func graphAnalysisForFiles(analysis *api.GraphAnalysis, files []string) *api.GraphAnalysis {
	if analysis == nil {
		return nil
	}
	focus := make(map[string]bool, len(files))
	for _, file := range files {
		focus[file] = true
	}
	result := &api.GraphAnalysis{
		TopImports:         append([]api.GraphScoreItem(nil), analysis.TopImports...),
		MostConnectedFiles: filterGraphScoreItems(analysis.MostConnectedFiles, focus),
		BridgeFiles:        filterGraphScoreItems(analysis.BridgeFiles, focus),
		HotspotFiles:       filterGraphScoreItems(analysis.HotspotFiles, focus),
		RecommendedFiles:   filterStringsBySet(analysis.RecommendedFiles, focus, false),
		RelationHighlights: filterRelationHighlights(analysis.RelationHighlights, focus),
		ReadingPaths:       filterReadingPaths(analysis.ReadingPaths, focus),
	}
	if len(result.TopImports) == 0 && len(result.MostConnectedFiles) == 0 && len(result.BridgeFiles) == 0 && len(result.HotspotFiles) == 0 && len(result.RecommendedFiles) == 0 && len(result.RelationHighlights) == 0 && len(result.ReadingPaths) == 0 {
		return nil
	}
	return result
}

func filterGraphScoreItems(items []api.GraphScoreItem, focus map[string]bool) []api.GraphScoreItem {
	if len(items) == 0 || len(focus) == 0 {
		return append([]api.GraphScoreItem(nil), items...)
	}
	result := make([]api.GraphScoreItem, 0, len(items))
	for _, item := range items {
		if focus[item.Name] {
			result = append(result, item)
		}
	}
	return result
}

func filterStringsBySet(items []string, focus map[string]bool, include bool) []string {
	if len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		_, ok := focus[item]
		if (include && ok) || (!include && !ok) {
			result = append(result, item)
		}
	}
	return result
}

func filterRelationHighlights(items []string, focus map[string]bool) []string {
	if len(items) == 0 || len(focus) == 0 {
		return append([]string(nil), items...)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		for file := range focus {
			if strings.Contains(item, file) {
				result = append(result, item)
				break
			}
		}
	}
	return result
}

func filterReadingPaths(paths []api.GraphReadingPath, focus map[string]bool) []api.GraphReadingPath {
	if len(paths) == 0 {
		return nil
	}
	if len(focus) == 0 {
		return append([]api.GraphReadingPath(nil), paths...)
	}
	result := make([]api.GraphReadingPath, 0, len(paths))
	for _, path := range paths {
		if focus[path.Entry] {
			result = append(result, path)
		}
	}
	return result
}

func graphSummaryParts(analysis *api.GraphAnalysis, related []string, recommended []string, nodeCount, edgeCount int, filePath string) []string {
	parts := []string{fmt.Sprintf("Graph view covers %d nodes and %d edges", nodeCount, edgeCount)}
	if len(related) > 0 {
		parts = append(parts, fmt.Sprintf("nearby files: %s", strings.Join(related, ", ")))
	}
	if len(recommended) > 0 {
		parts = append(parts, fmt.Sprintf("recommended next files: %s", strings.Join(recommended, ", ")))
	}
	if analysis != nil {
		if len(analysis.BridgeFiles) > 0 {
			parts = append(parts, fmt.Sprintf("bridge files: %s", joinGraphScoreItems(analysis.BridgeFiles)))
		}
		if len(analysis.HotspotFiles) > 0 {
			parts = append(parts, fmt.Sprintf("hotspots: %s", joinGraphScoreItems(analysis.HotspotFiles)))
		}
		if len(analysis.ReadingPaths) > 0 {
			parts = append(parts, fmt.Sprintf("reading path: %s", strings.Join(analysis.ReadingPaths[0].Path, " -> ")))
		}
		if len(analysis.RelationHighlights) > 0 {
			parts = append(parts, analysis.RelationHighlights[0])
		}
	}
	_ = filePath
	return parts
}

func joinGraphScoreItems(items []api.GraphScoreItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s (%d)", item.Name, item.Count))
	}
	return strings.Join(parts, ", ")
}

func (e *Engine) graphInsightsForFile(ctx context.Context, filePath string, limit int) (*api.GraphAnalysis, []string, []string, string) {
	globalGraph, err := e.ExportGraph(ctx, "")
	if err != nil || globalGraph == nil {
		return nil, nil, nil, "No graph insights available"
	}
	analysis := graphAnalysisForFiles(globalGraph.Analysis, []string{filePath})

	localGraph, err := e.GraphSubgraph(ctx, filePath, 1)
	if err != nil || localGraph == nil || localGraph.Graph == nil {
		if analysis == nil {
			return nil, nil, nil, "No graph insights available"
		}
		recommended := filterStringsBySet(analysis.RecommendedFiles, map[string]bool{filePath: true}, false)
		if len(recommended) > limit {
			recommended = recommended[:limit]
		}
		return analysis, nil, recommended, "No local graph neighborhood available"
	}

	if buildErr := e.graph.Build(ctx); buildErr != nil {
		return analysis, nil, nil, fmt.Sprintf("Graph view covers %d nodes and %d edges", len(localGraph.Graph.Nodes), len(localGraph.Graph.Edges))
	}
	related := e.graph.FileNeighbors(filePath)
	if len(related) > limit {
		related = related[:limit]
	}
	recommended := make([]string, 0)
	if analysis != nil {
		recommended = filterStringsBySet(analysis.RecommendedFiles, map[string]bool{filePath: true}, false)
		if len(recommended) > limit {
			recommended = recommended[:limit]
		}
	}
	if len(recommended) == 0 && len(related) > 0 {
		recommended = append(recommended, related...)
		if len(recommended) > limit {
			recommended = recommended[:limit]
		}
	}
	summaryParts := graphSummaryParts(analysis, related, recommended, len(localGraph.Graph.Nodes), len(localGraph.Graph.Edges), filePath)
	return analysis, related, recommended, strings.Join(summaryParts, "; ")
}

func snapshotRecommendedFiles(files []FileSummary, limit int) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, limit)
	for _, file := range files {
		for _, candidate := range file.RecommendedFiles {
			if seen[candidate] || candidate == file.Path {
				continue
			}
			seen[candidate] = true
			result = append(result, candidate)
			if len(result) >= limit {
				return result
			}
		}
	}
	return result
}

func mergeGraphAnalysesFromFiles(files []FileSummary) *api.GraphAnalysis {
	if len(files) == 0 {
		return nil
	}
	importCounts := make(map[string]int)
	connectedCounts := make(map[string]int)
	bridgeCounts := make(map[string]int)
	hotspotCounts := make(map[string]int)
	recommendedCounts := make(map[string]int)
	relationCounts := make(map[string]int)
	readingPaths := make([]api.GraphReadingPath, 0)
	seenReadingPath := make(map[string]bool)
	for _, file := range files {
		if file.Analysis == nil {
			continue
		}
		for _, item := range file.Analysis.TopImports {
			importCounts[item.Name] += item.Count
		}
		for _, item := range file.Analysis.MostConnectedFiles {
			connectedCounts[item.Name] += item.Count
		}
		for _, item := range file.Analysis.BridgeFiles {
			bridgeCounts[item.Name] += item.Count
		}
		for _, item := range file.Analysis.HotspotFiles {
			hotspotCounts[item.Name] += item.Count
		}
		for _, item := range file.Analysis.RecommendedFiles {
			recommendedCounts[item]++
		}
		for _, item := range file.Analysis.RelationHighlights {
			relationCounts[item]++
		}
		for _, path := range file.Analysis.ReadingPaths {
			key := path.Entry + ":" + strings.Join(path.Path, "->")
			if seenReadingPath[key] {
				continue
			}
			seenReadingPath[key] = true
			readingPaths = append(readingPaths, path)
		}
	}
	recommendedItems := topGraphScores(recommendedCounts, 3)
	recommended := make([]string, 0, len(recommendedItems))
	for _, item := range recommendedItems {
		recommended = append(recommended, item.Name)
	}
	relationItems := topGraphScores(relationCounts, 3)
	relationHighlights := make([]string, 0, len(relationItems))
	for _, item := range relationItems {
		relationHighlights = append(relationHighlights, item.Name)
	}
	if len(readingPaths) > 3 {
		readingPaths = readingPaths[:3]
	}
	analysis := &api.GraphAnalysis{
		TopImports:         topGraphScores(importCounts, 3),
		MostConnectedFiles: topGraphScores(connectedCounts, 3),
		BridgeFiles:        topGraphScores(bridgeCounts, 3),
		HotspotFiles:       topGraphScores(hotspotCounts, 3),
		RecommendedFiles:   recommended,
		RelationHighlights: relationHighlights,
		ReadingPaths:       readingPaths,
	}
	if len(analysis.TopImports) == 0 && len(analysis.MostConnectedFiles) == 0 && len(analysis.BridgeFiles) == 0 && len(analysis.HotspotFiles) == 0 && len(analysis.RecommendedFiles) == 0 && len(analysis.RelationHighlights) == 0 && len(analysis.ReadingPaths) == 0 {
		return nil
	}
	return analysis
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (e *Engine) GraphPath(ctx context.Context, from, to string) (*api.GraphPathResult, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return nil, fmt.Errorf("graph path requires non-empty from and to")
	}

	fromFile, fromResolution, err := e.resolveGraphPathTarget(ctx, from)
	if err != nil {
		return nil, err
	}
	toFile, toResolution, err := e.resolveGraphPathTarget(ctx, to)
	if err != nil {
		return nil, err
	}

	if err := e.graph.Build(ctx); err != nil {
		return nil, err
	}

	files := e.graph.TraceFiles(fromFile, toFile, 6)
	result := &api.GraphPathResult{
		From:       from,
		To:         to,
		FromFile:   fromFile,
		ToFile:     toFile,
		Files:      files,
		PathFound:  len(files) > 0,
		Resolution: strings.TrimSpace(strings.Join([]string{fromResolution, toResolution}, "; ")),
	}
	if result.PathFound {
		result.Summary = fmt.Sprintf("Found graph path across %d files from %s to %s", len(files), fromFile, toFile)
	} else {
		result.Summary = fmt.Sprintf("No graph path found from %s to %s", fromFile, toFile)
	}
	return result, nil
}

func (e *Engine) resolveGraphPathTarget(ctx context.Context, target string) (string, string, error) {
	return e.resolveGraphNavigationTarget(ctx, target)
}

func (e *Engine) resolveGraphFocusFiles(ctx context.Context, target string) ([]string, error) {
	if filePath, _, err := e.resolveGraphFileTarget(ctx, target); err == nil {
		return []string{filePath}, nil
	}
	defs, err := e.search.FindDefinition(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, fmt.Errorf("graph focus not found: %s", target)
	}
	seen := make(map[string]bool)
	files := make([]string, 0, len(defs))
	for _, def := range defs {
		if seen[def.FilePath] {
			continue
		}
		seen[def.FilePath] = true
		files = append(files, def.FilePath)
	}
	sort.Strings(files)
	return files, nil
}

func (e *Engine) resolveGraphNavigationTarget(ctx context.Context, target string) (string, string, error) {
	if filePath, resolution, err := e.resolveGraphFileTarget(ctx, target); err == nil {
		return filePath, resolution, nil
	}

	defs, err := e.search.FindDefinition(ctx, target)
	if err != nil {
		return "", "", err
	}
	if len(defs) > 0 {
		return defs[0].FilePath, fmt.Sprintf("resolved %q as symbol in %s", target, defs[0].FilePath), nil
	}

	matches, err := e.search.SearchSymbols(ctx, target, nil, 1)
	if err != nil {
		return "", "", err
	}
	if len(matches) > 0 {
		return matches[0].FilePath, fmt.Sprintf("resolved %q via symbol search in %s", target, matches[0].FilePath), nil
	}

	return "", "", fmt.Errorf("graph path target not found: %s", target)
}

func (e *Engine) resolveGraphFileTarget(ctx context.Context, target string) (string, string, error) {
	target = filepath.Clean(strings.TrimSpace(target))
	if target == "" || target == "." {
		return "", "", fmt.Errorf("empty graph file target")
	}

	if filepath.IsAbs(target) {
		if rel, err := filepath.Rel(e.root, target); err == nil {
			target = filepath.Clean(rel)
		}
	}

	if filePath, ok, err := e.lookupExistingFileTarget(ctx, target); err != nil {
		return "", "", err
	} else if ok {
		return filePath, fmt.Sprintf("resolved %q as file", target), nil
	}

	files, err := e.store.ListFiles(ctx, nil)
	if err != nil {
		return "", "", err
	}

	var basenameMatches []string
	for _, f := range files {
		cleanPath := filepath.Clean(f.Path)
		if cleanPath == target {
			return f.Path, fmt.Sprintf("resolved %q as file", target), nil
		}
		if strings.TrimPrefix(cleanPath, "./") == target {
			return f.Path, fmt.Sprintf("resolved %q as file", target), nil
		}
		if filepath.Base(cleanPath) == filepath.Base(target) {
			basenameMatches = append(basenameMatches, f.Path)
		}
	}
	if len(basenameMatches) == 1 {
		return basenameMatches[0], fmt.Sprintf("resolved %q by basename to %s", target, basenameMatches[0]), nil
	}

	return "", "", fmt.Errorf("graph file target not found: %s", target)
}

func (e *Engine) lookupExistingFileTarget(ctx context.Context, target string) (string, bool, error) {
	file, err := e.store.GetFile(ctx, target)
	if err != nil {
		return "", false, err
	}
	if file != nil {
		return file.Path, true, nil
	}
	trimmed := strings.TrimPrefix(target, "./")
	if trimmed == target {
		return "", false, nil
	}
	file, err = e.store.GetFile(ctx, trimmed)
	if err != nil {
		return "", false, err
	}
	if file != nil {
		return file.Path, true, nil
	}
	return "", false, nil
}

func (e *Engine) Stats(ctx context.Context) (*api.IndexStats, error) {
	return e.store.Stats(ctx)
}

func (e *Engine) Status(ctx context.Context) (*api.ServiceStatus, error) {
	stats, err := e.store.Stats(ctx)
	if err != nil {
		return nil, err
	}
	if stats.IndexVersion == "" {
		stats.IndexVersion = graphExportVersion
	}
	watch := e.currentWatchStatus()
	if freshness, err := e.Freshness(ctx, 20); err == nil {
		watch.Stale = freshness.Stale
		watch.Freshness = freshness
		watch.PendingFiles = freshnessPaths(freshness.Items)
	}
	return &api.ServiceStatus{
		Root:         e.root,
		DatabasePath: e.dbPath,
		GraphVersion: graphExportVersion,
		Capabilities: e.capabilityNames(),
		Embedding:    e.embeddingStatus(),
		Answer:       e.answerStatus(),
		Index:        stats,
		Watch:        &watch,
	}, nil
}

func (e *Engine) capabilityNames() []string {
	seen := map[string]struct{}{}
	for _, provider := range []any{e.store, e.embedder, e.answerer} {
		for _, cap := range store.DetectCapabilities(provider) {
			if cap != "" {
				seen[string(cap)] = struct{}{}
			}
		}
	}
	if e.canSearchHybrid() {
		seen[string(store.CapabilityHybridSearch)] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (e *Engine) canSearchHybrid() bool {
	if _, ok := e.store.(store.HybridSearcher); ok {
		return true
	}
	if _, ok := e.store.(store.TextSearcher); ok {
		return true
	}
	if _, ok := e.store.(store.VectorSearcher); ok {
		return true
	}
	if _, ok := e.store.(store.GraphTraverser); ok {
		return true
	}
	return false
}

func (e *Engine) embeddingStatus() *api.EmbeddingStatus {
	if e.embedder == nil {
		return &api.EmbeddingStatus{Enabled: false}
	}
	info := e.embedder.EmbeddingModel()
	return &api.EmbeddingStatus{
		Enabled:    true,
		Provider:   info.Provider,
		Model:      info.Model,
		Dimensions: info.Dimensions,
		BaseURL:    info.BaseURL,
		BatchSize:  info.BatchSize,
	}
}

func (e *Engine) answerStatus() *api.AnswerStatus {
	if e.answerer == nil {
		return &api.AnswerStatus{Enabled: false}
	}
	info := e.answerer.AnswerModel()
	return &api.AnswerStatus{
		Enabled:     true,
		Provider:    info.Provider,
		Model:       info.Model,
		BaseURL:     info.BaseURL,
		MaxTokens:   info.MaxTokens,
		Temperature: info.Temperature,
	}
}

func (e *Engine) ProviderDiagnostics(ctx context.Context) (*api.ProviderDiagnosticsReport, error) {
	checks := []api.ProviderConfigCheck{
		e.embeddingProviderCheck(),
		e.answerProviderCheck(),
	}
	ok := true
	warns := 0
	for _, check := range checks {
		switch check.Status {
		case "error":
			ok = false
		case "warn":
			warns++
		}
	}
	summary := "provider configuration ok"
	if !ok {
		summary = "provider configuration has errors"
	} else if warns > 0 {
		summary = fmt.Sprintf("provider configuration ok with %d warnings", warns)
	}
	return &api.ProviderDiagnosticsReport{OK: ok, Summary: summary, Checks: checks}, nil
}

func (e *Engine) embeddingProviderCheck() api.ProviderConfigCheck {
	opts := e.options.Embedding
	provider := opts.ProviderOrDefault()
	check := api.ProviderConfigCheck{
		Kind:     "embedding",
		Provider: provider,
		Status:   "ok",
	}
	if provider == embeddingpkg.ProviderNone {
		check.Enabled = false
		check.Message = "embedding provider disabled"
		check.Actions = []string{"set --embedding-provider openai-compatible with --embedding-base-url and --embedding-model to enable vector search"}
		return check
	}
	if e.embedder == nil {
		check.Status = "error"
		check.Message = "embedding provider configured but not available"
		check.Actions = []string{"run code-context config inspect", "verify embedding provider settings"}
		return check
	}
	info := e.embedder.EmbeddingModel()
	check.Enabled = true
	check.Provider = info.Provider
	check.Model = info.Model
	check.BaseURL = info.BaseURL
	check.Message = fmt.Sprintf("embedding provider %s model=%s", info.Provider, info.Model)
	if provider == embeddingpkg.ProviderOpenAI && strings.TrimSpace(opts.ResolvedAPIKey()) == "" {
		check.Status = "error"
		check.Message = "OpenAI embedding provider requires an API key"
		check.Actions = []string{"set --embedding-api-key-env OPENAI_API_KEY or --embedding-api-key"}
		return check
	}
	if provider == embeddingpkg.ProviderOpenAICompatible && strings.TrimSpace(opts.ResolvedAPIKey()) == "" {
		check.Status = "warn"
		check.Message = "openai-compatible embedding provider has no API key; assuming local or unauthenticated endpoint"
		check.Actions = []string{"set --embedding-api-key or --embedding-api-key-env if the endpoint requires authentication"}
		return check
	}
	return check
}

func (e *Engine) answerProviderCheck() api.ProviderConfigCheck {
	opts := e.options.Answer
	provider := opts.ProviderOrDefault()
	check := api.ProviderConfigCheck{
		Kind:     "answer",
		Provider: provider,
		Status:   "ok",
	}
	if provider == answerpkg.ProviderNone {
		check.Enabled = false
		check.Message = "answer provider disabled"
		check.Actions = []string{"set --answer-provider openai-compatible with --answer-base-url and --answer-model to enable provider-backed answers"}
		return check
	}
	if e.answerer == nil {
		check.Status = "error"
		check.Message = "answer provider configured but not available"
		check.Actions = []string{"run code-context config inspect", "verify answer provider settings"}
		return check
	}
	info := e.answerer.AnswerModel()
	check.Enabled = true
	check.Provider = info.Provider
	check.Model = info.Model
	check.BaseURL = info.BaseURL
	check.Message = fmt.Sprintf("answer provider %s model=%s", info.Provider, info.Model)
	if provider == answerpkg.ProviderOpenAI && strings.TrimSpace(opts.ResolvedAPIKey()) == "" {
		check.Status = "error"
		check.Message = "OpenAI answer provider requires an API key"
		check.Actions = []string{"set --answer-api-key-env OPENAI_API_KEY or --answer-api-key"}
		return check
	}
	if provider == answerpkg.ProviderOpenAICompatible && strings.TrimSpace(opts.ResolvedAPIKey()) == "" {
		check.Status = "warn"
		check.Message = "openai-compatible answer provider has no API key; assuming local or unauthenticated endpoint"
		check.Actions = []string{"set --answer-api-key or --answer-api-key-env if the endpoint requires authentication"}
		return check
	}
	return check
}

type EmbeddingPlan struct {
	Enabled          bool                     `json:"enabled"`
	CacheSupported   bool                     `json:"cache_supported"`
	Provider         string                   `json:"provider,omitempty"`
	Model            string                   `json:"model,omitempty"`
	Dimensions       int                      `json:"dimensions,omitempty"`
	TotalChunks      int                      `json:"total_chunks"`
	CachedChunks     int                      `json:"cached_chunks"`
	MissingChunks    int                      `json:"missing_chunks"`
	StaleChunks      int                      `json:"stale_chunks,omitempty"`
	ErrorChunks      int                      `json:"error_chunks,omitempty"`
	BackfillRequired bool                     `json:"backfill_required"`
	Summary          string                   `json:"summary"`
	Items            []EmbeddingPlanItem      `json:"items,omitempty"`
	Truncated        bool                     `json:"truncated,omitempty"`
	Namespaces       []EmbeddingPlanNamespace `json:"namespaces,omitempty"`
}

type EmbeddingPlanNamespace struct {
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	Chunks     int    `json:"chunks"`
}

type EmbeddingPlanItem struct {
	Key         string                   `json:"key,omitempty"`
	Status      string                   `json:"status"`
	Reason      string                   `json:"reason,omitempty"`
	Kind        store.EmbeddingInputKind `json:"kind,omitempty"`
	Path        string                   `json:"path,omitempty"`
	Name        string                   `json:"name,omitempty"`
	Line        int                      `json:"line,omitempty"`
	ContentHash string                   `json:"content_hash,omitempty"`
}

func (e *Engine) EmbeddingPlan(ctx context.Context, limit int) (*EmbeddingPlan, error) {
	plan, _, err := e.embeddingPlanState(ctx, limit)
	return plan, err
}

type EmbeddingBackfillOptions struct {
	Limit int  `json:"limit,omitempty"`
	Apply bool `json:"apply,omitempty"`
}

type EmbeddingBackfillResult struct {
	DryRun         bool           `json:"dry_run"`
	Provider       string         `json:"provider,omitempty"`
	Model          string         `json:"model,omitempty"`
	Dimensions     int            `json:"dimensions,omitempty"`
	PlannedChunks  int            `json:"planned_chunks"`
	EmbeddedChunks int            `json:"embedded_chunks,omitempty"`
	SkippedChunks  int            `json:"skipped_chunks,omitempty"`
	Summary        string         `json:"summary"`
	Plan           *EmbeddingPlan `json:"plan,omitempty"`
}

type EmbeddingNamespaceReport struct {
	CacheSupported  bool                       `json:"cache_supported"`
	TotalNamespaces int                        `json:"total_namespaces"`
	TotalChunks     int                        `json:"total_chunks"`
	Namespaces      []store.EmbeddingNamespace `json:"namespaces,omitempty"`
	Summary         string                     `json:"summary"`
}

type EmbeddingPruneOptions struct {
	Model        string `json:"model,omitempty"`
	Dimensions   int    `json:"dimensions,omitempty"`
	Apply        bool   `json:"apply,omitempty"`
	ForceCurrent bool   `json:"force_current,omitempty"`
}

type EmbeddingPruneResult struct {
	DryRun           bool                      `json:"dry_run"`
	CacheSupported   bool                      `json:"cache_supported"`
	Model            string                    `json:"model,omitempty"`
	Dimensions       int                       `json:"dimensions,omitempty"`
	MatchedChunks    int                       `json:"matched_chunks,omitempty"`
	DeletedChunks    int                       `json:"deleted_chunks,omitempty"`
	CurrentNamespace bool                      `json:"current_namespace,omitempty"`
	Namespace        *store.EmbeddingNamespace `json:"namespace,omitempty"`
	Summary          string                    `json:"summary"`
}

type EmbeddingLifecycleReport struct {
	Embedding          *api.EmbeddingStatus               `json:"embedding,omitempty"`
	Plan               *EmbeddingPlan                     `json:"plan,omitempty"`
	Namespaces         *EmbeddingNamespaceReport          `json:"namespaces,omitempty"`
	CurrentNamespace   *store.EmbeddingNamespace          `json:"current_namespace,omitempty"`
	PruneCandidates    []EmbeddingLifecyclePruneCandidate `json:"prune_candidates,omitempty"`
	RecommendedActions []EmbeddingLifecycleRecommendation `json:"recommended_actions,omitempty"`
	Summary            string                             `json:"summary"`
}

type EmbeddingLifecyclePruneCandidate struct {
	Namespace store.EmbeddingNamespace `json:"namespace"`
	Reason    string                   `json:"reason,omitempty"`
	Command   string                   `json:"command,omitempty"`
}

type EmbeddingLifecycleRecommendation struct {
	Type        string `json:"type"`
	Summary     string `json:"summary"`
	Command     string `json:"command,omitempty"`
	Destructive bool   `json:"destructive,omitempty"`
}

func (e *Engine) EmbeddingNamespaces(ctx context.Context) (*EmbeddingNamespaceReport, error) {
	report := &EmbeddingNamespaceReport{}
	inspector, ok := e.store.(store.EmbeddingCacheInspector)
	if !ok {
		report.Summary = "active store does not implement embedding cache inspection"
		return report, nil
	}
	report.CacheSupported = true
	namespaces, err := inspector.ListEmbeddingNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	report.Namespaces = namespaces
	report.TotalNamespaces = len(namespaces)
	for _, ns := range namespaces {
		report.TotalChunks += ns.Chunks
	}
	if report.TotalNamespaces == 0 {
		report.Summary = "no embedding namespaces found in the active cache"
	} else {
		report.Summary = fmt.Sprintf("found %d embedding namespaces with %d cached chunks", report.TotalNamespaces, report.TotalChunks)
	}
	return report, nil
}

func (e *Engine) EmbeddingLifecycle(ctx context.Context, limit int) (*EmbeddingLifecycleReport, error) {
	report := &EmbeddingLifecycleReport{
		Embedding: e.embeddingStatus(),
	}

	namespaces, err := e.EmbeddingNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	report.Namespaces = namespaces

	plan, err := e.EmbeddingPlan(ctx, limit)
	if err != nil {
		return nil, err
	}
	report.Plan = plan

	if namespaces != nil {
		for _, ns := range namespaces.Namespaces {
			if e.isCurrentEmbeddingNamespace(ns.Model, ns.Dimensions) {
				current := ns
				if report.CurrentNamespace == nil {
					report.CurrentNamespace = &current
				}
				continue
			}
			report.PruneCandidates = append(report.PruneCandidates, EmbeddingLifecyclePruneCandidate{
				Namespace: ns,
				Reason:    "namespace does not match the currently configured embedding model/dimensions",
				Command:   fmt.Sprintf("code-context embedding-prune --model %s --dimensions %d", ns.Model, ns.Dimensions),
			})
		}
	}

	report.RecommendedActions = e.embeddingLifecycleRecommendations(report)
	report.Summary = embeddingLifecycleSummary(report)
	return report, nil
}

func (e *Engine) PruneEmbeddingNamespace(ctx context.Context, opts EmbeddingPruneOptions) (*EmbeddingPruneResult, error) {
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		return nil, fmt.Errorf("embedding model is required")
	}
	if opts.Dimensions <= 0 {
		return nil, fmt.Errorf("embedding dimensions are required")
	}

	result := &EmbeddingPruneResult{
		DryRun:     !opts.Apply,
		Model:      model,
		Dimensions: opts.Dimensions,
	}
	inspector, ok := e.store.(store.EmbeddingCacheInspector)
	if !ok {
		result.Summary = "active store does not implement embedding cache inspection"
		return result, nil
	}
	result.CacheSupported = true
	namespaces, err := inspector.ListEmbeddingNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	for _, ns := range namespaces {
		if ns.Model == model && ns.Dimensions == opts.Dimensions {
			copy := ns
			result.Namespace = &copy
			result.MatchedChunks = ns.Chunks
			break
		}
	}
	result.CurrentNamespace = e.isCurrentEmbeddingNamespace(model, opts.Dimensions)
	if result.MatchedChunks == 0 {
		result.Summary = fmt.Sprintf("embedding namespace %s/%d was not found", model, opts.Dimensions)
		return result, nil
	}
	if !opts.Apply {
		result.Summary = fmt.Sprintf("dry run: %d chunks would be deleted from embedding namespace %s/%d", result.MatchedChunks, model, opts.Dimensions)
		return result, nil
	}
	if result.CurrentNamespace && !opts.ForceCurrent {
		return nil, fmt.Errorf("refusing to prune current embedding namespace %s/%d without ForceCurrent", model, opts.Dimensions)
	}
	pruner, ok := e.store.(store.EmbeddingCachePruner)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, store.CapabilityEmbeddingCache)
	}
	deleted, err := pruner.DeleteEmbeddingNamespace(ctx, model, opts.Dimensions)
	if err != nil {
		return nil, err
	}
	result.DeletedChunks = deleted
	result.Summary = fmt.Sprintf("deleted %d chunks from embedding namespace %s/%d", deleted, model, opts.Dimensions)
	return result, nil
}

func (e *Engine) isCurrentEmbeddingNamespace(model string, dimensions int) bool {
	if e.embedder == nil || dimensions <= 0 {
		return false
	}
	info := e.embedder.EmbeddingModel()
	if strings.TrimSpace(info.Model) != strings.TrimSpace(model) {
		return false
	}
	return info.Dimensions <= 0 || info.Dimensions == dimensions
}

func (e *Engine) embeddingLifecycleRecommendations(report *EmbeddingLifecycleReport) []EmbeddingLifecycleRecommendation {
	recs := make([]EmbeddingLifecycleRecommendation, 0)
	if report == nil {
		return recs
	}
	embedding := report.Embedding
	plan := report.Plan
	namespaces := report.Namespaces

	if embedding == nil || !embedding.Enabled {
		recs = append(recs, EmbeddingLifecycleRecommendation{
			Type:    "configure_embedding",
			Summary: "embedding provider is disabled; configure one before vector search, hybrid vector fusion, or backfill",
		})
		if namespaces != nil && namespaces.TotalNamespaces > 0 {
			recs = append(recs, EmbeddingLifecycleRecommendation{
				Type:    "review_cached_namespaces",
				Summary: fmt.Sprintf("embedding is disabled but %d cached namespaces remain available for inspection or cleanup", namespaces.TotalNamespaces),
				Command: "code-context embedding-namespaces",
			})
		}
		return recs
	}

	if plan != nil && !plan.CacheSupported {
		recs = append(recs, EmbeddingLifecycleRecommendation{
			Type:    "enable_embedding_cache",
			Summary: "active store does not support embedding cache; choose a backend with EmbeddingCache support before backfill",
		})
		return recs
	}

	if plan != nil && plan.BackfillRequired {
		recs = append(recs, EmbeddingLifecycleRecommendation{
			Type:    "backfill",
			Summary: fmt.Sprintf("backfill %d missing, %d stale, and %d error chunks for %s/%d", plan.MissingChunks, plan.StaleChunks, plan.ErrorChunks, plan.Model, plan.Dimensions),
			Command: "code-context embedding-backfill --apply",
		})
	}

	if len(report.PruneCandidates) > 0 {
		recs = append(recs, EmbeddingLifecycleRecommendation{
			Type:        "prune",
			Summary:     fmt.Sprintf("review %d old embedding namespaces that do not match the current model", len(report.PruneCandidates)),
			Command:     "code-context embedding-namespaces",
			Destructive: true,
		})
	}

	if len(recs) == 0 {
		recs = append(recs, EmbeddingLifecycleRecommendation{
			Type:    "healthy",
			Summary: "embedding cache lifecycle looks healthy",
		})
	}
	return recs
}

func embeddingLifecycleSummary(report *EmbeddingLifecycleReport) string {
	if report == nil {
		return "embedding lifecycle status unavailable"
	}
	if report.Embedding == nil || !report.Embedding.Enabled {
		if report.Namespaces != nil && report.Namespaces.TotalNamespaces > 0 {
			return fmt.Sprintf("embedding disabled; %d cached namespaces remain", report.Namespaces.TotalNamespaces)
		}
		return "embedding disabled; no cached namespaces found"
	}
	if report.Plan != nil && !report.Plan.CacheSupported {
		return "embedding enabled but active store does not support embedding cache"
	}
	if report.Plan != nil && report.Plan.BackfillRequired {
		return fmt.Sprintf("embedding backfill required for namespace %s/%d", report.Plan.Model, report.Plan.Dimensions)
	}
	if len(report.PruneCandidates) > 0 {
		return fmt.Sprintf("embedding cache is current; %d old namespaces can be reviewed for pruning", len(report.PruneCandidates))
	}
	if report.Plan != nil {
		return fmt.Sprintf("embedding cache lifecycle healthy for namespace %s/%d", report.Plan.Model, report.Plan.Dimensions)
	}
	return "embedding cache lifecycle healthy"
}

func (e *Engine) BackfillEmbeddings(ctx context.Context, opts EmbeddingBackfillOptions) (*EmbeddingBackfillResult, error) {
	plan, pending, err := e.embeddingPlanState(ctx, 0)
	if err != nil {
		return nil, err
	}
	result := &EmbeddingBackfillResult{
		DryRun:     !opts.Apply,
		Provider:   plan.Provider,
		Model:      plan.Model,
		Dimensions: plan.Dimensions,
		Plan:       plan,
	}
	if !plan.Enabled || !plan.CacheSupported || !plan.BackfillRequired {
		result.Summary = plan.Summary
		return result, nil
	}
	targets := make([]embeddingPendingChunk, 0, len(pending))
	for _, item := range pending {
		if item.Item.Status == "error" {
			result.SkippedChunks++
			continue
		}
		targets = append(targets, item)
		if opts.Limit > 0 && len(targets) >= opts.Limit {
			break
		}
	}
	result.PlannedChunks = len(targets)
	if len(targets) == 0 {
		result.Summary = "no embedding chunks are eligible for backfill"
		return result, nil
	}
	if !opts.Apply {
		result.Summary = fmt.Sprintf("dry run: %d embedding chunks would be backfilled for namespace %s/%d", result.PlannedChunks, plan.Model, plan.Dimensions)
		return result, nil
	}
	cache, ok := e.store.(store.EmbeddingCache)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, store.CapabilityEmbeddingCache)
	}
	if e.embedder == nil {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, store.CapabilityEmbedding)
	}
	inputs := make([]store.EmbeddingInput, 0, len(targets))
	for _, target := range targets {
		inputs = append(inputs, target.Chunk.Input())
	}
	vectors, err := e.embedder.Embed(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(targets) {
		return nil, fmt.Errorf("embedding result count = %d, want %d", len(vectors), len(targets))
	}
	for i, vector := range vectors {
		target := targets[i]
		dimensions := vector.Dimensions
		if dimensions <= 0 {
			dimensions = len(vector.Values)
		}
		if err := cache.UpsertEmbedding(ctx, store.EmbeddingCacheEntry{
			Key:         target.Item.Key,
			Model:       firstNonEmptyEngine(vector.Model, plan.Model),
			Dimensions:  dimensions,
			ContentHash: target.Chunk.ContentHash,
			InputKind:   target.Chunk.Kind,
			Target:      target.Chunk.Target,
			Values:      vector.Values,
			Metadata:    mergeStringMapsEngine(target.Chunk.Metadata, vector.Metadata),
		}); err != nil {
			return nil, err
		}
		result.EmbeddedChunks++
	}
	result.Summary = fmt.Sprintf("backfilled %d embedding chunks for namespace %s/%d", result.EmbeddedChunks, plan.Model, plan.Dimensions)
	return result, nil
}

type embeddingPendingChunk struct {
	Item  EmbeddingPlanItem
	Chunk embeddingpkg.Chunk
}

func (e *Engine) embeddingPlanState(ctx context.Context, limit int) (*EmbeddingPlan, []embeddingPendingChunk, error) {
	plan := &EmbeddingPlan{}
	if e.embedder == nil {
		plan.Summary = "embedding provider is disabled; no embedding backfill plan is available"
		return plan, nil, nil
	}
	info := e.embedder.EmbeddingModel()
	plan.Enabled = true
	plan.Provider = info.Provider
	plan.Model = strings.TrimSpace(info.Model)
	plan.Dimensions = info.Dimensions
	cache, ok := e.store.(store.EmbeddingCache)
	if !ok {
		plan.Summary = "active store does not implement embedding cache; embeddings cannot be planned"
		return plan, nil, nil
	}
	plan.CacheSupported = true
	if plan.Model == "" {
		return nil, nil, fmt.Errorf("embedding model is required")
	}

	chunks, err := e.embeddingPlanChunks(ctx)
	if err != nil {
		return nil, nil, err
	}
	plan.TotalChunks = len(chunks)
	pending := make([]embeddingPendingChunk, 0)
	namespaceCounts := map[string]*EmbeddingPlanNamespace{}
	addItem := func(item EmbeddingPlanItem) {
		if limit <= 0 || len(plan.Items) < limit {
			plan.Items = append(plan.Items, item)
		} else {
			plan.Truncated = true
		}
	}
	for _, chunk := range chunks {
		select {
		case <-ctx.Done():
			return plan, pending, ctx.Err()
		default:
		}
		key := embeddingpkg.CacheKey(plan.Model, plan.Dimensions, chunk.Text)
		entry, err := cache.GetEmbedding(ctx, key)
		status := "cached"
		reason := ""
		if err != nil {
			status = "error"
			reason = err.Error()
			plan.ErrorChunks++
		} else if entry == nil || len(entry.Values) == 0 {
			status = "missing"
			reason = "embedding cache entry not found for current model namespace"
			plan.MissingChunks++
		} else if entry.ContentHash != "" && entry.ContentHash != chunk.ContentHash {
			status = "stale"
			reason = "cached embedding content hash differs from current chunk"
			plan.StaleChunks++
		} else {
			plan.CachedChunks++
			nsKey := fmt.Sprintf("%s\x00%d", entry.Model, entry.Dimensions)
			ns := namespaceCounts[nsKey]
			if ns == nil {
				ns = &EmbeddingPlanNamespace{Model: entry.Model, Dimensions: entry.Dimensions}
				namespaceCounts[nsKey] = ns
			}
			ns.Chunks++
		}
		if status != "cached" {
			item := EmbeddingPlanItem{
				Key:         key,
				Status:      status,
				Reason:      reason,
				Kind:        chunk.Kind,
				Path:        chunk.Target.Path,
				Name:        chunk.Target.Name,
				Line:        chunk.Target.Line,
				ContentHash: chunk.ContentHash,
			}
			addItem(item)
			pending = append(pending, embeddingPendingChunk{Item: item, Chunk: chunk})
		}
	}
	plan.BackfillRequired = plan.MissingChunks > 0 || plan.StaleChunks > 0 || plan.ErrorChunks > 0
	namespaces := make([]EmbeddingPlanNamespace, 0, len(namespaceCounts))
	for _, ns := range namespaceCounts {
		namespaces = append(namespaces, *ns)
	}
	sort.Slice(namespaces, func(i, j int) bool {
		if namespaces[i].Model != namespaces[j].Model {
			return namespaces[i].Model < namespaces[j].Model
		}
		return namespaces[i].Dimensions < namespaces[j].Dimensions
	})
	plan.Namespaces = namespaces
	if plan.BackfillRequired {
		plan.Summary = fmt.Sprintf("embedding backfill required for %d/%d chunks in namespace %s/%d (%d missing, %d stale, %d errors)", plan.MissingChunks+plan.StaleChunks+plan.ErrorChunks, plan.TotalChunks, plan.Model, plan.Dimensions, plan.MissingChunks, plan.StaleChunks, plan.ErrorChunks)
	} else {
		plan.Summary = fmt.Sprintf("embedding cache is complete for %d chunks in namespace %s/%d", plan.TotalChunks, plan.Model, plan.Dimensions)
	}
	return plan, pending, nil
}

func firstNonEmptyEngine(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func mergeStringMapsEngine(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			if strings.TrimSpace(v) == "" {
				continue
			}
			out[k] = v
		}
	}
	return out
}

func (e *Engine) embeddingPlanChunks(ctx context.Context) ([]embeddingpkg.Chunk, error) {
	files, err := e.store.ListFiles(ctx, nil)
	if err != nil {
		return nil, err
	}
	chunks := make([]embeddingpkg.Chunk, 0)
	for _, f := range files {
		select {
		case <-ctx.Done():
			return chunks, ctx.Err()
		default:
		}
		content, err := os.ReadFile(filepath.Join(e.root, f.Path))
		if err != nil {
			continue
		}
		syms, err := e.store.GetFileSymbols(ctx, f.Path)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, embeddingpkg.BuildSymbolChunks("", f.Path, content, syms)...)
	}
	docs, err := e.store.ListDocuments(ctx)
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		select {
		case <-ctx.Done():
			return chunks, ctx.Err()
		default:
		}
		content, err := os.ReadFile(filepath.Join(e.root, doc.Path))
		if err != nil {
			continue
		}
		chunks = append(chunks, embeddingpkg.BuildDocumentChunks("", doc, content)...)
	}
	return chunks, nil
}

func capabilityNames(caps []store.Capability) []string {
	names := make([]string, 0, len(caps))
	for _, cap := range caps {
		if cap != "" {
			names = append(names, string(cap))
		}
	}
	return names
}

func (e *Engine) Doctor(ctx context.Context) (*api.DoctorReport, error) {
	checks := []api.DoctorCheck{}
	add := func(name, status, msg string) {
		checks = append(checks, api.DoctorCheck{Name: name, Status: status, Message: msg})
	}
	if info, err := os.Stat(e.root); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		add("root", "error", err.Error())
	} else {
		add("root", "ok", e.root)
	}
	if e.dbPath == "" {
		add("database", "warn", "using default database path")
	} else if _, err := os.Stat(e.dbPath); err != nil {
		add("database", "warn", fmt.Sprintf("database file not found yet: %v", err))
	} else {
		add("database", "ok", e.dbPath)
	}
	schema, err := e.store.SchemaStatus(ctx)
	if err != nil {
		add("schema", "error", err.Error())
		schema = &api.SchemaStatus{ExpectedVersion: store.SchemaVersion}
	} else if len(schema.MissingTables) > 0 || len(schema.MissingIndexes) > 0 {
		add("schema", "error", fmt.Sprintf("missing %d tables and %d indexes", len(schema.MissingTables), len(schema.MissingIndexes)))
	} else if !schema.VersionOK {
		add("schema", "error", fmt.Sprintf("schema version mismatch: applied %q, expected %q", schema.AppliedVersion, schema.ExpectedVersion))
	} else {
		add("schema", "ok", fmt.Sprintf("%s applied", schema.AppliedVersion))
	}
	stats, err := e.store.Stats(ctx)
	if err != nil {
		add("stats", "error", err.Error())
	} else {
		if stats.IndexVersion == "" {
			stats.IndexVersion = graphExportVersion
		}
		add("stats", "ok", fmt.Sprintf("%d files, %d symbols, %d imports, %d docs", stats.TotalFiles, stats.TotalSymbols, stats.TotalImports, stats.TotalDocuments))
	}
	freshness, freshnessErr := e.Freshness(ctx, 50)
	if freshnessErr != nil {
		add("freshness", "warn", freshnessErr.Error())
	} else if freshness.Stale {
		add("freshness", "warn", freshness.Summary)
	} else {
		add("freshness", "ok", freshness.Summary)
	}
	providers, providerErr := e.ProviderDiagnostics(ctx)
	if providerErr != nil {
		add("providers", "error", providerErr.Error())
	} else {
		for _, check := range providers.Checks {
			add(check.Kind+"_provider", check.Status, check.Message)
		}
	}
	ok := true
	warns := 0
	for _, c := range checks {
		if c.Status == "error" {
			ok = false
		}
		if c.Status == "warn" {
			warns++
		}
	}
	summary := "doctor passed"
	if !ok {
		summary = "doctor found errors"
	} else if warns > 0 {
		summary = fmt.Sprintf("doctor passed with %d warnings", warns)
	}
	return &api.DoctorReport{OK: ok, Summary: summary, Root: e.root, DatabasePath: e.dbPath, Schema: *schema, Freshness: freshness, Index: stats, Providers: providers, Checks: checks}, nil
}

func (e *Engine) Rebuild(ctx context.Context, verbose bool) (*api.IndexStats, error) {
	if err := e.store.ResetIndex(ctx); err != nil {
		return nil, err
	}
	return e.Index(ctx, verbose)
}

func (e *Engine) PendingFiles(ctx context.Context, limit int) ([]string, error) {
	report, err := e.Freshness(ctx, limit)
	if err != nil {
		return nil, err
	}
	return freshnessPaths(report.Items), nil
}

func (e *Engine) Freshness(ctx context.Context, limit int) (*api.FreshnessReport, error) {
	files, err := e.store.ListFiles(ctx, nil)
	if err != nil {
		return nil, err
	}
	docs, err := e.store.ListDocuments(ctx)
	if err != nil {
		return nil, err
	}
	report := &api.FreshnessReport{}
	add := func(item api.FreshnessItem) {
		report.PendingCount++
		switch item.Reason {
		case "modified":
			report.ModifiedCount++
		case "deleted":
			report.DeletedCount++
		case "unreadable":
			report.UnreadableCount++
		}
		if limit <= 0 || len(report.Items) < limit {
			report.Items = append(report.Items, item)
		} else {
			report.Truncated = true
		}
	}
	for _, f := range files {
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		default:
		}
		e.checkFreshnessPath(f.Path, "source", f.ContentHash, add)
	}
	for _, d := range docs {
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		default:
		}
		e.checkFreshnessPath(d.Path, "document", d.ContentHash, add)
	}
	report.Stale = report.PendingCount > 0
	if report.Stale {
		report.Summary = fmt.Sprintf("%d pending indexed items: %d modified, %d deleted, %d unreadable", report.PendingCount, report.ModifiedCount, report.DeletedCount, report.UnreadableCount)
	} else {
		report.Summary = "index matches indexed source and document files on disk"
	}
	return report, nil
}

func (e *Engine) checkFreshnessPath(path, kind, indexedHash string, add func(api.FreshnessItem)) {
	content, err := os.ReadFile(filepath.Join(e.root, path))
	if err != nil {
		reason := "unreadable"
		if os.IsNotExist(err) {
			reason = "deleted"
		}
		add(api.FreshnessItem{Path: path, Kind: kind, Reason: reason, IndexedHash: indexedHash, Message: err.Error()})
		return
	}
	fsHash := sha256HexEngine(content)
	if fsHash != indexedHash {
		add(api.FreshnessItem{Path: path, Kind: kind, Reason: "modified", IndexedHash: indexedHash, FilesystemHash: fsHash})
	}
}

func freshnessPaths(items []api.FreshnessItem) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.Path)
	}
	return paths
}

func sha256HexEngine(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func (e *Engine) StartBackgroundWatch(interval, debounce time.Duration, verbose bool) error {
	if interval <= 0 {
		return fmt.Errorf("watch interval must be greater than zero")
	}
	if debounce < 0 {
		return fmt.Errorf("watch debounce must be zero or greater")
	}

	e.watchMu.Lock()
	if e.watchStatus.Running {
		e.watchMu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.watchCancel = cancel
	e.watchMu.Unlock()

	go func() {
		if err := e.RunWatch(ctx, interval, debounce, verbose, nil); err != nil && ctx.Err() == nil {
			e.recordRefresh(nil, err, "watch-background")
		}
	}()
	return nil
}

func (e *Engine) StopBackgroundWatch() {
	e.watchMu.Lock()
	cancel := e.watchCancel
	e.watchCancel = nil
	e.watchMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (e *Engine) RunWatch(ctx context.Context, interval, debounce time.Duration, verbose bool, onRefresh func(*api.IndexStats, error)) error {
	if interval <= 0 {
		return fmt.Errorf("watch interval must be greater than zero")
	}
	if debounce < 0 {
		return fmt.Errorf("watch debounce must be zero or greater")
	}

	e.watchMu.Lock()
	e.watchStatus.Enabled = true
	e.watchStatus.Running = true
	e.watchStatus.Interval = interval.String()
	e.watchStatus.Debounce = debounce.String()
	e.watchStatus.LastError = ""
	e.watchMu.Unlock()
	defer func() {
		e.watchMu.Lock()
		e.watchStatus.Running = false
		e.watchCancel = nil
		e.watchMu.Unlock()
	}()

	stats, err := e.indexer.IndexIncremental(ctx, verbose)
	e.recordRefresh(stats, err, "watch-initial")
	if onRefresh != nil {
		onRefresh(stats, err)
	}
	if err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var nextAllowed time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if time.Now().Before(nextAllowed) {
				continue
			}
			stats, err := e.indexer.IndexIncremental(ctx, verbose)
			e.recordRefresh(stats, err, "watch-refresh")
			if onRefresh != nil {
				onRefresh(stats, err)
			}
			if err != nil {
				nextAllowed = time.Now().Add(debounce)
				continue
			}
			if stats != nil && (stats.IndexedFiles > 0 || stats.FailedFiles > 0) {
				nextAllowed = time.Now().Add(debounce)
			}
		}
	}
}

func (e *Engine) SetWatchConfiguration(enabled bool, interval, debounce time.Duration) {
	e.watchMu.Lock()
	defer e.watchMu.Unlock()
	e.watchStatus.Enabled = enabled
	if interval > 0 {
		e.watchStatus.Interval = interval.String()
	}
	if debounce >= 0 {
		e.watchStatus.Debounce = debounce.String()
	}
}

func (e *Engine) currentWatchStatus() api.WatchStatus {
	e.watchMu.RLock()
	defer e.watchMu.RUnlock()
	status := e.watchStatus
	if status.LastRefreshUnix > 0 && status.LastRefreshAt == "" {
		status.LastRefreshAt = time.Unix(status.LastRefreshUnix, 0).UTC().Format(time.RFC3339)
	}
	return status
}

func (e *Engine) recordRefresh(stats *api.IndexStats, err error, source string) {
	e.watchMu.Lock()
	defer e.watchMu.Unlock()
	now := time.Now().UTC()
	e.watchStatus.LastRefreshUnix = now.Unix()
	e.watchStatus.LastRefreshAt = now.Format(time.RFC3339)
	e.watchStatus.LastRefreshStatus = source
	if err != nil {
		e.watchStatus.LastError = err.Error()
		e.watchStatus.LastRefreshSummary = fmt.Sprintf("%s failed: %v", source, err)
		return
	}
	e.watchStatus.LastError = ""
	e.watchStatus.RefreshCount++
	if stats != nil {
		e.watchStatus.LastRefreshSummary = fmt.Sprintf("%s: %d indexed, %d skipped, %d failed", source, stats.IndexedFiles, stats.SkippedFiles, stats.FailedFiles)
		if stats.LastIndexedUnix == 0 {
			stats.LastIndexedUnix = now.Unix()
			stats.LastIndexedAt = now.Format(time.RFC3339)
		}
		if stats.IndexVersion == "" {
			stats.IndexVersion = graphExportVersion
		}
		return
	}
	e.watchStatus.LastRefreshSummary = source
}

func (e *Engine) ListFiles(ctx context.Context, lang *api.Language) ([]*api.FileInfo, error) {
	return e.store.ListFiles(ctx, lang)
}

type ModuleMap struct {
	Path      string             `json:"path"`
	Files     int                `json:"files"`
	Symbols   int                `json:"symbols"`
	Functions int                `json:"functions"`
	Types     int                `json:"types"`
	Methods   int                `json:"methods"`
	Children  []ModuleMap        `json:"children,omitempty"`
	Analysis  *api.GraphAnalysis `json:"analysis,omitempty"`
}

func (e *Engine) Map(ctx context.Context) (*ModuleMap, error) {
	files, err := e.store.ListFiles(ctx, nil)
	if err != nil {
		return nil, err
	}
	graphExport, _ := e.ExportGraph(ctx, "")

	dirMap := make(map[string]*ModuleMap)

	for _, f := range files {
		syms, err := e.store.GetFileSymbols(ctx, f.Path)
		if err != nil {
			continue
		}

		dir := filepath.Dir(f.Path)

		if _, ok := dirMap[dir]; !ok {
			dirMap[dir] = &ModuleMap{Path: dir}
		}
		m := dirMap[dir]
		m.Files++
		m.Symbols += len(syms)

		for _, s := range syms {
			switch s.Kind {
			case api.Function, api.Variable, api.Constant:
				m.Functions++
			case api.Type, api.Interface:
				m.Types++
			case api.Method:
				m.Methods++
			}
		}
	}

	var collectChildren func(dir string, visited map[string]bool) []string
	collectChildren = func(dir string, visited map[string]bool) []string {
		var children []string
		for d := range dirMap {
			if d == dir {
				continue
			}
			if visited[d] {
				continue
			}
			isChild := false
			if dir == "" {
				isChild = true
			} else {
				isChild = strings.HasPrefix(d, dir+"/")
			}
			if isChild {
				children = append(children, d)
				visited[d] = true
			}
		}
		return children
	}

	var buildTree func(dir string, visited map[string]bool) *ModuleMap
	buildTree = func(dir string, visited map[string]bool) *ModuleMap {
		node := &ModuleMap{Path: dir}
		if m, ok := dirMap[dir]; ok {
			node.Files = m.Files
			node.Symbols = m.Symbols
			node.Functions = m.Functions
			node.Types = m.Types
			node.Methods = m.Methods
		}

		childPaths := collectChildren(dir, visited)
		for _, cp := range childPaths {
			child := buildTree(cp, visited)
			node.Children = append(node.Children, *child)
			node.Files += child.Files
			node.Symbols += child.Symbols
			node.Functions += child.Functions
			node.Types += child.Types
			node.Methods += child.Methods
		}
		return node
	}

	visited := make(map[string]bool)
	root := buildTree("", visited)
	if graphExport != nil {
		root.Analysis = graphExport.Analysis
	}
	return root, nil
}

func (e *Engine) Close() error {
	return e.store.Close()
}

func (e *Engine) Root() string {
	return e.root
}

type FileSummary struct {
	Path             string                      `json:"path"`
	Language         string                      `json:"language"`
	Symbols          []api.Symbol                `json:"symbols"`
	Imports          []api.ImportEdge            `json:"imports"`
	Importers        []api.ImportEdge            `json:"importers,omitempty"`
	RelatedFiles     []string                    `json:"related_files,omitempty"`
	RecommendedFiles []string                    `json:"recommended_files,omitempty"`
	GraphSummary     string                      `json:"graph_summary,omitempty"`
	GraphTraversal   *store.GraphTraversalResult `json:"graph_traversal,omitempty"`
	Analysis         *api.GraphAnalysis          `json:"analysis,omitempty"`
}

func (e *Engine) Explain(ctx context.Context, filePath string) (*FileSummary, error) {
	resolvedFile, _, err := e.resolveGraphFileTarget(ctx, filePath)
	if err == nil {
		filePath = resolvedFile
	}

	fi, err := e.store.GetFile(ctx, filePath)
	if err != nil {
		return nil, err
	}
	if fi == nil {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}

	syms, err := e.store.GetFileSymbols(ctx, filePath)
	if err != nil {
		return nil, err
	}

	imports, err := e.store.GetImports(ctx, filePath)
	if err != nil {
		return nil, err
	}

	var importers []api.ImportEdge
	for _, imp := range imports {
		imprs, err := e.store.GetImporters(ctx, imp.ToSource)
		if err != nil {
			continue
		}
		importers = append(importers, imprs...)
	}

	analysis, relatedFiles, recommendedFiles, graphSummary := e.graphInsightsForFile(ctx, filePath, 3)
	traversal := e.graphTraversalForFile(ctx, filePath, 2)
	return &FileSummary{
		Path:             filePath,
		Language:         string(fi.Language),
		Symbols:          syms,
		Imports:          imports,
		Importers:        importers,
		RelatedFiles:     relatedFiles,
		RecommendedFiles: recommendedFiles,
		GraphSummary:     graphSummary,
		GraphTraversal:   traversal,
		Analysis:         analysis,
	}, nil
}

type SymbolContext struct {
	Definition       api.Symbol                  `json:"definition"`
	Methods          []api.Symbol                `json:"methods,omitempty"`
	Related          []api.Symbol                `json:"related"`
	HybridHits       []store.SearchHit           `json:"hybrid_hits,omitempty"`
	RelatedFiles     []string                    `json:"related_files,omitempty"`
	RecommendedFiles []string                    `json:"recommended_files,omitempty"`
	GraphSummary     string                      `json:"graph_summary,omitempty"`
	GraphTraversal   *store.GraphTraversalResult `json:"graph_traversal,omitempty"`
	Analysis         *api.GraphAnalysis          `json:"analysis,omitempty"`
}

type Snapshot struct {
	Query            string             `json:"query"`
	Files            []FileSummary      `json:"files"`
	Symbols          []api.Symbol       `json:"symbols"`
	HybridHits       []store.SearchHit  `json:"hybrid_hits,omitempty"`
	Summary          string             `json:"summary"`
	RecommendedFiles []string           `json:"recommended_files,omitempty"`
	Analysis         *api.GraphAnalysis `json:"analysis,omitempty"`
}

func (e *Engine) Snapshot(ctx context.Context, query string, maxFiles int) (*Snapshot, error) {
	if maxFiles <= 0 {
		maxFiles = 5
	}
	query = strings.TrimSpace(query)
	if query == "" {
		files, err := e.store.ListFiles(ctx, nil)
		if err != nil {
			return nil, err
		}
		var summaries []FileSummary
		for _, f := range files {
			if len(summaries) >= maxFiles {
				break
			}
			fs, err := e.Explain(ctx, f.Path)
			if err != nil {
				continue
			}
			summaries = append(summaries, *fs)
		}
		recommendedFiles := snapshotRecommendedFiles(summaries, maxFiles)
		summary := fmt.Sprintf("Found %d indexed files for project snapshot", len(summaries))
		if len(recommendedFiles) > 0 {
			summary += fmt.Sprintf(". Recommended next files: %s", strings.Join(recommendedFiles, ", "))
		}
		return &Snapshot{
			Query:            query,
			Files:            summaries,
			Summary:          summary,
			RecommendedFiles: recommendedFiles,
			Analysis:         mergeGraphAnalysesFromFiles(summaries),
		}, nil
	}

	syms, err := e.search.SearchSymbols(ctx, query, nil, 20)
	if err != nil {
		return nil, err
	}
	hybridHits := e.bestEffortHybridSearch(ctx, query, maxFiles*4, nil)

	symbolFileMap := make(map[string]bool)
	var resultSyms []api.Symbol
	for _, s := range syms {
		if !symbolFileMap[s.FilePath] {
			symbolFileMap[s.FilePath] = true
			resultSyms = append(resultSyms, s)
		}
	}

	var files []FileSummary
	fileMap := make(map[string]bool)
	addFileSummary := func(filePath string) {
		filePath = strings.TrimSpace(filePath)
		if filePath == "" || fileMap[filePath] || len(files) >= maxFiles {
			return
		}
		fs, err := e.Explain(ctx, filePath)
		if err != nil {
			return
		}
		files = append(files, *fs)
		fileMap[filePath] = true
	}
	for _, s := range resultSyms {
		if len(files) >= maxFiles {
			break
		}
		addFileSummary(s.FilePath)
	}

	for _, hit := range hybridHits {
		if len(files) >= maxFiles {
			break
		}
		addFileSummary(hit.Target.Path)
	}

	textResults, err := e.search.SearchText(ctx, query, "", 10)
	if err == nil {
		for _, r := range textResults {
			if len(files) >= maxFiles {
				break
			}
			addFileSummary(r.FilePath)
		}
	}

	recommendedFiles := snapshotRecommendedFiles(files, maxFiles)
	summary := fmt.Sprintf("Found %d related files for query '%s': ", len(files), query)
	for i, f := range files {
		if i > 0 {
			summary += ", "
		}
		summary += f.Path
	}
	if len(recommendedFiles) > 0 {
		summary += fmt.Sprintf(". Recommended next files: %s", strings.Join(recommendedFiles, ", "))
	}
	analysis := mergeGraphAnalysesFromFiles(files)

	return &Snapshot{
		Query:            query,
		Files:            files,
		Symbols:          resultSyms,
		HybridHits:       hybridHits,
		Summary:          summary,
		RecommendedFiles: recommendedFiles,
		Analysis:         analysis,
	}, nil
}

func (e *Engine) Context(ctx context.Context, name string) (*SymbolContext, error) {
	defs, err := e.store.FindDefinitions(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		matches, searchErr := e.search.SearchSymbols(ctx, name, nil, 1)
		if searchErr != nil {
			return nil, searchErr
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("symbol not found: %s", name)
		}
		defs = []api.Symbol{matches[0]}
	}

	def := defs[0]
	result := &SymbolContext{
		Definition: def,
	}

	results, err := e.store.FindReferences(ctx, name)
	if err == nil && len(results) > 0 {
		for _, r := range results {
			if r.Kind == api.Method {
				result.Methods = append(result.Methods, r)
			}
		}
	}

	searchResults, err := e.search.SearchSymbols(ctx, name, nil, 20)
	if err == nil {
		for _, s := range searchResults {
			if s.FilePath != def.FilePath || s.Line != def.Line {
				result.Related = append(result.Related, s)
			}
		}
	}

	analysis, relatedFiles, recommendedFiles, graphSummary := e.graphInsightsForFile(ctx, def.FilePath, 5)
	result.RelatedFiles = relatedFiles
	result.RecommendedFiles = recommendedFiles
	result.GraphSummary = fmt.Sprintf("%s. Definition file: %s", graphSummary, def.FilePath)
	result.GraphTraversal = e.graphTraversalForSymbol(ctx, def, 2)
	result.Analysis = analysis
	result.HybridHits = e.bestEffortHybridSearch(ctx, name, 8, []store.TargetRef{symbolTargetRef(def)})
	for _, hit := range result.HybridHits {
		if hit.Target.Path == "" || hit.Target.Path == def.FilePath {
			continue
		}
		if !containsString(result.RelatedFiles, hit.Target.Path) {
			result.RelatedFiles = append(result.RelatedFiles, hit.Target.Path)
		}
		if !containsString(result.RecommendedFiles, hit.Target.Path) {
			result.RecommendedFiles = append(result.RecommendedFiles, hit.Target.Path)
		}
	}
	return result, nil
}

type TraceResult struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Path     []string `json:"path"`
	Files    []string `json:"files"`
	Metadata string   `json:"metadata"`
}

type DiffImpact struct {
	File       string   `json:"file"`
	DirectDeps []string `json:"direct_deps"`
	AllDeps    []string `json:"all_deps"`
	Dependents []string `json:"dependents"`
	Recommends []string `json:"recommends"`
}

type SymbolImpact struct {
	Symbol           api.Symbol                  `json:"symbol"`
	DirectDeps       []string                    `json:"direct_deps,omitempty"`
	Dependents       []string                    `json:"dependents,omitempty"`
	Callers          []api.CallEdge              `json:"callers"`
	Callees          []api.CallEdge              `json:"callees"`
	Routes           []api.Route                 `json:"routes,omitempty"`
	RelatedDocs      []api.DocumentLink          `json:"related_docs,omitempty"`
	RecommendedTests []string                    `json:"recommended_tests,omitempty"`
	GraphTraversal   *store.GraphTraversalResult `json:"graph_traversal,omitempty"`
	Risk             RiskScore                   `json:"risk"`
	Summary          string                      `json:"summary"`
}

type ImpactResult struct {
	Target         string                      `json:"target"`
	Kind           string                      `json:"kind"`
	SymbolImpact   *SymbolImpact               `json:"symbol_impact,omitempty"`
	FileImpact     *DiffImpact                 `json:"file_impact,omitempty"`
	GraphTraversal *store.GraphTraversalResult `json:"graph_traversal,omitempty"`
	Summary        string                      `json:"summary"`
}

type RouteContext struct {
	Query            string                      `json:"query"`
	Routes           []api.Route                 `json:"routes"`
	Handlers         []api.Symbol                `json:"handlers,omitempty"`
	Callers          []api.CallEdge              `json:"callers,omitempty"`
	Callees          []api.CallEdge              `json:"callees,omitempty"`
	RelatedDocs      []api.DocumentLink          `json:"related_docs,omitempty"`
	RecommendedTests []string                    `json:"recommended_tests,omitempty"`
	GraphTraversal   *store.GraphTraversalResult `json:"graph_traversal,omitempty"`
	Risk             RiskScore                   `json:"risk"`
	Summary          string                      `json:"summary"`
}

func (e *Engine) RouteContext(ctx context.Context, query string) (*RouteContext, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("route-context requires a non-empty query")
	}
	routes, err := e.Routes(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("route not found: %s", query)
	}
	filesSeen := map[string]bool{}
	var files []string
	var handlers []api.Symbol
	var changed []ChangedSymbol
	var callers []api.CallEdge
	var callees []api.CallEdge
	for _, route := range routes {
		if route.FilePath != "" && !filesSeen[route.FilePath] {
			filesSeen[route.FilePath] = true
			files = append(files, route.FilePath)
		}
		for _, h := range e.resolveRouteHandler(ctx, route) {
			handlers = append(handlers, h)
			changed = append(changed, ChangedSymbol{Name: h.Name, Kind: string(h.Kind), FilePath: h.FilePath, Line: h.Line})
			cs, _ := e.Callers(ctx, h.Name)
			callers = append(callers, cs...)
			ce, _ := e.Callees(ctx, h.Name)
			callees = append(callees, ce...)
		}
	}
	handlers = dedupSymbols(handlers)
	callers = dedupCallEdges(callers)
	callees = dedupCallEdges(callees)
	docs := e.docsForFilesAndSymbols(ctx, files, changed)
	tests := e.recommendedTestsForFilesAndSymbols(ctx, files, changed)
	risk := routeContextRisk(routes, handlers, callers, tests)
	traversal := e.graphTraversalForRoute(ctx, routes[0], 2)
	return &RouteContext{Query: query, Routes: routes, Handlers: handlers, Callers: callers, Callees: callees, RelatedDocs: docs, RecommendedTests: tests, GraphTraversal: traversal, Risk: risk, Summary: fmt.Sprintf("%d routes, %d handlers, %d callers, %d tests", len(routes), len(handlers), len(callers), len(tests))}, nil
}

func (e *Engine) resolveRouteHandler(ctx context.Context, route api.Route) []api.Symbol {
	handler := strings.TrimSpace(route.Handler)
	if handler == "" {
		return nil
	}
	candidates := []string{handler}
	if i := strings.LastIndex(handler, "."); i >= 0 && i < len(handler)-1 {
		candidates = append(candidates, handler[i+1:])
	}
	seen := map[string]bool{}
	var out []api.Symbol
	for _, name := range candidates {
		defs, err := e.store.FindDefinitions(ctx, name)
		if err != nil {
			continue
		}
		for _, d := range defs {
			if route.FilePath != "" && d.FilePath != route.FilePath {
				continue
			}
			key := d.FilePath + ":" + d.Name + fmt.Sprint(d.Line)
			if !seen[key] {
				seen[key] = true
				out = append(out, d)
			}
		}
	}
	return out
}

func (e *Engine) SymbolImpact(ctx context.Context, name string) (*SymbolImpact, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("symbol-impact requires a non-empty symbol")
	}
	defs, err := e.store.FindDefinitions(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		matches, err := e.search.SearchSymbols(ctx, name, nil, 1)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("symbol not found: %s", name)
		}
		defs = matches
	}
	def := defs[0]
	callers, _ := e.Callers(ctx, def.Name)
	callees, _ := e.Callees(ctx, def.Name)
	directDeps, dependents := e.fileDependencyImpact(ctx, def.FilePath, 2)
	routes := e.routesForFiles(ctx, []string{def.FilePath})
	var symbolRoutes []api.Route
	for _, route := range routes {
		if route.Handler == def.Name || strings.HasSuffix(route.Handler, "."+def.Name) || route.Handler == "" {
			symbolRoutes = append(symbolRoutes, route)
		}
	}
	docs, _ := e.DocsFor(ctx, def.Name)
	relatedDocs := []api.DocumentLink(nil)
	if docs != nil {
		relatedDocs = docs.Links
	}
	tests := e.recommendedTestsForFilesAndSymbols(ctx, []string{def.FilePath}, []ChangedSymbol{{Name: def.Name, Kind: string(def.Kind), FilePath: def.FilePath, Line: def.Line}})
	risk := symbolImpactRisk(def, callers, symbolRoutes, tests, dependents)
	traversal := e.graphTraversalForSymbol(ctx, def, 2)
	return &SymbolImpact{Symbol: def, DirectDeps: directDeps, Dependents: dependents, Callers: callers, Callees: callees, Routes: symbolRoutes, RelatedDocs: relatedDocs, RecommendedTests: tests, GraphTraversal: traversal, Risk: risk, Summary: fmt.Sprintf("%s has %d callers, %d callees, %d dependents, %d routes, %d docs", def.Name, len(callers), len(callees), len(dependents), len(symbolRoutes), len(relatedDocs))}, nil
}

func (e *Engine) Impact(ctx context.Context, target string, depth int) (*ImpactResult, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("impact requires a non-empty target")
	}
	if file, err := e.store.GetFile(ctx, target); err != nil {
		return nil, err
	} else if file != nil {
		impact, err := e.DiffImpact(ctx, target, depth)
		if err != nil {
			return nil, err
		}
		traversal := e.graphTraversalForFile(ctx, target, depth)
		return &ImpactResult{Target: target, Kind: "file", FileImpact: impact, GraphTraversal: traversal, Summary: fmt.Sprintf("file %s has %d dependencies and %d dependents", target, len(impact.AllDeps), len(impact.Dependents))}, nil
	}
	symbolImpact, err := e.SymbolImpact(ctx, target)
	if err != nil {
		return nil, err
	}
	return &ImpactResult{Target: target, Kind: "symbol", SymbolImpact: symbolImpact, Summary: symbolImpact.Summary}, nil
}

func (e *Engine) fileDependencyImpact(ctx context.Context, filePath string, depth int) ([]string, []string) {
	if filePath == "" {
		return nil, nil
	}
	if depth <= 0 {
		depth = 2
	}
	if err := e.graph.Build(ctx); err != nil {
		return nil, nil
	}
	directDeps := e.graph.DirectImports(filePath)
	dependents := e.graph.Dependents(filePath, depth)
	return directDeps, dependents
}

func symbolImpactRisk(def api.Symbol, callers []api.CallEdge, routes []api.Route, tests []string, dependents []string) RiskScore {
	score := 0
	reasons := []string{}
	if len(routes) > 0 {
		score += 30
		reasons = append(reasons, fmt.Sprintf("symbol handles %d routes", len(routes)))
	}
	if len(callers) > 5 {
		score += 20
		reasons = append(reasons, fmt.Sprintf("symbol has %d callers", len(callers)))
	}
	if len(dependents) > 3 {
		score += 15
		reasons = append(reasons, fmt.Sprintf("symbol file has %d dependent files", len(dependents)))
	}
	if len(tests) == 0 {
		score += 15
		reasons = append(reasons, "no related tests found")
	}
	if def.Kind == api.Interface || def.Kind == api.Type || def.Kind == api.Class {
		score += 10
		reasons = append(reasons, fmt.Sprintf("public structural symbol kind: %s", def.Kind))
	}
	level := "low"
	if score >= 50 {
		level = "high"
	} else if score >= 25 {
		level = "medium"
	}
	return RiskScore{Level: level, Score: score, Reasons: dedupStrings(reasons)}
}

func routeContextRisk(routes []api.Route, handlers []api.Symbol, callers []api.CallEdge, tests []string) RiskScore {
	score := 0
	reasons := []string{}
	if len(routes) > 0 {
		score += 25
		reasons = append(reasons, fmt.Sprintf("%d externally reachable routes", len(routes)))
	}
	if len(handlers) == 0 {
		score += 20
		reasons = append(reasons, "route handler definitions were not resolved")
	}
	if len(callers) > 3 {
		score += 15
		reasons = append(reasons, fmt.Sprintf("handlers have %d callers", len(callers)))
	}
	if len(tests) == 0 {
		score += 20
		reasons = append(reasons, "no related tests found")
	}
	level := "low"
	if score >= 50 {
		level = "high"
	} else if score >= 25 {
		level = "medium"
	}
	return RiskScore{Level: level, Score: score, Reasons: dedupStrings(reasons)}
}

func dedupSymbols(in []api.Symbol) []api.Symbol {
	seen := map[string]bool{}
	var out []api.Symbol
	for _, s := range in {
		key := s.FilePath + ":" + s.Name + fmt.Sprint(s.Line)
		if !seen[key] {
			seen[key] = true
			out = append(out, s)
		}
	}
	return out
}

func dedupCallEdges(in []api.CallEdge) []api.CallEdge {
	seen := map[string]bool{}
	var out []api.CallEdge
	for _, c := range in {
		key := c.FromFile + ":" + c.FromSymbol + ":" + c.ToName + fmt.Sprint(c.Line)
		if !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}

func (e *Engine) DiffImpact(ctx context.Context, filePath string, depth int) (*DiffImpact, error) {
	if depth <= 0 {
		depth = 3
	}

	_, err := e.store.GetFile(ctx, filePath)
	if err != nil {
		return nil, err
	}

	err = e.graph.Build(ctx)
	if err != nil {
		return nil, err
	}

	directDeps := e.graph.DirectImports(filePath)
	allDeps := e.graph.Dependencies(filePath, depth)
	dependents := e.graph.Dependents(filePath, depth)

	recSet := make(map[string]bool)
	var recommends []string
	for _, dep := range dependents {
		testFile := dep
		if !strings.HasSuffix(testFile, "_test.go") {
			testFile = strings.Replace(testFile, ".go", "_test.go", 1)
		}
		if !recSet[testFile] {
			_, err := e.store.GetFile(ctx, testFile)
			if err == nil {
				recSet[testFile] = true
				recommends = append(recommends, testFile)
			}
		}
	}

	return &DiffImpact{
		File:       filePath,
		DirectDeps: directDeps,
		AllDeps:    allDeps,
		Dependents: dependents,
		Recommends: recommends,
	}, nil
}

func (e *Engine) Trace(ctx context.Context, fromSym, toSym string) (*TraceResult, error) {
	fromDefs, err := e.store.FindDefinitions(ctx, fromSym)
	if err != nil || len(fromDefs) == 0 {
		return nil, fmt.Errorf("symbol not found: %s", fromSym)
	}

	toDefs, err := e.store.FindDefinitions(ctx, toSym)
	if err != nil || len(toDefs) == 0 {
		return nil, fmt.Errorf("symbol not found: %s", toSym)
	}

	fromFile := fromDefs[0].FilePath
	toFile := toDefs[0].FilePath

	if fromFile == toFile {
		return &TraceResult{
			From:     fromSym,
			To:       toSym,
			Path:     []string{fmt.Sprintf("%s:%d", fromFile, fromDefs[0].Line)},
			Files:    []string{fromFile},
			Metadata: "same file",
		}, nil
	}

	err = e.graph.Build(ctx)
	if err != nil {
		return nil, err
	}

	path := e.graph.TraceFiles(fromFile, toFile, 5)

	var files []string
	var fullPath []string
	for _, f := range path {
		files = append(files, f)
		syms, _ := e.store.GetFileSymbols(ctx, f)
		for _, s := range syms {
			if s.Name == fromSym || s.Name == toSym {
				fullPath = append(fullPath, fmt.Sprintf("%s:%d", f, s.Line))
				break
			}
		}
	}

	return &TraceResult{
		From:     fromSym,
		To:       toSym,
		Path:     fullPath,
		Files:    files,
		Metadata: fmt.Sprintf("found path through %d files", len(files)),
	}, nil
}
