package search

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/store"
)

type Searcher struct {
	store store.Store
	root  string
}

func New(s store.Store, root string) *Searcher {
	return &Searcher{store: s, root: root}
}

func (sr *Searcher) SearchSymbols(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error) {
	if limit <= 0 {
		limit = 50
	}

	q := strings.TrimSpace(query)
	base, err := sr.store.SearchSymbols(ctx, q, kind, limit)
	if err != nil {
		return nil, err
	}

	if q == "" {
		return base, nil
	}

	lowerQ := strings.ToLower(q)
	type rankedSymbol struct {
		symbol api.Symbol
		score  int
		order  int
	}

	seen := map[string]struct{}{}
	ranked := make([]rankedSymbol, 0, len(base))
	order := 0

	add := func(sym api.Symbol) {
		key := fmt.Sprintf("%s|%s|%s|%d", sym.FilePath, sym.Name, sym.Kind, sym.Line)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		ranked = append(ranked, rankedSymbol{symbol: sym, score: symbolMatchScore(sym.Name, lowerQ), order: order})
		order++
	}

	for _, sym := range base {
		add(sym)
	}

	if len(ranked) < limit {
		files, err := sr.store.ListFiles(ctx, nil)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			syms, err := sr.store.GetFileSymbols(ctx, f.Path)
			if err != nil {
				return nil, err
			}
			for _, sym := range syms {
				if kind != nil && sym.Kind != *kind {
					continue
				}
				if symbolMatchScore(sym.Name, lowerQ) == 0 {
					continue
				}
				add(sym)
			}
		}
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].order < ranked[j].order
	})

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	out := make([]api.Symbol, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.symbol)
	}
	return out, nil
}

func (sr *Searcher) SearchSymbolsHybrid(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error) {
	if limit <= 0 {
		limit = 50
	}

	q := strings.TrimSpace(query)
	if q == "" {
		return sr.SearchSymbols(ctx, query, kind, limit)
	}

	ftsResults, err := sr.store.SearchSymbols(ctx, q, kind, limit)
	if err != nil {
		return nil, err
	}

	allSyms, err := sr.allSymbols(ctx, kind)
	if err != nil {
		return nil, err
	}
	if len(allSyms) == 0 {
		return ftsResults, nil
	}

	ftsSet := make(map[string]struct{}, len(ftsResults))
	for _, s := range ftsResults {
		ftsSet[symbolKey(s)] = struct{}{}
	}

	queryTokens := tokenizeSemantic(q)
	if len(queryTokens) == 0 {
		return ftsResults, nil
	}

	df := make(map[string]int)
	for _, sym := range allSyms {
		tokens := tokenizeSemantic(symbolSemanticText(sym))
		seen := make(map[string]struct{}, len(tokens))
		for _, tok := range tokens {
			if _, ok := seen[tok]; ok {
				continue
			}
			seen[tok] = struct{}{}
			df[tok]++
		}
	}

	queryVec, queryNorm := tfidfVector(queryTokens, df, len(allSyms))
	if queryNorm == 0 {
		return ftsResults, nil
	}

	lowerQ := strings.ToLower(q)
	type hybridRankedSymbol struct {
		symbol   api.Symbol
		hybrid   float64
		semantic float64
		keyword  float64
		order    int
	}

	ranked := make([]hybridRankedSymbol, 0, len(allSyms))
	for i, sym := range allSyms {
		symVec, symNorm := tfidfVector(tokenizeSemantic(symbolSemanticText(sym)), df, len(allSyms))
		semantic := cosineSimilarity(queryVec, queryNorm, symVec, symNorm)

		keyword := float64(symbolMatchScore(sym.Name, lowerQ)) / 3.0
		if _, ok := ftsSet[symbolKey(sym)]; ok {
			keyword = 1.0
		}

		if keyword == 0 && semantic < 0.08 {
			continue
		}

		hybrid := 0.6*keyword + 0.4*semantic
		ranked = append(ranked, hybridRankedSymbol{
			symbol:   sym,
			hybrid:   hybrid,
			semantic: semantic,
			keyword:  keyword,
			order:    i,
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].hybrid != ranked[j].hybrid {
			return ranked[i].hybrid > ranked[j].hybrid
		}
		if ranked[i].semantic != ranked[j].semantic {
			return ranked[i].semantic > ranked[j].semantic
		}
		if ranked[i].keyword != ranked[j].keyword {
			return ranked[i].keyword > ranked[j].keyword
		}
		return ranked[i].order < ranked[j].order
	})

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	out := make([]api.Symbol, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.symbol)
	}
	return out, nil
}

func symbolMatchScore(name, lowerQuery string) int {
	lowerName := strings.ToLower(name)
	switch {
	case lowerName == lowerQuery:
		return 3
	case strings.HasPrefix(lowerName, lowerQuery):
		return 2
	case strings.Contains(lowerName, lowerQuery):
		return 1
	default:
		return 0
	}
}

func (sr *Searcher) allSymbols(ctx context.Context, kind *api.SymbolKind) ([]api.Symbol, error) {
	files, err := sr.store.ListFiles(ctx, nil)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	all := make([]api.Symbol, 0, len(files)*8)
	for _, f := range files {
		syms, err := sr.store.GetFileSymbols(ctx, f.Path)
		if err != nil {
			return nil, err
		}
		for _, sym := range syms {
			if kind != nil && sym.Kind != *kind {
				continue
			}
			key := symbolKey(sym)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			all = append(all, sym)
		}
	}
	return all, nil
}

func symbolKey(sym api.Symbol) string {
	return fmt.Sprintf("%s|%s|%s|%d", sym.FilePath, sym.Name, sym.Kind, sym.Line)
}

func symbolSemanticText(sym api.Symbol) string {
	return strings.Join([]string{sym.Name, sym.Signature, sym.Parent, sym.FilePath}, " ")
}

func tokenizeSemantic(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	parts := strings.FieldsFunc(text, func(r rune) bool {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
		return true
	})

	tokens := make([]string, 0, len(parts)*2)
	for _, p := range parts {
		if p == "" {
			continue
		}
		for _, seg := range splitIdentifierParts(p) {
			if seg != "" {
				tokens = append(tokens, strings.ToLower(seg))
			}
		}
	}
	return tokens
}

func splitIdentifierParts(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}

	parts := make([]string, 0, 2)
	start := 0
	for i := 1; i < len(runes); i++ {
		prev := runes[i-1]
		curr := runes[i]
		nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])

		if unicode.IsUpper(curr) && (unicode.IsLower(prev) || nextLower) {
			parts = append(parts, string(runes[start:i]))
			start = i
		}
	}
	parts = append(parts, string(runes[start:]))
	return parts
}

func tfidfVector(tokens []string, df map[string]int, totalDocs int) (map[string]float64, float64) {
	if len(tokens) == 0 || totalDocs <= 0 {
		return nil, 0
	}

	tf := make(map[string]float64)
	for _, tok := range tokens {
		tf[tok]++
	}

	vec := make(map[string]float64, len(tf))
	var sumSquares float64
	for tok, count := range tf {
		idf := math.Log((1.0+float64(totalDocs))/(1.0+float64(df[tok]))) + 1.0
		w := count * idf
		vec[tok] = w
		sumSquares += w * w
	}

	return vec, math.Sqrt(sumSquares)
}

func cosineSimilarity(a map[string]float64, aNorm float64, b map[string]float64, bNorm float64) float64 {
	if aNorm == 0 || bNorm == 0 {
		return 0
	}

	var dot float64
	for k, av := range a {
		if bv, ok := b[k]; ok {
			dot += av * bv
		}
	}
	return dot / (aNorm * bNorm)
}

func (sr *Searcher) FindDefinition(ctx context.Context, name string) ([]api.Symbol, error) {
	return sr.store.FindDefinitions(ctx, name)
}

func (sr *Searcher) FindReferences(ctx context.Context, name string) ([]api.Symbol, error) {
	// First find where the symbol is defined
	defs, err := sr.store.FindDefinitions(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, nil
	}

	// Get the definition to know its kind and file
	def := defs[0]

	// Find all symbols with the same name across ALL files
	// This finds usages (not just definitions)
	refs, err := sr.store.FindReferences(ctx, name)
	if err != nil {
		return nil, err
	}

	// Filter out the definition itself
	var result []api.Symbol
	for _, r := range refs {
		// Exclude the definition file's same-name symbol
		if r.FilePath == def.FilePath && r.Line == def.Line {
			continue
		}
		result = append(result, r)
	}

	return result, nil
}

func (sr *Searcher) GetFileSymbols(ctx context.Context, path string) ([]api.Symbol, error) {
	return sr.store.GetFileSymbols(ctx, path)
}

func (sr *Searcher) SearchText(ctx context.Context, query string, filePattern string, limit int) ([]api.SearchMatch, error) {
	if limit <= 0 {
		limit = 50
	}

	files, err := sr.store.ListFiles(ctx, nil)
	if err != nil {
		return nil, err
	}

	var matches []api.SearchMatch
	for _, f := range files {
		if len(matches) >= limit {
			break
		}
		if filePattern != "" && !strings.Contains(f.Path, filePattern) {
			continue
		}
		ms := grepFile(sr.root, f.Path, query, limit-len(matches))
		matches = append(matches, ms...)
	}
	return matches, nil
}

func grepFile(root, path, pattern string, max int) []api.SearchMatch {
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return nil
	}

	var matches []api.SearchMatch
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if len(matches) >= max {
			break
		}
		if strings.Contains(line, pattern) {
			matches = append(matches, api.SearchMatch{
				FilePath: path,
				Line:     i + 1,
				Content:  strings.TrimSpace(line),
			})
		}
	}
	return matches
}

func formatSym(s api.Symbol) string {
	return fmt.Sprintf("  %-40s  %-10s  %s:%d", s.Name, s.Kind, s.FilePath, s.Line)
}

func FormatSymbols(syms []api.Symbol) string {
	var lines []string
	for _, s := range syms {
		lines = append(lines, formatSym(s))
	}
	return strings.Join(lines, "\n")
}

func FormatMatches(ms []api.SearchMatch) string {
	var lines []string
	for _, m := range ms {
		lines = append(lines, fmt.Sprintf("  %s:%d  %s", m.FilePath, m.Line, m.Content))
	}
	return strings.Join(lines, "\n")
}
