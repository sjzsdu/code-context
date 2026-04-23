package graph

import (
	"context"
	"sort"

	"github.com/sjzsdu/code-context/internal/store"
)

type scoreItem struct {
	Key   string
	Value int
}

type Graph struct {
	store   store.Store
	forward map[string][]string
	reverse map[string][]string
}

func New(s store.Store) *Graph {
	return &Graph{
		store:   s,
		forward: make(map[string][]string),
		reverse: make(map[string][]string),
	}
}

func (g *Graph) Build(ctx context.Context) error {
	files, err := g.store.ListFiles(ctx, nil)
	if err != nil {
		return err
	}
	g.forward = make(map[string][]string)
	g.reverse = make(map[string][]string)

	for _, f := range files {
		edges, err := g.store.GetImports(ctx, f.Path)
		if err != nil {
			continue
		}
		for _, e := range edges {
			g.forward[f.Path] = append(g.forward[f.Path], e.ToSource)
			g.reverse[e.ToSource] = append(g.reverse[e.ToSource], f.Path)
		}
	}
	return nil
}

func (g *Graph) DirectImports(file string) []string {
	return dedup(g.forward[file])
}

func (g *Graph) DirectImporters(source string) []string {
	return dedup(g.reverse[source])
}

func (g *Graph) Dependencies(file string, depth int) []string {
	if depth <= 0 {
		depth = 10
	}
	return dedup(g.bfs(g.forward, file, depth))
}

func (g *Graph) Dependents(file string, depth int) []string {
	if depth <= 0 {
		depth = 10
	}

	visited := make(map[string]bool)
	visited[file] = true
	queue := []string{file}
	var result []string

	for d := 0; d < depth && len(queue) > 0; d++ {
		var next []string
		for _, f := range queue {
			var importers []string
			if _, ok := g.forward[f]; ok {
				imports := g.forward[f]
				for _, imp := range imports {
					importers = append(importers, g.reverse[imp]...)
				}
			} else {
				importers = g.reverse[f]
			}
			for _, impFile := range importers {
				if !visited[impFile] {
					visited[impFile] = true
					result = append(result, impFile)
					next = append(next, impFile)
				}
			}
		}
		queue = next
	}

	return dedup(result)
}

func (g *Graph) Related(file string, topN int) []string {
	if topN <= 0 {
		topN = 10
	}
	myImports := g.forward[file]
	impSet := make(map[string]bool)
	for _, imp := range myImports {
		impSet[imp] = true
	}

	sorted := sortScores(g.relatedScores(file, impSet))

	var result []string
	for i, item := range sorted {
		if i >= topN {
			break
		}
		result = append(result, item.Key)
	}
	return result
}

func (g *Graph) RelatedScores(file string) map[string]int {
	impSet := make(map[string]bool)
	for _, imp := range g.forward[file] {
		impSet[imp] = true
	}
	return g.relatedScores(file, impSet)
}

func (g *Graph) ImportCounts() map[string]int {
	counts := make(map[string]int)
	for _, imports := range g.forward {
		for _, imp := range dedup(imports) {
			counts[imp]++
		}
	}
	return counts
}

func (g *Graph) FileConnectionCounts() map[string]int {
	counts := make(map[string]int)
	for file := range g.forward {
		scores := g.RelatedScores(file)
		for relatedFile := range scores {
			counts[file]++
			counts[relatedFile]++
		}
	}
	return counts
}

func (g *Graph) FileNeighbors(file string) []string {
	scores := g.RelatedScores(file)
	items := sortScores(scores)
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Key)
	}
	return result
}

func (g *Graph) SubgraphFiles(start string, depth int) []string {
	if depth <= 0 {
		depth = 1
	}
	if start == "" {
		return nil
	}
	visited := map[string]bool{start: true}
	queue := []string{start}

	for level := 0; level < depth && len(queue) > 0; level++ {
		var next []string
		for _, file := range queue {
			for _, neighbor := range g.FileNeighbors(file) {
				if visited[neighbor] {
					continue
				}
				visited[neighbor] = true
				next = append(next, neighbor)
			}
		}
		queue = next
	}

	result := make([]string, 0, len(visited))
	for file := range visited {
		result = append(result, file)
	}
	sort.Strings(result)
	return result
}

func (g *Graph) relatedScores(file string, impSet map[string]bool) map[string]int {
	scores := make(map[string]int)
	for otherFile, otherImports := range g.forward {
		if otherFile == file {
			continue
		}
		count := 0
		for _, imp := range otherImports {
			if impSet[imp] {
				count++
			}
		}
		if count > 0 {
			scores[otherFile] = count
		}
	}
	return scores
}

func sortScores(scores map[string]int) []scoreItem {
	var sorted []scoreItem
	for k, v := range scores {
		sorted = append(sorted, scoreItem{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Value != sorted[j].Value {
			return sorted[i].Value > sorted[j].Value
		}
		return sorted[i].Key < sorted[j].Key
	})
	return sorted
}

func (g *Graph) bfs(adj map[string][]string, start string, depth int) []string {
	visited := make(map[string]bool)
	visited[start] = true
	queue := []string{start}
	var result []string

	for d := 0; d < depth && len(queue) > 0; d++ {
		var next []string
		for _, node := range queue {
			for _, neighbor := range adj[node] {
				if !visited[neighbor] {
					visited[neighbor] = true
					result = append(result, neighbor)
					next = append(next, neighbor)
				}
			}
		}
		queue = next
	}
	return result
}

func dedup(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}

func (g *Graph) TraceFiles(from, to string, maxDepth int) []string {
	if from == to {
		return []string{from}
	}
	if maxDepth <= 0 {
		maxDepth = 6
	}

	type pathNode struct {
		file string
		path []string
	}

	queue := []pathNode{{file: from, path: []string{from}}}
	visited := make(map[string]bool)
	visited[from] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if len(current.path) >= maxDepth {
			continue
		}

		for _, neighbor := range g.FileNeighbors(current.file) {
			if neighbor == to {
				return append(current.path, neighbor)
			}
			if !visited[neighbor] {
				visited[neighbor] = true
				newPath := make([]string, len(current.path))
				copy(newPath, current.path)
				queue = append(queue, pathNode{file: neighbor, path: append(newPath, neighbor)})
			}
		}
	}

	return nil
}
