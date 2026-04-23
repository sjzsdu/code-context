package api

// SymbolKind represents the type of a code symbol.
type SymbolKind string

const (
	Function  SymbolKind = "function"
	Method    SymbolKind = "method"
	Class     SymbolKind = "class"
	Type      SymbolKind = "type"
	Interface SymbolKind = "interface"
	Variable  SymbolKind = "variable"
	Constant  SymbolKind = "constant"
	Module    SymbolKind = "module"
	Import    SymbolKind = "import"
	Package   SymbolKind = "package"
)

// Language represents a programming language.
type Language string

const (
	Go         Language = "go"
	TypeScript Language = "typescript"
	JavaScript Language = "javascript"
	Python     Language = "python"
	Rust       Language = "rust"
	Java       Language = "java"
)

// AllLanguages returns all supported languages.
func AllLanguages() []Language {
	return []Language{Go, TypeScript, JavaScript, Python, Rust, Java}
}

// Symbol represents a code symbol (function, type, etc.).
type Symbol struct {
	Name      string     `json:"name"`
	Kind      SymbolKind `json:"kind"`
	FilePath  string     `json:"file"`
	Line      int        `json:"line"`
	EndLine   int        `json:"end_line"`
	Signature string     `json:"signature,omitempty"`
	Parent    string     `json:"parent,omitempty"` // enclosing class/struct
}

// FileInfo represents an indexed source file.
type FileInfo struct {
	Path        string   `json:"path"`
	Language    Language `json:"language"`
	ContentHash string   `json:"hash"`
	Size        int64    `json:"size"`
}

// ImportEdge represents an import dependency.
type ImportEdge struct {
	FromFile string `json:"from"`
	ToSource string `json:"to"`
	Line     int    `json:"line"`
}

// GraphNode represents an exported graph node.
type GraphNode struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	FilePath string   `json:"file,omitempty"`
	Name     string   `json:"name,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Language Language `json:"language,omitempty"`
	Line     int      `json:"line,omitempty"`
}

// GraphEdge represents an exported graph edge.
type GraphEdge struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Type       string `json:"type"`
	Evidence   string `json:"evidence,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Line       int    `json:"line,omitempty"`
}

// GraphExport represents a graph export payload.
type GraphExport struct {
	Version  string         `json:"version"`
	Focus    string         `json:"focus,omitempty"`
	Nodes    []GraphNode    `json:"nodes"`
	Edges    []GraphEdge    `json:"edges"`
	Summary  string         `json:"summary"`
	Analysis *GraphAnalysis `json:"analysis,omitempty"`
}

// GraphScoreItem represents a ranked graph metric.
type GraphScoreItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// GraphAnalysis represents derived graph insights for exported graphs.
type GraphAnalysis struct {
	TopImports         []GraphScoreItem `json:"top_imports,omitempty"`
	MostConnectedFiles []GraphScoreItem `json:"most_connected_files,omitempty"`
	RecommendedFiles   []string         `json:"recommended_files,omitempty"`
}

// GraphPathResult represents a navigation path through the graph.
type GraphPathResult struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	FromFile   string   `json:"from_file"`
	ToFile     string   `json:"to_file"`
	Files      []string `json:"files"`
	PathFound  bool     `json:"path_found"`
	Summary    string   `json:"summary"`
	Resolution string   `json:"resolution,omitempty"`
}

// GraphNeighborsResult represents adjacent graph context for a file or symbol.
type GraphNeighborsResult struct {
	Target       string   `json:"target"`
	ResolvedFile string   `json:"resolved_file"`
	Resolution   string   `json:"resolution,omitempty"`
	Symbols      []string `json:"symbols"`
	Imports      []string `json:"imports"`
	RelatedFiles []string `json:"related_files"`
	Summary      string   `json:"summary"`
}

// GraphSubgraphResult represents a local graph export around a file or symbol.
type GraphSubgraphResult struct {
	Target       string       `json:"target"`
	ResolvedFile string       `json:"resolved_file"`
	Resolution   string       `json:"resolution,omitempty"`
	Depth        int          `json:"depth"`
	Graph        *GraphExport `json:"graph"`
	Files        []string     `json:"files"`
	Summary      string       `json:"summary"`
}

// SearchMatch represents a search result.
type SearchMatch struct {
	FilePath string `json:"file"`
	Line     int    `json:"line"`
	Content  string `json:"content"`
	Kind     string `json:"kind,omitempty"`
}

// IndexStats reports indexing results.
type IndexStats struct {
	TotalFiles   int     `json:"total_files"`
	IndexedFiles int     `json:"indexed_files"`
	SkippedFiles int     `json:"skipped_files"`
	FailedFiles  int     `json:"failed_files"`
	TotalSymbols int     `json:"total_symbols"`
	TotalImports int     `json:"total_imports"`
	Duration     float64 `json:"duration_sec"`
}
