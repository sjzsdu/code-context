package graph

import (
	"context"
	"sort"

	"github.com/sjzsdu/code-memory/internal/store"
)

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

func (g *Graph) Dependents(source string, depth int) []string {
	if depth <= 0 {
		depth = 10
	}
	return dedup(g.bfs(g.reverse, source, depth))
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

	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range scores {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	var result []string
	for i, item := range sorted {
		if i >= topN {
			break
		}
		result = append(result, item.Key)
	}
	return result
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
