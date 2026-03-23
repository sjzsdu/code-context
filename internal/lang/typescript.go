package lang

import "github.com/smacker/go-tree-sitter/typescript/typescript"

func typescriptLangDef() *LanguageDef {
	return &LanguageDef{
		Name:       "typescript",
		Extensions: []string{".ts", ".tsx"},
		TSLanguage: typescript.GetLanguage(),
		SymbolQueries: []SymbolQuery{
			{Kind: "function", Pattern: `(function_declaration name: (identifier) @name) @definition`},
			{Kind: "function", Pattern: `(lexical_declaration (variable_declarator name: (identifier) @name value: (arrow_function))) @definition`},
			{Kind: "class", Pattern: `(class_declaration name: (type_identifier) @name) @definition`},
			{Kind: "method", Pattern: `(method_definition name: (property_identifier) @name) @definition`},
			{Kind: "interface", Pattern: `(interface_declaration name: (type_identifier) @name) @definition`},
			{Kind: "type", Pattern: `(type_alias_declaration name: (type_identifier) @name) @definition`},
		},
		ImportQuery: `(import_statement source: (string) @path) @definition`,
	}
}
