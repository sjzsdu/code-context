package embedding

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/store"
)

const (
	maxSymbolSnippetLines = 80
	maxSymbolTextChars    = 6000
	maxDocumentChunkChars = 4000
)

var markdownHeadingPattern = regexp.MustCompile(`^#{1,6}\s+(.+)$`)

// Chunk is the normalized text unit used for embedding. It keeps source
// provenance separate from storage-specific vector indexes so the same chunks
// can be cached locally, written to Helix, or used by another provider later.
type Chunk struct {
	ID          string
	Text        string
	ContentHash string
	Kind        store.EmbeddingInputKind
	Target      store.TargetRef
	Metadata    map[string]string
}

func (c Chunk) Input() store.EmbeddingInput {
	return store.EmbeddingInput{
		ID:       c.ID,
		Text:     c.Text,
		Kind:     c.Kind,
		Target:   c.Target,
		Metadata: c.Metadata,
	}
}

func BuildSymbolChunks(projectID, filePath string, content []byte, symbols []api.Symbol) []Chunk {
	if len(symbols) == 0 {
		return nil
	}
	chunks := make([]Chunk, 0, len(symbols))
	for _, sym := range symbols {
		if sym.FilePath == "" {
			sym.FilePath = filePath
		}
		text := symbolChunkText(sym, content)
		if strings.TrimSpace(text) == "" {
			continue
		}
		target := store.TargetRef{
			ProjectID: projectID,
			Kind:      store.TargetSymbol,
			Path:      sym.FilePath,
			Name:      sym.Name,
			Type:      string(sym.Kind),
			Line:      sym.Line,
			EndLine:   sym.EndLine,
		}
		chunks = append(chunks, Chunk{
			ID:          fmt.Sprintf("symbol:%s:%s:%d", sym.FilePath, sym.Name, sym.Line),
			Text:        text,
			ContentHash: sha256HexString(text),
			Kind:        store.EmbeddingInputSymbol,
			Target:      target,
			Metadata: map[string]string{
				"file":      sym.FilePath,
				"name":      sym.Name,
				"kind":      string(sym.Kind),
				"line":      strconv.Itoa(sym.Line),
				"end_line":  strconv.Itoa(sym.EndLine),
				"signature": sym.Signature,
				"parent":    sym.Parent,
			},
		})
	}
	return chunks
}

func BuildDocumentChunks(projectID string, doc *api.Document, content []byte) []Chunk {
	if doc == nil {
		return nil
	}
	sections := splitDocumentSections(doc, string(content))
	chunks := make([]Chunk, 0, len(sections))
	for i, section := range sections {
		text := strings.TrimSpace(section.Text)
		if text == "" {
			continue
		}
		if len(text) > maxDocumentChunkChars {
			text = text[:maxDocumentChunkChars]
		}
		title := firstNonEmpty(section.Title, doc.Title, filepath.Base(doc.Path))
		line := section.Line
		if line <= 0 {
			line = 1
		}
		target := store.TargetRef{
			ProjectID: projectID,
			Kind:      store.TargetDocument,
			Path:      doc.Path,
			Name:      title,
			Type:      "document",
			Line:      line,
		}
		chunks = append(chunks, Chunk{
			ID:          fmt.Sprintf("document:%s:%03d:%s", doc.Path, i, slugify(title)),
			Text:        documentChunkText(doc, title, text),
			ContentHash: sha256HexString(documentChunkText(doc, title, text)),
			Kind:        store.EmbeddingInputDocument,
			Target:      target,
			Metadata: map[string]string{
				"file":     doc.Path,
				"title":    title,
				"language": doc.Language,
				"line":     strconv.Itoa(line),
				"chunk":    strconv.Itoa(i),
			},
		})
	}
	return chunks
}

type documentSection struct {
	Title string
	Line  int
	Text  string
}

func splitDocumentSections(doc *api.Document, content string) []documentSection {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return []documentSection{{Title: doc.Title, Line: 1, Text: firstNonEmpty(doc.Summary, doc.Title, doc.Path)}}
	}
	var sections []documentSection
	currentTitle := firstNonEmpty(doc.Title, filepath.Base(doc.Path))
	currentLine := 1
	var current []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(current, "\n"))
		if text != "" {
			sections = append(sections, documentSection{Title: currentTitle, Line: currentLine, Text: text})
		}
		current = nil
	}
	for i, line := range lines {
		if match := markdownHeadingPattern.FindStringSubmatch(strings.TrimSpace(line)); len(match) > 1 {
			flush()
			currentTitle = strings.TrimSpace(match[1])
			currentLine = i + 1
		}
		current = append(current, line)
		if charLen(current) >= maxDocumentChunkChars {
			flush()
			currentLine = i + 1
		}
	}
	flush()
	if len(sections) == 0 {
		sections = append(sections, documentSection{Title: currentTitle, Line: 1, Text: firstNonEmpty(doc.Summary, doc.Title, doc.Path)})
	}
	return sections
}

func symbolChunkText(sym api.Symbol, content []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Symbol: %s\nKind: %s\nFile: %s\nLine: %d\n", sym.Name, sym.Kind, sym.FilePath, sym.Line)
	if sym.Parent != "" {
		fmt.Fprintf(&b, "Parent: %s\n", sym.Parent)
	}
	if sym.Signature != "" {
		fmt.Fprintf(&b, "Signature: %s\n", sym.Signature)
	}
	if snippet := sourceSnippet(content, sym.Line, sym.EndLine); snippet != "" {
		fmt.Fprintf(&b, "Code:\n%s", snippet)
	}
	text := strings.TrimSpace(b.String())
	if len(text) > maxSymbolTextChars {
		return text[:maxSymbolTextChars]
	}
	return text
}

func documentChunkText(doc *api.Document, title, text string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Document: %s\nPath: %s\n", firstNonEmpty(title, doc.Title, doc.Path), doc.Path)
	if doc.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", doc.Summary)
	}
	fmt.Fprintf(&b, "Content:\n%s", text)
	return strings.TrimSpace(b.String())
}

func sourceSnippet(content []byte, startLine, endLine int) string {
	if startLine <= 0 || len(content) == 0 {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	if startLine > len(lines) {
		return ""
	}
	if endLine < startLine {
		endLine = startLine
	}
	if endLine-startLine+1 > maxSymbolSnippetLines {
		endLine = startLine + maxSymbolSnippetLines - 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.TrimSpace(strings.Join(lines[startLine-1:endLine], "\n"))
}

func charLen(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line) + 1
	}
	return total
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	dash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sha256HexString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
