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
	Markdown   Language = "markdown"
	Text       Language = "text"
)

// AllLanguages returns all supported languages.
func AllLanguages() []Language {
	return []Language{Go, TypeScript, JavaScript, Python, Rust, Java, Markdown, Text}
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
	ContentHash string   `json:"content_hash,omitempty"`
	Size        int64    `json:"size"`
	IndexedAt   int64    `json:"indexed_at,omitempty"`
}

// FileSummary represents a file with its symbols and related data.
type FileSummary struct {
	Path             string         `json:"path"`
	Language         string         `json:"language"`
	Symbols          []Symbol       `json:"symbols"`
	Imports          []ImportEdge   `json:"imports"`
	Importers        []ImportEdge   `json:"importers,omitempty"`
	RelatedFiles     []string       `json:"related_files,omitempty"`
	RelatedDocuments []string       `json:"related_documents,omitempty"`
	RecommendedFiles []string       `json:"recommended_files,omitempty"`
	GraphSummary     string         `json:"graph_summary,omitempty"`
	Analysis         *GraphAnalysis `json:"analysis,omitempty"`
}

type ImportEdge struct {
	FromFile string `json:"from"`
	ToSource string `json:"to"`
	Line     int    `json:"line"`
}

// CallEdge represents a lightweight symbol call relationship extracted from source.
type CallEdge struct {
	FromFile   string `json:"from_file"`
	FromSymbol string `json:"from_symbol"`
	ToName     string `json:"to_name"`
	Line       int    `json:"line"`
	Confidence string `json:"confidence,omitempty"`
}

// Route represents a web framework route entry point linked to a handler.
type Route struct {
	FilePath   string `json:"file"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Handler    string `json:"handler,omitempty"`
	Framework  string `json:"framework,omitempty"`
	Line       int    `json:"line"`
	Confidence string `json:"confidence,omitempty"`
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
	Title    string   `json:"title,omitempty"`
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

// GraphReadingPath represents a suggested graph-guided reading flow.
type GraphReadingPath struct {
	Entry  string   `json:"entry"`
	Path   []string `json:"path"`
	Reason string   `json:"reason"`
}

// GraphAnalysis represents derived graph insights for exported graphs.
type GraphAnalysis struct {
	TopImports         []GraphScoreItem   `json:"top_imports,omitempty"`
	MostConnectedFiles []GraphScoreItem   `json:"most_connected_files,omitempty"`
	BridgeFiles        []GraphScoreItem   `json:"bridge_files,omitempty"`
	HotspotFiles       []GraphScoreItem   `json:"hotspot_files,omitempty"`
	RecommendedFiles   []string           `json:"recommended_files,omitempty"`
	RelationHighlights []string           `json:"relation_highlights,omitempty"`
	ReadingPaths       []GraphReadingPath `json:"reading_paths,omitempty"`
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
	TotalFiles       int     `json:"total_files"`
	IndexedFiles     int     `json:"indexed_files"`
	SkippedFiles     int     `json:"skipped_files"`
	FailedFiles      int     `json:"failed_files"`
	TotalSymbols     int     `json:"total_symbols"`
	TotalImports     int     `json:"total_imports"`
	TotalDocuments   int     `json:"total_documents,omitempty"`
	IndexedDocuments int     `json:"indexed_documents,omitempty"`
	Duration         float64 `json:"duration_sec"`
	LastIndexedUnix  int64   `json:"last_indexed_unix,omitempty"`
	LastIndexedAt    string  `json:"last_indexed_at,omitempty"`
	IndexVersion     string  `json:"index_version,omitempty"`
}

// WatchStatus reports workflow refresh state for watch-enabled processes.
type WatchStatus struct {
	Enabled            bool             `json:"enabled"`
	Running            bool             `json:"running"`
	Stale              bool             `json:"stale,omitempty"`
	PendingFiles       []string         `json:"pending_files,omitempty"`
	Freshness          *FreshnessReport `json:"freshness,omitempty"`
	Interval           string           `json:"interval,omitempty"`
	Debounce           string           `json:"debounce,omitempty"`
	LastRefreshUnix    int64            `json:"last_refresh_unix,omitempty"`
	LastRefreshAt      string           `json:"last_refresh_at,omitempty"`
	LastRefreshStatus  string           `json:"last_refresh_status,omitempty"`
	LastRefreshSummary string           `json:"last_refresh_summary,omitempty"`
	LastError          string           `json:"last_error,omitempty"`
	RefreshCount       int              `json:"refresh_count,omitempty"`
}

// FreshnessItem reports why an indexed source/document needs refresh.
type FreshnessItem struct {
	Path           string `json:"path"`
	Kind           string `json:"kind"`   // source or document
	Reason         string `json:"reason"` // modified, deleted, unreadable
	IndexedHash    string `json:"indexed_hash,omitempty"`
	FilesystemHash string `json:"filesystem_hash,omitempty"`
	Message        string `json:"message,omitempty"`
}

// FreshnessReport summarizes index freshness against the filesystem.
type FreshnessReport struct {
	Stale           bool            `json:"stale"`
	PendingCount    int             `json:"pending_count"`
	ModifiedCount   int             `json:"modified_count,omitempty"`
	DeletedCount    int             `json:"deleted_count,omitempty"`
	UnreadableCount int             `json:"unreadable_count,omitempty"`
	Items           []FreshnessItem `json:"items,omitempty"`
	Truncated       bool            `json:"truncated,omitempty"`
	Summary         string          `json:"summary"`
}

// ServiceStatus reports workflow and indexing metadata for the running service.
type ServiceStatus struct {
	Root         string       `json:"root"`
	DatabasePath string       `json:"database_path"`
	GraphVersion string       `json:"graph_version"`
	Index        *IndexStats  `json:"index,omitempty"`
	Watch        *WatchStatus `json:"watch,omitempty"`
}

// DoctorCheck reports one health check result.
type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok, warn, error
	Message string `json:"message"`
}

// SchemaStatus reports SQLite schema capabilities.
type SchemaStatus struct {
	ExpectedVersion string   `json:"expected_version"`
	AppliedVersion  string   `json:"applied_version,omitempty"`
	VersionOK       bool     `json:"version_ok"`
	Tables          []string `json:"tables"`
	MissingTables   []string `json:"missing_tables,omitempty"`
	Indexes         []string `json:"indexes,omitempty"`
	MissingIndexes  []string `json:"missing_indexes,omitempty"`
}

// DoctorReport summarizes repository/index health.
type DoctorReport struct {
	OK           bool             `json:"ok"`
	Summary      string           `json:"summary"`
	Root         string           `json:"root"`
	DatabasePath string           `json:"database_path"`
	Schema       SchemaStatus     `json:"schema"`
	Freshness    *FreshnessReport `json:"freshness,omitempty"`
	Index        *IndexStats      `json:"index,omitempty"`
	Checks       []DoctorCheck    `json:"checks"`
}

// Document represents a document file.
type Document struct {
	ID          int64  `json:"id"`
	Path        string `json:"path"`
	Language    string `json:"language"`
	ContentHash string `json:"content_hash,omitempty"`
	Title       string `json:"title,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Size        int    `json:"size"`
	IndexedAt   int64  `json:"indexed_at,omitempty"`
}

// DocumentLink represents a relationship between a document and code.
type DocumentLink struct {
	ID           int64   `json:"id"`
	DocumentID   int64   `json:"document_id"`
	DocumentPath string  `json:"document_path,omitempty"`
	TargetType   string  `json:"target_type"`
	TargetValue  string  `json:"target_value"`
	Line         int     `json:"line"`
	SectionTitle string  `json:"section_title,omitempty"`
	SectionSlug  string  `json:"section_slug,omitempty"`
	SectionLine  int     `json:"section_line,omitempty"`
	Evidence     string  `json:"evidence,omitempty"`
	Confidence   float64 `json:"confidence"`
}

// DocReference groups document links for a queried code/document target.
type DocReference struct {
	Query string         `json:"query"`
	Links []DocumentLink `json:"links"`
}

// DocDriftItem reports a document reference that no longer resolves.
type DocDriftItem struct {
	DocumentPath string `json:"document_path"`
	TargetType   string `json:"target_type"`
	TargetValue  string `json:"target_value"`
	Line         int    `json:"line"`
	SectionTitle string `json:"section_title,omitempty"`
	SectionSlug  string `json:"section_slug,omitempty"`
	SectionLine  int    `json:"section_line,omitempty"`
	Evidence     string `json:"evidence,omitempty"`
	Reason       string `json:"reason"`
}

// DocDriftReport summarizes stale or broken document references.
type DocDriftReport struct {
	TotalLinks int            `json:"total_links"`
	Broken     []DocDriftItem `json:"broken"`
	Summary    string         `json:"summary"`
}

// DocCoverageReport summarizes code surfaces that are not referenced by docs.
type DocCoverageReport struct {
	TotalRoutes           int      `json:"total_routes"`
	DocumentedRoutes      int      `json:"documented_routes"`
	MissingRoutes         []Route  `json:"missing_routes"`
	RouteCoveragePercent  float64  `json:"route_coverage_percent"`
	TotalSymbols          int      `json:"total_symbols"`
	DocumentedSymbols     int      `json:"documented_symbols"`
	MissingSymbols        []Symbol `json:"missing_symbols"`
	SymbolCoveragePercent float64  `json:"symbol_coverage_percent"`
	Summary               string   `json:"summary"`
}
