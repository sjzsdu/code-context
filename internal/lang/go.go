package lang

import "github.com/smacker/go-tree-sitter/golang"

func goLangDef() *LanguageDef {
	return &LanguageDef{
		Name:       "go",
		Extensions: []string{".go"},
		TSLanguage: golang.GetLanguage(),
		SymbolQueries: []SymbolQuery{
			{Kind: "function", Pattern: `(function_declaration name: (identifier) @name) @definition`},
			{Kind: "method", Pattern: `(method_declaration name: (field_identifier) @name) @definition`},
			{Kind: "type", Pattern: `(type_spec name: (type_identifier) @name) @definition`},
			{Kind: "variable", Pattern: `(var_spec name: (identifier) @name) @definition`},
			{Kind: "constant", Pattern: `(const_spec name: (identifier) @name) @definition`},
			{Kind: "package", Pattern: `(package_clause (package_identifier) @name) @definition`},
		},
		ImportQuery: `(import_spec path: (interpreted_string_literal) @path) @definition`,
	}
}
