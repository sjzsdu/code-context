package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/graph"
	"github.com/sjzsdu/code-context/internal/indexer"
	"github.com/sjzsdu/code-context/internal/lang"
	"github.com/sjzsdu/code-context/internal/parser"
	"github.com/sjzsdu/code-context/internal/search"
	"github.com/sjzsdu/code-context/internal/store"
)

type Engine struct {
	root    string
	dbPath  string
	store   store.Store
	parser  parser.Parser
	indexer *indexer.Indexer
	search  *search.Searcher
	graph   *graph.Graph
}

func New(root string, dbPath string) (*Engine, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	root, _ = filepath.Abs(root)

	if dbPath == "" {
		dbPath = filepath.Join(root, ".code-context", "index.db")
		os.MkdirAll(filepath.Dir(dbPath), 0o755)
	}

	reg := lang.NewRegistry()
	p := parser.NewTreeSitterParser(reg)
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	if err := s.Init(context.Background()); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	idx := indexer.New(p, s, root)
	sr := search.New(s, root)
	g := graph.New(s)

	return &Engine{
		root:    root,
		dbPath:  dbPath,
		store:   s,
		parser:  p,
		indexer: idx,
		search:  sr,
		graph:   g,
	}, nil
}

func (e *Engine) Index(ctx context.Context, verbose bool) (*api.IndexStats, error) {
	return e.indexer.IndexAll(ctx, verbose)
}

func (e *Engine) IndexIncremental(ctx context.Context, verbose bool) (*api.IndexStats, error) {
	return e.indexer.IndexIncremental(ctx, verbose)
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
			importEdgeCount++
		}

		syms, err := e.store.GetFileSymbols(ctx, f.Path)
		if err != nil {
			return nil, err
		}
		for _, sym := range syms {
			symbolNodeID := fmt.Sprintf("symbol:%s:%s:%d", sym.FilePath, sym.Name, sym.Line)
			nodeMap[symbolNodeID] = api.GraphNode{
				ID:       symbolNodeID,
				Type:     "symbol",
				Label:    sym.Name,
				FilePath: sym.FilePath,
				Name:     sym.Name,
				Kind:     string(sym.Kind),
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
			symbolCount++
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
		Version:  "graph-export.v1",
		Focus:    focus,
		Nodes:    nodes,
		Edges:    edges,
		Summary:  fmt.Sprintf("Exported %d files, %d symbols, and %d import edges for %s", includedFiles, symbolCount, importEdgeCount, scope),
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
		importCounts[strings.TrimPrefix(edge.Target, "import:")]++
		if _, ok := fileImports[edge.Source]; !ok {
			fileImports[edge.Source] = make(map[string]bool)
		}
		fileImports[edge.Source][strings.TrimPrefix(edge.Target, "import:")] = true
	}

	fileCounts := make(map[string]int)
	for sourceID, imports := range fileImports {
		sourceFile := fileNodes[sourceID]
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
			}
		}
	}

	recommendedScores := make(map[string]int)
	if len(focusSet) == 0 {
		for file, count := range fileCounts {
			recommendedScores[file] = count
		}
	} else {
		for focusFile := range focusSet {
			for otherFile, count := range fileCounts {
				if otherFile != focusFile {
					recommendedScores[otherFile] += count
				}
			}
		}
	}

	topImports := topGraphScores(importCounts, 3)
	mostConnected := topGraphScores(fileCounts, 3)
	recommendedItems := topGraphScores(recommendedScores, 3)
	recommended := make([]string, 0, len(recommendedItems))
	for _, item := range recommendedItems {
		recommended = append(recommended, item.Name)
	}
	if len(topImports) == 0 && len(mostConnected) == 0 && len(recommended) == 0 {
		return nil
	}
	return &api.GraphAnalysis{
		TopImports:         topImports,
		MostConnectedFiles: mostConnected,
		RecommendedFiles:   recommended,
	}
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
		RecommendedFiles:   filterStringsBySet(analysis.RecommendedFiles, focus, false),
	}
	if len(result.TopImports) == 0 && len(result.MostConnectedFiles) == 0 && len(result.RecommendedFiles) == 0 {
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

func (e *Engine) graphInsightsForFile(ctx context.Context, filePath string, limit int) (*api.GraphAnalysis, []string, []string, string) {
	graphExport, err := e.ExportGraph(ctx, filePath)
	if err != nil || graphExport == nil {
		return nil, nil, nil, "No graph insights available"
	}
	analysis := graphAnalysisForFiles(graphExport.Analysis, []string{filePath})
	if buildErr := e.graph.Build(ctx); buildErr != nil {
		return analysis, nil, nil, fmt.Sprintf("Graph view covers %d nodes and %d edges", len(graphExport.Nodes), len(graphExport.Edges))
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
	summaryParts := []string{fmt.Sprintf("Graph view covers %d nodes and %d edges", len(graphExport.Nodes), len(graphExport.Edges))}
	if len(related) > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("nearby files: %s", strings.Join(related, ", ")))
	}
	if len(recommended) > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("recommended next files: %s", strings.Join(recommended, ", ")))
	}
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
	recommendedCounts := make(map[string]int)
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
		for _, item := range file.Analysis.RecommendedFiles {
			recommendedCounts[item]++
		}
	}
	recommendedItems := topGraphScores(recommendedCounts, 3)
	recommended := make([]string, 0, len(recommendedItems))
	for _, item := range recommendedItems {
		recommended = append(recommended, item.Name)
	}
	analysis := &api.GraphAnalysis{
		TopImports:         topGraphScores(importCounts, 3),
		MostConnectedFiles: topGraphScores(connectedCounts, 3),
		RecommendedFiles:   recommended,
	}
	if len(analysis.TopImports) == 0 && len(analysis.MostConnectedFiles) == 0 && len(analysis.RecommendedFiles) == 0 {
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

type FileSummary struct {
	Path             string             `json:"path"`
	Language         string             `json:"language"`
	Symbols          []api.Symbol       `json:"symbols"`
	Imports          []api.ImportEdge   `json:"imports"`
	Importers        []api.ImportEdge   `json:"importers,omitempty"`
	RelatedFiles     []string           `json:"related_files,omitempty"`
	RecommendedFiles []string           `json:"recommended_files,omitempty"`
	GraphSummary     string             `json:"graph_summary,omitempty"`
	Analysis         *api.GraphAnalysis `json:"analysis,omitempty"`
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
	return &FileSummary{
		Path:             filePath,
		Language:         string(fi.Language),
		Symbols:          syms,
		Imports:          imports,
		Importers:        importers,
		RelatedFiles:     relatedFiles,
		RecommendedFiles: recommendedFiles,
		GraphSummary:     graphSummary,
		Analysis:         analysis,
	}, nil
}

type SymbolContext struct {
	Definition       api.Symbol         `json:"definition"`
	Methods          []api.Symbol       `json:"methods,omitempty"`
	Related          []api.Symbol       `json:"related"`
	RelatedFiles     []string           `json:"related_files,omitempty"`
	RecommendedFiles []string           `json:"recommended_files,omitempty"`
	GraphSummary     string             `json:"graph_summary,omitempty"`
	Analysis         *api.GraphAnalysis `json:"analysis,omitempty"`
}

type Snapshot struct {
	Query            string             `json:"query"`
	Files            []FileSummary      `json:"files"`
	Symbols          []api.Symbol       `json:"symbols"`
	Summary          string             `json:"summary"`
	RecommendedFiles []string           `json:"recommended_files,omitempty"`
	Analysis         *api.GraphAnalysis `json:"analysis,omitempty"`
}

func (e *Engine) Snapshot(ctx context.Context, query string, maxFiles int) (*Snapshot, error) {
	if maxFiles <= 0 {
		maxFiles = 5
	}

	syms, err := e.search.SearchSymbols(ctx, query, nil, 20)
	if err != nil {
		return nil, err
	}

	fileMap := make(map[string]bool)
	var resultSyms []api.Symbol
	for _, s := range syms {
		if !fileMap[s.FilePath] {
			fileMap[s.FilePath] = true
			resultSyms = append(resultSyms, s)
		}
	}

	var files []FileSummary
	count := 0
	for _, s := range resultSyms {
		if count >= maxFiles {
			break
		}
		fs, err := e.Explain(ctx, s.FilePath)
		if err != nil {
			continue
		}
		files = append(files, *fs)
		count++
	}

	textResults, err := e.search.SearchText(ctx, query, "", 10)
	if err == nil {
		for _, r := range textResults {
			if !fileMap[r.FilePath] {
				fileMap[r.FilePath] = true
				fs, err := e.Explain(ctx, r.FilePath)
				if err != nil {
					continue
				}
				files = append(files, *fs)
				if len(files) >= maxFiles {
					break
				}
			}
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
	result.Analysis = analysis
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
