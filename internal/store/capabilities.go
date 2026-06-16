package store

import (
	"context"
	"sort"
	"strconv"
	"strings"
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
	CapabilityEmbeddingCache  Capability = "embedding_cache"
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
	if _, ok := provider.(EmbeddingCache); ok {
		add(CapabilityEmbeddingCache)
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
	Target    string          `json:"target,omitempty"`
	EdgeKinds []GraphEdgeKind `json:"edge_kinds,omitempty"`
	Direction GraphDirection  `json:"direction,omitempty"`
	MaxDepth  int             `json:"max_depth,omitempty"`
	Limit     int             `json:"limit,omitempty"`
	Filter    SearchFilter    `json:"filter,omitempty"`
	// IncludePaths asks providers to include shortest traversal paths from the
	// start target to reached nodes when the backend can derive them cheaply.
	IncludePaths bool `json:"include_paths,omitempty"`
}

type GraphNode struct {
	Target     TargetRef         `json:"target"`
	Depth      int               `json:"depth,omitempty"`
	Root       bool              `json:"root,omitempty"`
	Score      float64           `json:"score,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type GraphEdge struct {
	From       TargetRef         `json:"from"`
	To         TargetRef         `json:"to"`
	Kind       GraphEdgeKind     `json:"kind"`
	Depth      int               `json:"depth,omitempty"`
	Weight     float64           `json:"weight,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type GraphTraversalStep struct {
	From      TargetRef      `json:"from"`
	To        TargetRef      `json:"to"`
	EdgeKind  GraphEdgeKind  `json:"edge_kind"`
	Direction GraphDirection `json:"direction,omitempty"`
}

type GraphTraversalPath struct {
	Target TargetRef            `json:"target"`
	Depth  int                  `json:"depth,omitempty"`
	Steps  []GraphTraversalStep `json:"steps,omitempty"`
}

type GraphTraversalResult struct {
	Start     TargetRef            `json:"start,omitempty"`
	Direction GraphDirection       `json:"direction,omitempty"`
	MaxDepth  int                  `json:"max_depth,omitempty"`
	EdgeKinds []GraphEdgeKind      `json:"edge_kinds,omitempty"`
	Nodes     []GraphNode          `json:"nodes,omitempty"`
	Edges     []GraphEdge          `json:"edges,omitempty"`
	Paths     []GraphTraversalPath `json:"paths,omitempty"`
	Summary   string               `json:"summary,omitempty"`
}

type GraphTraverser interface {
	TraverseGraph(ctx context.Context, query GraphTraversalQuery) (*GraphTraversalResult, error)
}

// ParseTargetRef converts a human-friendly graph target into a provider-neutral
// TargetRef. It intentionally supports only stable code-context concepts (not
// backend ids), so it can be reused by CLI, HTTP, MCP, and future providers.
func ParseTargetRef(raw string) TargetRef {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TargetRef{}
	}

	lower := strings.ToLower(raw)
	for _, prefix := range []struct {
		text string
		kind TargetKind
	}{
		{"file:", TargetFile},
		{"document:", TargetDocument},
		{"doc:", TargetDocument},
		{"symbol:", TargetSymbol},
		{"route:", TargetRoute},
		{"text:", TargetText},
	} {
		if strings.HasPrefix(lower, prefix.text) {
			return parseTargetRefBody(prefix.kind, strings.TrimSpace(raw[len(prefix.text):]))
		}
	}

	if method, routePath, ok := parseMethodRoute(raw); ok {
		return TargetRef{Kind: TargetRoute, Method: method, RoutePath: routePath, Value: strings.TrimSpace(method + " " + routePath)}
	}
	if looksLikeDocumentPath(raw) {
		path, line := splitTargetPathLine(raw)
		return TargetRef{Kind: TargetDocument, Path: path, Line: line}
	}
	if looksLikeFilePath(raw) {
		path, line := splitTargetPathLine(raw)
		return TargetRef{Kind: TargetFile, Path: path, Line: line}
	}
	return TargetRef{Kind: TargetSymbol, Name: raw}
}

func parseTargetRefBody(kind TargetKind, body string) TargetRef {
	switch kind {
	case TargetFile:
		path, line := splitTargetPathLine(body)
		return TargetRef{Kind: TargetFile, Path: path, Line: line}
	case TargetDocument:
		path, line := splitTargetPathLine(body)
		return TargetRef{Kind: TargetDocument, Path: path, Line: line}
	case TargetSymbol:
		if before, after, ok := strings.Cut(body, "@"); ok {
			path, line := splitTargetPathLine(strings.TrimSpace(after))
			return TargetRef{Kind: TargetSymbol, Name: strings.TrimSpace(before), Path: path, Line: line}
		}
		if before, after, ok := strings.Cut(body, "#"); ok && looksLikeFilePath(before) {
			path, line := splitTargetPathLine(strings.TrimSpace(before))
			return TargetRef{Kind: TargetSymbol, Name: strings.TrimSpace(after), Path: path, Line: line}
		}
		return TargetRef{Kind: TargetSymbol, Name: body}
	case TargetRoute:
		if method, routePath, ok := parseMethodRoute(body); ok {
			return TargetRef{Kind: TargetRoute, Method: method, RoutePath: routePath, Value: strings.TrimSpace(method + " " + routePath)}
		}
		return TargetRef{Kind: TargetRoute, RoutePath: body, Value: body}
	case TargetText:
		return TargetRef{Kind: TargetText, Value: body}
	default:
		return TargetRef{Kind: kind, Value: body}
	}
}

func parseMethodRoute(raw string) (string, string, bool) {
	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return "", "", false
	}
	method := strings.ToUpper(fields[0])
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE", "CONNECT":
		return method, strings.Join(fields[1:], " "), true
	default:
		return "", "", false
	}
}

func looksLikeDocumentPath(raw string) bool {
	path, _ := splitTargetPathLine(raw)
	lower := strings.ToLower(path)
	for _, suffix := range []string{".md", ".mdx", ".markdown", ".rst", ".adoc", ".txt"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func looksLikeFilePath(raw string) bool {
	path, _ := splitTargetPathLine(raw)
	return strings.Contains(path, "/") || strings.Contains(path, "\\") || strings.HasPrefix(path, ".")
}

func splitTargetPathLine(raw string) (string, int) {
	raw = strings.TrimSpace(raw)
	colon := strings.LastIndex(raw, ":")
	if colon <= 0 || colon == len(raw)-1 {
		return raw, 0
	}
	line, err := strconv.Atoi(raw[colon+1:])
	if err != nil || line <= 0 {
		return raw, 0
	}
	return raw[:colon], line
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

type EmbeddingInputKind string

const (
	EmbeddingInputQuery    EmbeddingInputKind = "query"
	EmbeddingInputDocument EmbeddingInputKind = "document"
	EmbeddingInputCode     EmbeddingInputKind = "code"
	EmbeddingInputSymbol   EmbeddingInputKind = "symbol"
)

// EmbeddingInput is the provider-neutral unit sent to an embedding provider.
// The optional Target and Metadata fields let indexers retain source
// provenance without binding the embedder to any storage backend.
type EmbeddingInput struct {
	ID       string             `json:"id,omitempty"`
	Text     string             `json:"text"`
	Kind     EmbeddingInputKind `json:"kind,omitempty"`
	Target   TargetRef          `json:"target,omitempty"`
	Metadata map[string]string  `json:"metadata,omitempty"`
}

type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

// EmbeddingVector is the provider-neutral output from an embedding provider.
// Values are always float32 so they can be stored directly in vector indexes
// such as Helix nodeVector/edgeVector properties.
type EmbeddingVector struct {
	ID         string            `json:"id,omitempty"`
	Values     []float32         `json:"values"`
	Model      string            `json:"model,omitempty"`
	Dimensions int               `json:"dimensions,omitempty"`
	Target     TargetRef         `json:"target,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Usage      *EmbeddingUsage   `json:"usage,omitempty"`
}

type EmbeddingCacheEntry struct {
	Key         string             `json:"key"`
	Model       string             `json:"model"`
	Dimensions  int                `json:"dimensions"`
	ContentHash string             `json:"content_hash"`
	InputKind   EmbeddingInputKind `json:"input_kind,omitempty"`
	Target      TargetRef          `json:"target,omitempty"`
	Values      []float32          `json:"values"`
	Metadata    map[string]string  `json:"metadata,omitempty"`
	CreatedAt   time.Time          `json:"created_at,omitempty"`
	UpdatedAt   time.Time          `json:"updated_at,omitempty"`
}

type EmbeddingCache interface {
	GetEmbedding(ctx context.Context, key string) (*EmbeddingCacheEntry, error)
	UpsertEmbedding(ctx context.Context, entry EmbeddingCacheEntry) error
}

type EmbeddingModelInfo struct {
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	Dimensions int    `json:"dimensions,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	BatchSize  int    `json:"batch_size,omitempty"`
}

// Embedder produces vectors for one or more inputs. Implementations should
// preserve input order in the returned slice and should not perform retrieval
// or storage writes themselves.
type Embedder interface {
	Embed(ctx context.Context, inputs []EmbeddingInput) ([]EmbeddingVector, error)
	EmbeddingModel() EmbeddingModelInfo
}
