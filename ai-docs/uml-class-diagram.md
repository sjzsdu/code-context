# code-context UML 类图

## 1. 核心类型类图

```mermaid
classDiagram
    class Symbol {
        +string Name
        +SymbolKind Kind
        +string FilePath
        +int Line
        +int EndLine
        +string Signature
        +string Parent
    }
    
    class SymbolKind {
        <<enumeration>>
        +Function
        +Method
        +Class
        +Type
        +Interface
        +Variable
        +Constant
        +Module
        +Import
        +Package
    }
    
    class Language {
        <<enumeration>>
        +Go
        +TypeScript
        +JavaScript
        +Python
        +Rust
        +Java
    }
    
    class FileInfo {
        +string Path
        +Language Language
        +string ContentHash
        +int64 Size
    }
    
    class ImportEdge {
        +string FromFile
        +string ToSource
        +int Line
    }
    
    class IndexStats {
        +int TotalFiles
        +int IndexedFiles
        +int SkippedFiles
        +int FailedFiles
        +int TotalSymbols
        +int TotalImports
        +float64 Duration
    }
    
    class SearchMatch {
        +string FilePath
        +int Line
        +string Content
        +string Kind
    }
    
    Symbol *-- SymbolKind
    FileInfo *-- Language
```

## 2. 存储层类图

```mermaid
classDiagram
    class Store {
        <<interface>>
        +Init(ctx) error
        +UpsertFile(ctx, *FileInfo) (int64, error)
        +GetFile(ctx, string) (*FileInfo, error)
        +DeleteFile(ctx, string) error
        +ListFiles(ctx, *Language) ([]*FileInfo, error)
        +ReplaceSymbols(ctx, int64, []Symbol) error
        +ReplaceImports(ctx, int64, []ImportEdge) error
        +SearchSymbols(ctx, string, *SymbolKind, int) ([]Symbol, error)
        +FindDefinitions(ctx, string) ([]Symbol, error)
        +FindReferences(ctx, string) ([]Symbol, error)
        +GetFileSymbols(ctx, string) ([]Symbol, error)
        +GetImports(ctx, string) ([]ImportEdge, error)
        +GetImporters(ctx, string) ([]ImportEdge, error)
        +Stats(ctx) (*IndexStats, error)
        +Close() error
    }
    
    class sqliteStore {
        -db *sql.DB
        +Init(ctx) error
        +UpsertFile(ctx, *FileInfo) (int64, error)
        +GetFile(ctx, string) (*FileInfo, error)
        +DeleteFile(ctx, string) error
        +ListFiles(ctx, *Language) ([]*FileInfo, error)
        +ReplaceSymbols(ctx, int64, []Symbol) error
        +ReplaceImports(ctx, int64, []ImportEdge) error
        +SearchSymbols(ctx, string, *SymbolKind, int) ([]Symbol, error)
        +FindDefinitions(ctx, string) ([]Symbol, error)
        +FindReferences(ctx, string) ([]Symbol, error)
        +GetFileSymbols(ctx, string) ([]Symbol, error)
        +GetImports(ctx, string) ([]ImportEdge, error)
        +GetImporters(ctx, string) ([]ImportEdge, error)
        +Stats(ctx) (*IndexStats, error)
        +Close() error
    }
    
    Store <|.. sqliteStore
```

## 3. 解析层类图

```mermaid
classDiagram
    class Parser {
        <<interface>>
        +Parse(ctx, string, []byte, Language) (*ParseResult, error)
        +DetectLanguage(string) (Language, bool)
        +SupportsLanguage(Language) bool
    }
    
    class treeSitterParser {
        -registry *Registry
        +Parse(ctx, string, []byte, Language) (*ParseResult, error)
        +DetectLanguage(string) (Language, bool)
        +SupportsLanguage(Language) bool
    }
    
    class ParseResult {
        +[]Symbol Symbols
        +[]ImportEdge Imports
    }
    
    Parser <|.. treeSitterParser
    treeSitterParser *-- ParseResult
```

## 4. 语言定义类图

```mermaid
classDiagram
    class Registry {
        -mu sync.RWMutex
        -langs map[Language]*LanguageDef
        -extMap map[string]Language
        +Register(*LanguageDef)
        +Get(Language) (*LanguageDef, bool)
        +Detect(string) (Language, bool)
        +Supported() []Language
    }
    
    class LanguageDef {
        +Name Language
        +Extensions []string
        +TSLanguage *sitter.Language
        +SymbolQueries []SymbolQuery
        +ImportQuery string
    }
    
    class SymbolQuery {
        +Kind SymbolKind
        +Pattern string
    }
    
    Registry *-- LanguageDef
    LanguageDef *-- SymbolQuery
```

## 5. 索引器类图

```mermaid
classDiagram
    class Indexer {
        -parser Parser
        -store Store
        -root string
        -workers int
        +IndexAll(ctx, bool) (*IndexStats, error)
        +IndexIncremental(ctx, bool) (*IndexStats, error)
        -walk() ([]string, error)
        -parseAll(ctx, []string, chan parseResult)
        -indexOneFile(ctx, string) (int, int, bool, error)
    }
    
    class parseResult {
        +string path
        +[]byte content
        +Language lang
        +*ParseResult result
        +error err
    }
    
    Indexer *-- parseResult
    parseResult *-- ParseResult
```

## 6. 搜索器类图

```mermaid
classDiagram
    class Searcher {
        -store Store
        -root string
        +SearchSymbols(ctx, string, *SymbolKind, int) ([]Symbol, error)
        +FindDefinition(ctx, string) ([]Symbol, error)
        +FindReferences(ctx, string) ([]Symbol, error)
        +GetFileSymbols(ctx, string) ([]Symbol, error)
        +SearchText(ctx, string, string, int) ([]SearchMatch, error)
    }
```

## 7. 依赖图类图

```mermaid
classDiagram
    class Graph {
        -store Store
        -forward map[string][]string
        -reverse map[string][]string
        +Build(ctx) error
        +DirectImports(string) []string
        +DirectImporters(string) []string
        +Dependencies(string, int) []string
        +Dependents(string, int) []string
        +Related(string, int) []string
        +TraceFiles(string, string, int) []string
    }
```

## 8. 引擎类图

```mermaid
classDiagram
    class Engine {
        -root string
        -dbPath string
        -store Store
        -parser Parser
        -indexer *Indexer
        -search *Searcher
        -graph *Graph
        +New(string, string) (*Engine, error)
        +Index(ctx, bool) (*IndexStats, error)
        +IndexIncremental(ctx, bool) (*IndexStats, error)
        +SearchSymbols(ctx, string, *SymbolKind, int) ([]Symbol, error)
        +FindDef(ctx, string) ([]Symbol, error)
        +FindRefs(ctx, string) ([]Symbol, error)
        +FileSymbols(ctx, string) ([]Symbol, error)
        +SearchText(ctx, string, string, int) ([]SearchMatch, error)
        +Imports(ctx, string) ([]ImportEdge, error)
        +Importers(ctx, string) ([]ImportEdge, error)
        +BuildGraph(ctx) error
        +GraphDeps(string, int) []string
        +GraphRelated(string, int) []string
        +Stats(ctx) (*IndexStats, error)
        +ListFiles(ctx, *Language) ([]*FileInfo, error)
        +Map(ctx) (*ModuleMap, error)
        +Explain(ctx, string) (*FileSummary, error)
        +Context(ctx, string) (*SymbolContext, error)
        +Snapshot(ctx, string, int) (*Snapshot, error)
        +Trace(ctx, string, string) (*TraceResult, error)
        +DiffImpact(ctx, string, int) (*DiffImpact, error)
        +Close() error
    }
    
    class ModuleMap {
        +string Path
        +int Files
        +int Symbols
        +int Functions
        +int Types
        +int Methods
        +[]ModuleMap Children
    }
    
    class FileSummary {
        +string Path
        +string Language
        +[]Symbol Symbols
        +[]ImportEdge Imports
        +[]ImportEdge Importers
    }
    
    class SymbolContext {
        +Symbol Definition
        +[]Symbol Methods
        +[]Symbol Related
    }
    
    class Snapshot {
        +string Query
        +[]FileSummary Files
        +[]Symbol Symbols
        +string Summary
    }
    
    class TraceResult {
        +string From
        +string To
        +[]string Path
        +[]string Files
        +string Metadata
    }
    
    class DiffImpact {
        +string File
        +[]string DirectDeps
        +[]string AllDeps
        +[]string Dependents
        +[]string Recommends
    }
    
    Engine *-- Store
    Engine *-- Parser
    Engine *-- Indexer
    Engine *-- Searcher
    Engine *-- Graph
```

## 9. 服务器类图

```mermaid
classDiagram
    class Server {
        -eng *Engine
        -port int
        +New(*Engine, int) *Server
        +Run() error
        +Handler() http.Handler
    }
    
    class "http.Handler" {
        <<interface>>
        +ServeHTTP(http.ResponseWriter, *http.Request)
    }
    
    Server ..> "http.Handler"
```

## 10. 整体关系图

```mermaid
classDiagram
    Engine --> Parser
    Engine --> Store
    Engine --> Indexer
    Engine --> Searcher
    Engine --> Graph
    
    Indexer --> Parser
    Indexer --> Store
    
    Searcher --> Store
    
    Graph --> Store
    
    Server --> Engine