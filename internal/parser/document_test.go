package parser

import (
	"testing"
)

func TestExtractDocumentLinksIncludeSections(t *testing.T) {
	_, links := ExtractDocument("docs/guide.md", `# Guide

Intro mentions `+"`IntroSymbol`"+`.

## API Usage

Use `+"`NewEngine`"+` with internal/engine/engine.go.

## Review Flow

Call `+"`ReviewContext`"+` from docs.
`)

	var foundEngine, foundReview bool
	for _, link := range links {
		switch link.TargetValue {
		case "NewEngine", "internal/engine/engine.go":
			if link.SectionTitle != "API Usage" || link.SectionSlug != "api-usage" || link.SectionLine != 5 {
				t.Fatalf("NewEngine section mismatch: %+v", link)
			}
			foundEngine = true
		case "ReviewContext":
			if link.SectionTitle != "Review Flow" || link.SectionSlug != "review-flow" || link.SectionLine != 9 {
				t.Fatalf("ReviewContext section mismatch: %+v", link)
			}
			foundReview = true
		}
	}
	if !foundEngine || !foundReview {
		t.Fatalf("expected sectioned document links, got %+v", links)
	}
}

func TestMarkdownSlug(t *testing.T) {
	if got := markdownSlug("API Usage & Review Flow!"); got != "api-usage-review-flow" {
		t.Fatalf("markdownSlug got %q", got)
	}
}
