package lang

import "github.com/smacker/go-tree-sitter/java"

func javaLangDef() *LanguageDef {
	return &LanguageDef{
		Name:       "java",
		Extensions: []string{".java"},
		TSLanguage: java.GetLanguage(),
		SymbolQueries: []SymbolQuery{
			{Kind: "class", Pattern: `(class_declaration name: (identifier) @name) @definition`},
			{Kind: "interface", Pattern: `(interface_declaration name: (identifier) @name) @definition`},
			{Kind: "method", Pattern: `(method_declaration name: (identifier) @name) @definition`},
		},
		ImportQuery: `(import_declaration (scoped_identifier) @path) @definition`,
	}
}
