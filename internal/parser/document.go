package parser

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sjzsdu/code-context/internal/api"
)

var (
	pathPattern      = regexp.MustCompile(`([\w/\\]+\.(go|ts|tsx|js|jsx|py|rs|java|md|yaml|yml|json|toml))`)
	backtickPattern  = regexp.MustCompile("`([^`]+)`")
	camelCasePattern = regexp.MustCompile(`\b([A-Z][a-z]+(?:[A-Z][a-z]+)+)\b`)
	snakeCasePattern = regexp.MustCompile(`\b([a-z]+_[a-z0-9_]+)\b`)
	headingPattern   = regexp.MustCompile(`^#+\s+(.+)$`)
	knownModules     = map[string]bool{
		"internal/api":     true,
		"internal/parser":  true,
		"internal/store":   true,
		"internal/indexer": true,
		"internal/engine":  true,
		"internal/search":  true,
		"internal/graph":   true,
		"cmd/code-context": true,
		"cmd/mcp":          true,
	}
)

func ExtractDocument(path, content string) (*api.Document, []api.DocumentLink) {
	doc := &api.Document{
		Path:     path,
		Language: string(api.Markdown),
		Size:     len(content),
	}

	title := extractTitle(content)
	doc.Title = title
	doc.Summary = extractSummary(content, 300)

	contentHash := hashContent(content)
	doc.ContentHash = contentHash

	links := extractDocumentLinks(path, content, title)

	return doc, links
}

func extractTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			heading := headingPattern.FindStringSubmatch(line)
			if len(heading) > 1 {
				return strings.TrimSpace(heading[1])
			}
			trimmed := strings.TrimPrefix(line, "#")
			trimmed = strings.TrimSpace(trimmed)
			if trimmed != "" {
				return trimmed
			}
		}
	}

	base := filepath.Base(filepath.Dir(content))
	if base == "." {
		base = "root"
	}
	return base
}

func extractSummary(content string, maxLen int) string {
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	count := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "```") || (strings.HasPrefix(line, "- ") && strings.Contains(line, "[]")) {
			continue
		}

		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")

		if len(line) > 3 && !strings.HasPrefix(line, "|") {
			sb.WriteString(line)
			sb.WriteString(" ")
			count++
			if sb.Len() >= maxLen {
				break
			}
		}
		if count >= 3 {
			break
		}
	}

	summary := strings.TrimSpace(sb.String())
	if len(summary) > maxLen {
		summary = summary[:maxLen] + "..."
	}
	return summary
}

func hashContent(content string) string {
	hash := 0
	for i, c := range content {
		hash = hash*31 + int(c)*((i%255)+1)
	}
	return string(rune(hash & 0xFFFFFFFF))
}

func extractDocumentLinks(path, content, title string) []api.DocumentLink {
	var links []api.DocumentLink

	contentLower := strings.ToLower(content)
	_ = contentLower
	pathDir := filepath.Dir(path)
	_ = pathDir

	for _, m := range pathPattern.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		candidate := m[1]
		if strings.Contains(candidate, "..") {
			continue
		}

		line := findLineNumber(content, candidate)
		links = append(links, api.DocumentLink{
			TargetType:  "file",
			TargetValue: candidate,
			Line:        line,
			Evidence:    candidate,
			Confidence:  0.9,
		})
	}

	for _, m := range backtickPattern.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		candidate := strings.TrimSpace(m[1])

		if isLikelySymbol(candidate) {
			line := findLineNumber(content, m[0])
			links = append(links, api.DocumentLink{
				TargetType:  "symbol",
				TargetValue: candidate,
				Line:        line,
				Evidence:    m[0],
				Confidence:  0.8,
			})
		}
	}

	for _, m := range camelCasePattern.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		candidate := m[1]
		if len(candidate) < 4 {
			continue
		}

		line := findLineNumber(content, m[0])
		links = append(links, api.DocumentLink{
			TargetType:  "symbol",
			TargetValue: candidate,
			Line:        line,
			Evidence:    m[0],
			Confidence:  0.6,
		})
	}

	for _, m := range snakeCasePattern.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		candidate := m[1]
		if len(candidate) < 5 {
			continue
		}

		line := findLineNumber(content, m[0])
		links = append(links, api.DocumentLink{
			TargetType:  "symbol",
			TargetValue: candidate,
			Line:        line,
			Evidence:    m[0],
			Confidence:  0.5,
		})
	}

	dirParts := strings.Split(pathDir, "/")
	for i := range dirParts {
		if i == 0 {
			continue
		}
		candidate := strings.Join(dirParts[:i+1], "/")
		if knownModules[candidate] {
			line := 1
			links = append(links, api.DocumentLink{
				TargetType:  "module",
				TargetValue: candidate,
				Line:        line,
				Evidence:    "directory structure",
				Confidence:  0.7,
			})
		}
	}

	return deduplicateLinks(links)
}

func isLikelySymbol(s string) bool {
	if len(s) < 2 || len(s) > 60 {
		return false
	}

	if strings.Contains(s, " ") || strings.Contains(s, "/") || strings.Contains(s, "\\") {
		return false
	}

	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "-") {
		return false
	}

	hasUpper := false
	for _, c := range s {
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
			break
		}
	}

	return hasUpper || snakeCasePattern.MatchString(s)
}

func findLineNumber(content, target string) int {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, target) {
			return i + 1
		}
	}
	return 1
}

func deduplicateLinks(links []api.DocumentLink) []api.DocumentLink {
	seen := make(map[string]bool)
	var result []api.DocumentLink

	for _, link := range links {
		key := link.TargetType + "|" + link.TargetValue
		if !seen[key] {
			seen[key] = true
			result = append(result, link)
		}
	}

	return result
}
