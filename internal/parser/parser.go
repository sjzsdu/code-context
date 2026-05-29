package parser

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/lang"
)

type ParseResult struct {
	Symbols []api.Symbol
	Imports []api.ImportEdge
	Calls   []api.CallEdge
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

	result.Calls = extractCalls(filePath, string(content), language, result.Symbols)

	return result, nil
}

var callPattern = regexp.MustCompile(`(?m)([A-Za-z_][A-Za-z0-9_]*(?:(?:\.|::)[A-Za-z_][A-Za-z0-9_]*)?)\s*\(`)

func extractCalls(file string, content string, language api.Language, symbols []api.Symbol) []api.CallEdge {
	if len(symbols) == 0 {
		return nil
	}
	sorted := append([]api.Symbol(nil), symbols...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Line < sorted[j].Line })
	lineStarts := lineStartOffsets(content)
	seen := make(map[string]bool)
	var calls []api.CallEdge
	for _, match := range callPattern.FindAllStringSubmatchIndex(content, -1) {
		if len(match) < 4 {
			continue
		}
		name := content[match[2]:match[3]]
		if shouldSkipCall(name, language) {
			continue
		}
		line := offsetToLine(lineStarts, match[2])
		from := enclosingSymbol(sorted, line)
		if from == "" || from == name || strings.HasSuffix(name, "."+from) || strings.HasSuffix(name, "::"+from) {
			continue
		}
		key := fmt.Sprintf("%s:%s:%d", from, name, line)
		if seen[key] {
			continue
		}
		seen[key] = true
		calls = append(calls, api.CallEdge{FromFile: file, FromSymbol: from, ToName: name, Line: line, Confidence: "HEURISTIC"})
	}
	return calls
}

func lineStartOffsets(content string) []int {
	starts := []int{0}
	for i, r := range content {
		if r == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func offsetToLine(starts []int, offset int) int {
	i := sort.Search(len(starts), func(i int) bool { return starts[i] > offset })
	return i
}

func enclosingSymbol(symbols []api.Symbol, line int) string {
	for i := len(symbols) - 1; i >= 0; i-- {
		s := symbols[i]
		end := s.EndLine
		if end <= 0 {
			end = s.Line
		}
		if line >= s.Line && line <= end && (s.Kind == api.Function || s.Kind == api.Method) {
			return s.Name
		}
	}
	return ""
}

func shouldSkipCall(name string, language api.Language) bool {
	base := name
	if idx := strings.LastIndexAny(base, ".:"); idx >= 0 {
		base = strings.TrimLeft(base[idx+1:], ":")
	}
	keywords := map[string]bool{
		"if": true, "for": true, "switch": true, "while": true, "catch": true, "return": true,
		"func": true, "function": true, "def": true, "class": true, "new": true, "make": true,
		"println": true, "print": true, "len": true, "cap": true, "append": true, "delete": true,
	}
	_ = language
	return keywords[base]
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
