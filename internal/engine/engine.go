package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sjzsdu/code-context/internal/api"
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
		for _, sym := range syms {
			symbolNodeID := fmt.Sprintf("symbol:%s:%s:%d", sym.FilePath, sym.Name, sym.Line)
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
	return &api.ServiceStatus{
		Root:         e.root,
		DatabasePath: e.dbPath,
		GraphVersion: graphExportVersion,
		Index:        stats,
		Watch:        &watch,
	}, nil
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
