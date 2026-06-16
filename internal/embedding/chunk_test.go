package embedding

import (
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/store"
)

func TestBuildSymbolChunks(t *testing.T) {
	content := []byte("package main\n\nfunc HealthHandler() {}\n")
	chunks := BuildSymbolChunks("", "main.go", content, []api.Symbol{{
		Name:      "HealthHandler",
		Kind:      api.Function,
		FilePath:  "main.go",
		Line:      3,
		EndLine:   3,
		Signature: "func HealthHandler()",
	}})
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	chunk := chunks[0]
	if chunk.Kind != store.EmbeddingInputSymbol {
		t.Fatalf("kind = %q", chunk.Kind)
	}
	if chunk.Target.Kind != store.TargetSymbol || chunk.Target.Name != "HealthHandler" {
		t.Fatalf("target = %+v", chunk.Target)
	}
	if !strings.Contains(chunk.Text, "func HealthHandler()") {
		t.Fatalf("chunk text missing source/signature: %q", chunk.Text)
	}
	if chunk.ContentHash == "" {
		t.Fatalf("content hash is empty")
	}
	if input := chunk.Input(); input.ID != chunk.ID || input.Text != chunk.Text {
		t.Fatalf("input = %+v", input)
	}
}

func TestBuildDocumentChunksSplitsHeadings(t *testing.T) {
	doc := &api.Document{Path: "README.md", Language: "markdown", Title: "Project", Summary: "Short summary"}
	content := []byte("# Project\nIntro text\n\n## Install\nRun setup\n\n## Usage\nRun command\n")
	chunks := BuildDocumentChunks("", doc, content)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3: %#v", len(chunks), chunks)
	}
	if chunks[1].Target.Name != "Install" || chunks[1].Target.Line != 4 {
		t.Fatalf("install chunk target = %+v", chunks[1].Target)
	}
	if !strings.Contains(chunks[2].Text, "Run command") {
		t.Fatalf("usage chunk text = %q", chunks[2].Text)
	}
}
