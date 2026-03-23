package lang

import (
	"sync"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/sjzsdu/code-memory/internal/api"
)

// SymbolQuery defines a tree-sitter query for extracting one kind of symbol.
type SymbolQuery struct {
	Kind    api.SymbolKind
	Pattern string // tree-sitter S-expression query
}

// LanguageDef defines how to parse one programming language.
type LanguageDef struct {
	Name          api.Language
	Extensions    []string
	TSLanguage    *sitter.Language
	SymbolQueries []SymbolQuery
	ImportQuery   string
}

// Registry holds all registered language definitions.
type Registry struct {
	mu     sync.RWMutex
	langs  map[api.Language]*LanguageDef
	extMap map[string]api.Language
}

// NewRegistry creates a registry pre-loaded with all supported languages.
func NewRegistry() *Registry {
	r := &Registry{
		langs:  make(map[api.Language]*LanguageDef),
		extMap: make(map[string]api.Language),
	}
	defs := allLanguageDefs()
	for _, d := range defs {
		r.Register(d)
	}
	return r
}

// Register adds a language definition to the registry.
func (r *Registry) Register(def *LanguageDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.langs[def.Name] = def
	for _, ext := range def.Extensions {
		r.extMap[ext] = def.Name
	}
}

// Get returns the language definition for a language, or nil if unsupported.
func (r *Registry) Get(lang api.Language) (*LanguageDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.langs[lang]
	return d, ok
}

// Detect returns the language for a file extension.
func (r *Registry) Detect(ext string) (api.Language, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.extMap[ext]
	return l, ok
}

// allLanguageDefs returns all supported language definitions.
func allLanguageDefs() []*LanguageDef {
	return []*LanguageDef{
		goLangDef(),
		typescriptLangDef(),
		javascriptLangDef(),
		pythonLangDef(),
		rustLangDef(),
		javaLangDef(),
	}
}

// Supported returns all registered languages.
func (r *Registry) Supported() []api.Language {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]api.Language, 0, len(r.langs))
	for k := range r.langs {
		out = append(out, k)
	}
	return out
}
