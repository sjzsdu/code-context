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
	Routes  []api.Route
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
	result.Routes = extractRoutes(filePath, string(content), language)

	return result, nil
}

type routePattern struct {
	re         *regexp.Regexp
	method     string
	framework  string
	pathIdx    int
	handlerIdx int
}

var routePatterns = map[api.Language][]routePattern{
	api.Go: {
		{regexp.MustCompile(`(?m)\b(?:[A-Za-z_][A-Za-z0-9_]*\.)?(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s*\(\s*["\x60]([^"\x60]+)["\x60]\s*,\s*([A-Za-z_][A-Za-z0-9_\.]*)`), "", "go-router", 2, 3},
		{regexp.MustCompile(`(?m)\bHandleFunc\s*\(\s*["\x60]([^"\x60]+)["\x60]\s*,\s*([A-Za-z_][A-Za-z0-9_\.]*)`), "", "net/http", 1, 2},
	},
	api.TypeScript: jsRoutePatterns(),
	api.JavaScript: jsRoutePatterns(),
	api.Python: {
		{regexp.MustCompile(`(?m)@(?:app|router|bp)\.(get|post|put|patch|delete|head|options|route)\s*\(\s*["']([^"']+)["']`), "", "python-web", 2, 0},
		{regexp.MustCompile(`(?m)\bpath\s*\(\s*["']([^"']+)["']\s*,\s*([A-Za-z_][A-Za-z0-9_\.]*)`), "", "django", 1, 2},
	},
	api.Java: {
		{regexp.MustCompile(`(?m)@(GetMapping|PostMapping|PutMapping|PatchMapping|DeleteMapping|RequestMapping)\s*(?:\(\s*(?:value\s*=\s*)?["']([^"']+)["'])?`), "", "spring", 2, 0},
	},
	api.Rust: {
		{regexp.MustCompile(`(?m)#\[(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"']+)["']\s*\)\]`), "", "rust-attr", 2, 0},
		{regexp.MustCompile(`(?m)\.route\s*\(\s*["']([^"']+)["']\s*,\s*(?:get|post|put|patch|delete)\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`), "", "axum", 1, 2},
	},
}

func jsRoutePatterns() []routePattern {
	return []routePattern{
		{regexp.MustCompile(`(?m)\b(?:app|router)\.(get|post|put|patch|delete|head|options|use)\s*\(\s*["'\x60]([^"'\x60]+)["'\x60]\s*(?:,\s*[A-Za-z_][A-Za-z0-9_]*\s*)*,\s*([A-Za-z_][A-Za-z0-9_\.]*)`), "", "express", 2, 3},
		{regexp.MustCompile(`(?m)@(Get|Post|Put|Patch|Delete|Head|Options)\s*\(\s*["'\x60]([^"'\x60]*)["'\x60]\s*\)`), "", "nestjs", 2, 0},
		{regexp.MustCompile(`(?m)<Route\s+[^>]*path=["'\x60]([^"'\x60]+)["'\x60][^>]*(?:component=\{?([A-Za-z_][A-Za-z0-9_]*)\}?|element=\{?<([A-Za-z_][A-Za-z0-9_]*)\b)`), "", "react-router", 1, 2},
	}
}

func extractRoutes(file string, content string, language api.Language) []api.Route {
	patterns := routePatterns[language]
	if len(patterns) == 0 {
		return nil
	}
	lineStarts := lineStartOffsets(content)
	seen := make(map[string]bool)
	var routes []api.Route
	for _, rp := range patterns {
		for _, match := range rp.re.FindAllStringSubmatchIndex(content, -1) {
			method := rp.method
			if method == "" && len(match) >= 4 && match[2] >= 0 {
				candidate := strings.ToUpper(content[match[2]:match[3]])
				method = normalizeRouteMethod(candidate)
			}
			path := captureAt(content, match, rp.pathIdx)
			handler := captureAt(content, match, rp.handlerIdx)
			if handler == "" && rp.framework == "react-router" {
				handler = captureAt(content, match, 3)
			}
			if path == "" {
				continue
			}
			path = applyRoutePrefix(content, language, rp.framework, match[0], path)
			line := offsetToLine(lineStarts, match[0])
			if handler == "" {
				handler = nextSymbolName(content, match[1])
			}
			key := method + " " + path + " " + handler + fmt.Sprint(line)
			if seen[key] {
				continue
			}
			seen[key] = true
			routes = append(routes, api.Route{FilePath: file, Method: method, Path: path, Handler: handler, Framework: rp.framework, Line: line, Confidence: "HEURISTIC"})
		}
	}
	return routes
}

func applyRoutePrefix(content string, language api.Language, framework string, routeOffset int, path string) string {
	prefix := ""
	switch language {
	case api.TypeScript, api.JavaScript:
		if framework == "nestjs" {
			prefix = nearestDecoratorPrefix(content[:routeOffset], regexp.MustCompile(`@Controller\s*\(\s*["'\x60]([^"'\x60]*)["'\x60]`))
		}
	case api.Java:
		if framework == "spring" {
			prefix = nearestJavaClassRequestMappingPrefix(content[:routeOffset])
		}
	case api.Python:
		prefix = pythonDecoratorPrefix(content, routeOffset)
	}
	return joinRoutePaths(prefix, path)
}

func nearestDecoratorPrefix(before string, re *regexp.Regexp) string {
	matches := re.FindAllStringSubmatch(before, -1)
	if len(matches) == 0 || len(matches[len(matches)-1]) < 2 {
		return ""
	}
	return matches[len(matches)-1][1]
}

func nearestJavaClassRequestMappingPrefix(before string) string {
	re := regexp.MustCompile(`(?s)@RequestMapping\s*\(\s*(?:value\s*=\s*)?["']([^"']+)["'][^)]*\)\s*(?:public\s+)?(?:abstract\s+)?class\s+[A-Za-z_][A-Za-z0-9_]*`)
	matches := re.FindAllStringSubmatch(before, -1)
	if len(matches) == 0 || len(matches[len(matches)-1]) < 2 {
		return ""
	}
	return matches[len(matches)-1][1]
}

func pythonDecoratorPrefix(content string, routeOffset int) string {
	decoratorStart := routeOffset
	lineStart := strings.LastIndex(content[:routeOffset], "\n")
	if lineStart >= 0 {
		decoratorStart = lineStart + 1
	}
	lineEndRel := strings.Index(content[decoratorStart:], "\n")
	lineEnd := len(content)
	if lineEndRel >= 0 {
		lineEnd = decoratorStart + lineEndRel
	}
	line := content[decoratorStart:lineEnd]
	m := regexp.MustCompile(`@([A-Za-z_][A-Za-z0-9_]*)\.`).FindStringSubmatch(line)
	if len(m) < 2 || m[1] == "app" {
		return ""
	}
	name := regexp.QuoteMeta(m[1])
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)\b` + name + `\s*=\s*APIRouter\s*\([^)]*prefix\s*=\s*["']([^"']+)["']`),
		regexp.MustCompile(`(?m)\b` + name + `\s*=\s*Blueprint\s*\([^)]*url_prefix\s*=\s*["']([^"']+)["']`),
	}
	before := content[:routeOffset]
	for _, re := range patterns {
		matches := re.FindAllStringSubmatch(before, -1)
		if len(matches) > 0 && len(matches[len(matches)-1]) > 1 {
			return matches[len(matches)-1][1]
		}
	}
	return ""
}

func joinRoutePaths(prefix, path string) string {
	if prefix == "" || prefix == "/" {
		if path == "" {
			return "/"
		}
		if strings.HasPrefix(path, "/") {
			return path
		}
		return "/" + path
	}
	if path == "" || path == "/" {
		if strings.HasPrefix(prefix, "/") {
			return strings.TrimRight(prefix, "/")
		}
		return "/" + strings.TrimRight(prefix, "/")
	}
	return "/" + strings.Trim(strings.TrimRight(prefix, "/")+"/"+strings.TrimLeft(path, "/"), "/")
}

func captureAt(content string, match []int, idx int) string {
	if idx <= 0 || idx*2+1 >= len(match) || match[idx*2] < 0 {
		return ""
	}
	return strings.TrimSpace(content[match[idx*2]:match[idx*2+1]])
}

func normalizeRouteMethod(method string) string {
	switch strings.ToUpper(method) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return strings.ToUpper(method)
	case "GETMAPPING":
		return "GET"
	case "POSTMAPPING":
		return "POST"
	case "PUTMAPPING":
		return "PUT"
	case "PATCHMAPPING":
		return "PATCH"
	case "DELETEMAPPING":
		return "DELETE"
	default:
		return ""
	}
}

func nextSymbolName(content string, after int) string {
	tail := content[after:]
	if len(tail) > 500 {
		tail = tail[:500]
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)`),
		regexp.MustCompile(`(?m)^\s*(?:async\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
		regexp.MustCompile(`(?m)(?:public|private|protected)?\s*(?:[A-Za-z0-9_<>,\[\]]+\s+)+([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
		regexp.MustCompile(`(?m)^\s*(?:async\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(tail); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

var callPattern = regexp.MustCompile(`(?m)([A-Za-z_][A-Za-z0-9_]*(?:(?:\.|::)[A-Za-z_][A-Za-z0-9_]*)?)\s*\(`)

func extractCalls(file string, content string, language api.Language, symbols []api.Symbol) []api.CallEdge {
	if len(symbols) == 0 {
		return nil
	}
	importAliases := map[string]bool{}
	if language == api.Go {
		importAliases = goImportAliases(content)
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
		if shouldSkipCall(name, language) || isCallMatchInNonCode(content, match[2], language) {
			continue
		}
		if language == api.Go && isGoImportedQualifiedCall(name, importAliases) {
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

func isGoImportedQualifiedCall(name string, importAliases map[string]bool) bool {
	if len(importAliases) == 0 {
		return false
	}
	idx := strings.Index(name, ".")
	if idx <= 0 {
		return false
	}
	return importAliases[name[:idx]]
}

func goImportAliases(content string) map[string]bool {
	aliases := map[string]bool{}
	add := func(alias, path string) {
		if alias == "_" || alias == "." {
			return
		}
		if alias == "" {
			path = strings.Trim(path, "\"`")
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1]
		}
		if alias != "" {
			aliases[alias] = true
		}
	}

	blockRe := regexp.MustCompile(`(?s)import\s*\((.*?)\)`)
	lineRe := regexp.MustCompile("(?m)^\\s*(?:(\\w+|\\.|_)\\s+)?[\"`]([^\"`]+)[\"`]")
	for _, block := range blockRe.FindAllStringSubmatch(content, -1) {
		for _, m := range lineRe.FindAllStringSubmatch(block[1], -1) {
			add(m[1], m[2])
		}
	}

	singleRe := regexp.MustCompile("(?m)^\\s*import\\s+(?:(\\w+|\\.|_)\\s+)?[\"`]([^\"`]+)[\"`]")
	for _, m := range singleRe.FindAllStringSubmatch(content, -1) {
		add(m[1], m[2])
	}
	return aliases
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

func isCallMatchInNonCode(content string, offset int, language api.Language) bool {
	lineStart := strings.LastIndex(content[:offset], "\n") + 1
	linePrefix := content[lineStart:offset]
	if inLineComment(linePrefix, language) || inStringLiteral(linePrefix) {
		return true
	}
	return inBlockComment(content, offset, language)
}

func inLineComment(prefix string, language api.Language) bool {
	markers := []string{"//"}
	if language == api.Python {
		markers = append(markers, "#")
	}
	for _, marker := range markers {
		idx := strings.Index(prefix, marker)
		if idx >= 0 && !inStringLiteral(prefix[:idx]) {
			return true
		}
	}
	return false
}

func inStringLiteral(prefix string) bool {
	var quote rune
	escaped := false
	for _, r := range prefix {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote != '`' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' || r == '`' {
			quote = r
		}
	}
	return quote != 0
}

func inBlockComment(content string, offset int, language api.Language) bool {
	if language == api.Python {
		return false
	}
	before := content[:offset]
	start := strings.LastIndex(before, "/*")
	if start < 0 {
		return false
	}
	end := strings.LastIndex(before, "*/")
	return end < start
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
