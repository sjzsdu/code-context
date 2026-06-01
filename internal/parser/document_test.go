package parser

import (
	"testing"
)

func TestExtractDocumentLinksIncludeSections(t *testing.T) {
	_, links := ExtractDocument("docs/guide.md", `# Guide

Intro mentions `+"`IntroSymbol`"+`.

## API Usage

Use `+"`NewEngine`"+` with internal/engine/engine.go.
GET /api/context/{id}

## Review Flow

Call `+"`ReviewContext`"+` from docs.
`)

	var foundEngine, foundReview, foundRoute bool
	for _, link := range links {
		switch link.TargetValue {
		case "NewEngine", "internal/engine/engine.go":
			if link.SectionTitle != "API Usage" || link.SectionSlug != "api-usage" || link.SectionLine != 5 {
				t.Fatalf("NewEngine section mismatch: %+v", link)
			}
			foundEngine = true
		case "GET /api/context/{id}":
			if link.TargetType != "route" || link.SectionTitle != "API Usage" || link.SectionSlug != "api-usage" || link.SectionLine != 5 {
				t.Fatalf("route section mismatch: %+v", link)
			}
			foundRoute = true
		case "ReviewContext":
			if link.SectionTitle != "Review Flow" || link.SectionSlug != "review-flow" || link.SectionLine != 10 {
				t.Fatalf("ReviewContext section mismatch: %+v", link)
			}
			foundReview = true
		}
	}
	if !foundEngine || !foundReview || !foundRoute {
		t.Fatalf("expected sectioned document links, got %+v", links)
	}
}

func TestMarkdownSlug(t *testing.T) {
	if got := markdownSlug("API Usage & Review Flow!"); got != "api-usage-review-flow" {
		t.Fatalf("markdownSlug got %q", got)
	}
}
