package lang

import "github.com/smacker/go-tree-sitter/python"

func pythonLangDef() *LanguageDef {
	return &LanguageDef{
		Name:       "python",
		Extensions: []string{".py"},
		TSLanguage: python.GetLanguage(),
		SymbolQueries: []SymbolQuery{
			{Kind: "function", Pattern: `(function_definition name: (identifier) @name) @definition`},
			{Kind: "class", Pattern: `(class_definition name: (identifier) @name) @definition`},
			{Kind: "variable", Pattern: `(expression_statement (assignment left: (identifier) @name)) @definition`},
		},
		ImportQuery: `[
			(import_statement name: (dotted_name) @path)
			(import_from_statement module_name: (dotted_name) @path)
		] @definition`,
	}
}
