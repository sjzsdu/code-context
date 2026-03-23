package lang

import "github.com/smacker/go-tree-sitter/javascript"

func javascriptLangDef() *LanguageDef {
	return &LanguageDef{
		Name:       "javascript",
		Extensions: []string{".js", ".jsx", ".mjs"},
		TSLanguage: javascript.GetLanguage(),
		SymbolQueries: []SymbolQuery{
			{Kind: "function", Pattern: `(function_declaration name: (identifier) @name) @definition`},
			{Kind: "function", Pattern: `(lexical_declaration (variable_declarator name: (identifier) @name value: (arrow_function))) @definition`},
			{Kind: "class", Pattern: `(class_declaration name: (identifier) @name) @definition`},
			{Kind: "method", Pattern: `(method_definition name: (property_identifier) @name) @definition`},
			{Kind: "variable", Pattern: `(lexical_declaration (variable_declarator name: (identifier) @name) @definition)`},
		},
		ImportQuery: `(import_statement source: (string) @path) @definition`,
	}
}
