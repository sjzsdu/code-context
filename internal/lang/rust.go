package lang

import "github.com/smacker/go-tree-sitter/rust"

func rustLangDef() *LanguageDef {
	return &LanguageDef{
		Name:       "rust",
		Extensions: []string{".rs"},
		TSLanguage: rust.GetLanguage(),
		SymbolQueries: []SymbolQuery{
			{Kind: "function", Pattern: `(function_item name: (identifier) @name) @definition`},
			{Kind: "type", Pattern: `(struct_item name: (type_identifier) @name) @definition`},
			{Kind: "type", Pattern: `(enum_item name: (type_identifier) @name) @definition`},
			{Kind: "interface", Pattern: `(trait_item name: (type_identifier) @name) @definition`},
			{Kind: "module", Pattern: `(mod_item name: (identifier) @name) @definition`},
			{Kind: "type", Pattern: `(type_item name: (type_identifier) @name) @definition`},
		},
		ImportQuery: `(use_declaration argument: (_) @path) @definition`,
	}
}
