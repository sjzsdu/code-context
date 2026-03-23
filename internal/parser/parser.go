package parser

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/sjzsdu/code-memory/internal/api"
	"github.com/sjzsdu/code-memory/internal/lang"
)

type ParseResult struct {
	Symbols []api.Symbol
	Imports []api.ImportEdge
}

type Parser interface {
	Parse(ctx context.Context, filePath string, content []byte, language api.Language) (*ParseResult, error)
	DetectLanguage(path string) (api.Language, bool)
	SupportsLanguage(lang api.Language) bool
}

type treeSitterParser struct {
	registry *lang.Registry
}

func NewTreeSitterParser(reg *lang.Registry) Parser {
	return &treeSitterParser{registry: reg}
}

func (p *treeSitterParser) DetectLanguage(path string) (api.Language, bool) {
	return DetectLanguage(path)
}

func (p *treeSitterParser) SupportsLanguage(l api.Language) bool {
	_, ok := p.registry.Get(l)
	return ok
}

func (p *treeSitterParser) Parse(ctx context.Context, filePath string, content []byte, language api.Language) (*ParseResult, error) {
	langDef, ok := p.registry.Get(language)
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(langDef.TSLanguage)

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}
	defer tree.Close()

	root := tree.RootNode()
	result := &ParseResult{}

	for _, qd := range langDef.SymbolQueries {
		symbols, err := execSymbolQuery(qd, root, content, filePath, langDef.TSLanguage)
		if err != nil {
			continue
		}
		result.Symbols = append(result.Symbols, symbols...)
	}

	if langDef.ImportQuery != "" {
		imports, err := execImportQuery(langDef.ImportQuery, root, content, filePath, langDef.TSLanguage)
		if err == nil {
			result.Imports = imports
		}
	}

	return result, nil
}

func execSymbolQuery(qd lang.SymbolQuery, root *sitter.Node, src []byte, file string, tsLang *sitter.Language) ([]api.Symbol, error) {
	q, err := sitter.NewQuery([]byte(qd.Pattern), tsLang)
	if err != nil {
		return nil, err
	}
	defer q.Close()

	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)

	var symbols []api.Symbol
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}
		match = qc.FilterPredicates(match, src)

		var name string
		var defNode *sitter.Node
		for _, cap := range match.Captures {
			capName := q.CaptureNameForId(cap.Index)
			switch capName {
			case "name":
				name = cap.Node.Content(src)
			case "definition":
				defNode = cap.Node
			}
		}
		if name != "" && defNode != nil {
			symbols = append(symbols, api.Symbol{
				Name:     name,
				Kind:     qd.Kind,
				FilePath: file,
				Line:     int(defNode.StartPoint().Row) + 1,
				EndLine:  int(defNode.EndPoint().Row) + 1,
			})
		}
	}
	return symbols, nil
}

func execImportQuery(pattern string, root *sitter.Node, src []byte, file string, tsLang *sitter.Language) ([]api.ImportEdge, error) {
	q, err := sitter.NewQuery([]byte(pattern), tsLang)
	if err != nil {
		return nil, err
	}
	defer q.Close()

	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)

	var imports []api.ImportEdge
	seen := make(map[string]bool)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}
		match = qc.FilterPredicates(match, src)

		for _, cap := range match.Captures {
			capName := q.CaptureNameForId(cap.Index)
			if capName == "path" {
				path := cap.Node.Content(src)
				path = strings.Trim(path, "\"'")
				if path != "" && !seen[path] {
					seen[path] = true
					imports = append(imports, api.ImportEdge{
						FromFile: file,
						ToSource: path,
						Line:     int(cap.Node.StartPoint().Row) + 1,
					})
				}
			}
		}
	}
	return imports, nil
}
