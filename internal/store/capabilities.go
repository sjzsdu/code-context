package store

import (
	"context"
	"sort"
	"time"
)

// Capability names optional advanced provider features without tying callers to
// a concrete backend such as HelixDB. Implementations can report these
// explicitly via CapabilityReporter, or they can be detected by interface.
type Capability string

const (
	CapabilityTextSearch      Capability = "text_search"
	CapabilityVectorSearch    Capability = "vector_search"
	CapabilityHybridSearch    Capability = "hybrid_search"
	CapabilityGraphTraversal  Capability = "graph_traversal"
	CapabilityWorkspaceSearch Capability = "workspace_search"
	CapabilityMemory          Capability = "memory"
	CapabilityEmbedding       Capability = "embedding"
)

// CapabilityReporter lets backends advertise custom or partial advanced
// capabilities. It is optional; DetectCapabilities also uses type assertions.
type CapabilityReporter interface {
	Capabilities() []Capability
}

// DetectCapabilities returns a stable list of advanced capabilities supported
// by provider. It accepts any provider, not just Store, so future retrieval or
// memory providers can live outside the core storage implementation.
func DetectCapabilities(provider any) []Capability {
	if provider == nil {
		return nil
	}

	seen := map[Capability]struct{}{}
	add := func(c Capability) {
		if c != "" {
			seen[c] = struct{}{}
		}
	}

	if reporter, ok := provider.(CapabilityReporter); ok {
		for _, c := range reporter.Capabilities() {
			add(c)
		}
	}
	if _, ok := provider.(TextSearcher); ok {
		add(CapabilityTextSearch)
	}
	if _, ok := provider.(VectorSearcher); ok {
		add(CapabilityVectorSearch)
	}
	if _, ok := provider.(HybridSearcher); ok {
		add(CapabilityHybridSearch)
	}
	if _, ok := provider.(GraphTraverser); ok {
		add(CapabilityGraphTraversal)
	}
	if _, ok := provider.(WorkspaceSearcher); ok {
		add(CapabilityWorkspaceSearch)
	}
	if _, ok := provider.(MemoryStore); ok {
		add(CapabilityMemory)
	}
	if _, ok := provider.(Embedder); ok {
		add(CapabilityEmbedding)
	}

	caps := make([]Capability, 0, len(seen))
	for c := range seen {
		caps = append(caps, c)
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i] < caps[j] })
	return caps
}

// TargetKind identifies the kind of code-context object referenced by search,
// graph, workspace, or memory results.
type TargetKind string

const (
	TargetFile     TargetKind = "file"
	TargetSymbol   TargetKind = "symbol"
	TargetRoute    TargetKind = "route"
	TargetDocument TargetKind = "document"
	TargetText     TargetKind = "text"
	TargetMemory   TargetKind = "memory"
)

// TargetRef is the provider-neutral reference used by advanced capabilities.
// It intentionally avoids Helix labels, node ids, or traversal details.
type TargetRef struct {
	ProjectID string     `json:"project_id,omitempty"`
	Kind      TargetKind `json:"kind,omitempty"`

	Path    string `json:"path,omitempty"`
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	Line    int    `json:"line,omitempty"`
	EndLine int    `json:"end_line,omitempty"`

	Method    string `json:"method,omitempty"`
	RoutePath string `json:"route_path,omitempty"`
	Value     string `json:"value,omitempty"`
}

type SearchFilter struct {
	ProjectIDs  []string          `json:"project_ids,omitempty"`
	TargetKinds []TargetKind      `json:"target_kinds,omitempty"`
	FilePattern string            `json:"file_pattern,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type TextSearchQuery struct {
	Query  string       `json:"query"`
	Filter SearchFilter `json:"filter,omitempty"`
	Limit  int          `json:"limit,omitempty"`
	Offset int          `json:"offset,omitempty"`
}

type VectorSearchQuery struct {
	Vector    []float32    `json:"vector"`
	QueryText string       `json:"query_text,omitempty"`
	Filter    SearchFilter `json:"filter,omitempty"`
	Limit     int          `json:"limit,omitempty"`
	Offset    int          `json:"offset,omitempty"`
}

type HybridSearchQuery struct {
	Query          string       `json:"query"`
	Vector         []float32    `json:"vector,omitempty"`
	Filter         SearchFilter `json:"filter,omitempty"`
	Limit          int          `json:"limit,omitempty"`
	Offset         int          `json:"offset,omitempty"`
	TextWeight     float64      `json:"text_weight,omitempty"`
	VectorWeight   float64      `json:"vector_weight,omitempty"`
	GraphWeight    float64      `json:"graph_weight,omitempty"`
	ExpandFrom     []TargetRef  `json:"expand_from,omitempty"`
	ExpandMaxDepth int          `json:"expand_max_depth,omitempty"`
}

type SearchSource string

const (
	SearchSourceText   SearchSource = "text"
	SearchSourceVector SearchSource = "vector"
	SearchSourceGraph  SearchSource = "graph"
	SearchSourceHybrid SearchSource = "hybrid"
)

type SearchHighlight struct {
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type SearchHit struct {
	Target     TargetRef         `json:"target"`
	Score      float64           `json:"score,omitempty"`
	Source     SearchSource      `json:"source,omitempty"`
	Evidence   string            `json:"evidence,omitempty"`
	Highlights []SearchHighlight `json:"highlights,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type TextSearcher interface {
	SearchText(ctx context.Context, query TextSearchQuery) ([]SearchHit, error)
}

type VectorSearcher interface {
	SearchVector(ctx context.Context, query VectorSearchQuery) ([]SearchHit, error)
}

type HybridSearcher interface {
	SearchHybrid(ctx context.Context, query HybridSearchQuery) ([]SearchHit, error)
}

type GraphDirection string

const (
	GraphOutbound GraphDirection = "outbound"
	GraphInbound  GraphDirection = "inbound"
	GraphBoth     GraphDirection = "both"
)

type GraphEdgeKind string

const (
	GraphEdgeDefines    GraphEdgeKind = "defines"
	GraphEdgeImports    GraphEdgeKind = "imports"
	GraphEdgeCalls      GraphEdgeKind = "calls"
	GraphEdgeRoutes     GraphEdgeKind = "routes"
	GraphEdgeDocuments  GraphEdgeKind = "documents"
	GraphEdgeReferences GraphEdgeKind = "references"
	GraphEdgeSimilar    GraphEdgeKind = "similar"
)

type GraphTraversalQuery struct {
	Start     TargetRef       `json:"start"`
	EdgeKinds []GraphEdgeKind `json:"edge_kinds,omitempty"`
	Direction GraphDirection  `json:"direction,omitempty"`
	MaxDepth  int             `json:"max_depth,omitempty"`
	Limit     int             `json:"limit,omitempty"`
}

type GraphNode struct {
	Target     TargetRef         `json:"target"`
	Score      float64           `json:"score,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type GraphEdge struct {
	From       TargetRef         `json:"from"`
	To         TargetRef         `json:"to"`
	Kind       GraphEdgeKind     `json:"kind"`
	Weight     float64           `json:"weight,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type GraphTraversalResult struct {
	Nodes []GraphNode `json:"nodes,omitempty"`
	Edges []GraphEdge `json:"edges,omitempty"`
}

type GraphTraverser interface {
	TraverseGraph(ctx context.Context, query GraphTraversalQuery) (*GraphTraversalResult, error)
}

type WorkspaceSearchQuery struct {
	Query      string       `json:"query"`
	ProjectIDs []string     `json:"project_ids,omitempty"`
	Filter     SearchFilter `json:"filter,omitempty"`
	Limit      int          `json:"limit,omitempty"`
}

type WorkspaceSearcher interface {
	SearchWorkspace(ctx context.Context, query WorkspaceSearchQuery) ([]SearchHit, error)
}

type MemoryRecord struct {
	ID        string            `json:"id,omitempty"`
	ProjectID string            `json:"project_id,omitempty"`
	Type      string            `json:"type,omitempty"`
	Title     string            `json:"title,omitempty"`
	Content   string            `json:"content"`
	Summary   string            `json:"summary,omitempty"`
	Targets   []TargetRef       `json:"targets,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

type MemorySearchQuery struct {
	Query     string       `json:"query"`
	Vector    []float32    `json:"vector,omitempty"`
	Filter    SearchFilter `json:"filter,omitempty"`
	Limit     int          `json:"limit,omitempty"`
	ProjectID string       `json:"project_id,omitempty"`
}

type MemoryHit struct {
	Memory   MemoryRecord      `json:"memory"`
	Score    float64           `json:"score,omitempty"`
	Source   SearchSource      `json:"source,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type MemoryStore interface {
	UpsertMemory(ctx context.Context, memory MemoryRecord) (string, error)
	SearchMemory(ctx context.Context, query MemorySearchQuery) ([]MemoryHit, error)
}

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}
