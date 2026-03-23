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
