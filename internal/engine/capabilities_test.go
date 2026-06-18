package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	answerpkg "github.com/sjzsdu/code-context/internal/answer"
	embeddingpkg "github.com/sjzsdu/code-context/internal/embedding"
	"github.com/sjzsdu/code-context/internal/store"
)

func TestCapabilityNames(t *testing.T) {
	got := capabilityNames([]store.Capability{
		store.CapabilityTextSearch,
		"",
		store.CapabilityGraphTraversal,
	})
	want := []string{"text_search", "graph_traversal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilityNames() = %#v, want %#v", got, want)
	}
}

func TestCapabilityNamesEmpty(t *testing.T) {
	got := capabilityNames(nil)
	if got == nil {
		t.Fatal("capabilityNames(nil) returned nil, want empty slice for stable JSON")
	}
	if len(got) != 0 {
		t.Fatalf("capabilityNames(nil) = %#v, want empty", got)
	}
}

func TestAnswerTemplateCatalog(t *testing.T) {
	infos := AnswerTemplateCatalog(true)
	if len(infos) != len(AnswerTemplates()) {
		t.Fatalf("catalog len = %d, names len = %d", len(infos), len(AnswerTemplates()))
	}
	if infos[0].Name != AnswerTemplateGeneral || infos[0].Description == "" || infos[0].Prompt == "" {
		t.Fatalf("first template = %#v, want populated general template", infos[0])
	}
	var foundPlan bool
	for _, info := range infos {
		if info.Name == AnswerTemplatePlan {
			foundPlan = true
			if !strings.Contains(info.Description, "implementation plan") || !strings.Contains(info.Prompt, "implementation plan") {
				t.Fatalf("plan template info = %#v", info)
			}
		}
	}
	if !foundPlan {
		t.Fatalf("catalog = %#v, want plan template", infos)
	}
}

func TestStatusIncludesEmbeddingCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := NewWithOptions(root, Options{
		Store: store.Options{
			Backend: store.BackendSQLite,
			SQLite:  store.SQLiteOptions{Path: filepath.Join(root, "index.db")},
		},
		Embedding: embeddingpkg.Options{
			Provider:   embeddingpkg.ProviderOpenAICompatible,
			BaseURL:    "http://embedding.local/v1",
			Model:      "text-embedding-test",
			Dimensions: 3,
		},
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}
	defer eng.Close()

	status, err := eng.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Embedding == nil || !status.Embedding.Enabled {
		t.Fatalf("embedding status = %#v, want enabled", status.Embedding)
	}
	if status.Embedding.Model != "text-embedding-test" {
		t.Fatalf("embedding model = %q", status.Embedding.Model)
	}
	if !containsString(status.Capabilities, "embedding") {
		t.Fatalf("capabilities = %#v, want embedding", status.Capabilities)
	}
}

func TestStatusIncludesAnswerCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := NewWithOptions(root, Options{
		Store: store.Options{
			Backend: store.BackendSQLite,
			SQLite:  store.SQLiteOptions{Path: filepath.Join(root, "index.db")},
		},
		Answer: answerpkg.Options{
			Provider:  answerpkg.ProviderOpenAICompatible,
			BaseURL:   "http://answer.local/v1",
			Model:     "chat-test",
			MaxTokens: 256,
		},
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}
	defer eng.Close()

	status, err := eng.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Answer == nil || !status.Answer.Enabled {
		t.Fatalf("answer status = %#v, want enabled", status.Answer)
	}
	if status.Answer.Model != "chat-test" {
		t.Fatalf("answer model = %q", status.Answer.Model)
	}
	if !containsString(status.Capabilities, "answer") {
		t.Fatalf("capabilities = %#v, want answer", status.Capabilities)
	}
}

func TestEmbedUnsupportedCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	_, err = eng.Embed(context.Background(), []store.EmbeddingInput{{ID: "q", Text: "hello"}})
	if !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("Embed error = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestEmbedDelegatesToProvider(t *testing.T) {
	eng := &Engine{embedder: fakeEmbedder{}}
	vectors, err := eng.Embed(context.Background(), []store.EmbeddingInput{{ID: "q", Text: "hello"}})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 1 || vectors[0].ID != "q" || len(vectors[0].Values) != 1 {
		t.Fatalf("vectors = %#v", vectors)
	}
}

func TestAnswerUnsupportedCapability(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}}
	_, err := eng.Answer(context.Background(), AnswerOptions{Question: "hello"})
	if !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("Answer error = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestAnswerContextOnlyUsesHybridRetrieval(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}}
	result, err := eng.Answer(context.Background(), AnswerOptions{Question: "hello", ContextOnly: true, Limit: 3})
	if err != nil {
		t.Fatalf("Answer context-only: %v", err)
	}
	if !result.ContextOnly {
		t.Fatalf("context_only = false")
	}
	if result.Answer != "" {
		t.Fatalf("answer = %q, want empty in context-only mode", result.Answer)
	}
	if len(result.Context) != 1 || !strings.Contains(result.Context[0].Content, "text evidence") {
		t.Fatalf("context = %#v", result.Context)
	}
	if result.Context[0].Citation != "[1]" {
		t.Fatalf("citation = %q, want [1]", result.Context[0].Citation)
	}
	if len(result.Sources) != 1 || result.Sources[0].Citation != "[1]" || result.Sources[0].Title == "" {
		t.Fatalf("sources = %#v", result.Sources)
	}
}

func TestAnswerRetrievalOptionsPassThroughToHybridSearch(t *testing.T) {
	hybridStore := &fakeHybridStore{}
	eng := &Engine{store: hybridStore, embedder: fakeEmbedder{}}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:     "hello",
		ContextOnly:  true,
		Limit:        3,
		TextWeight:   1,
		VectorWeight: 0,
		Filter: store.SearchFilter{
			FilePattern: "a.go",
			TargetKinds: []store.TargetKind{
				store.TargetSymbol,
			},
			Metadata: map[string]string{"language": "go"},
		},
	})
	if err != nil {
		t.Fatalf("Answer with retrieval options: %v", err)
	}
	if len(result.Context) != 1 {
		t.Fatalf("context = %#v", result.Context)
	}
	if hybridStore.textQuery.Query != "hello" || hybridStore.textQuery.Limit < 3 {
		t.Fatalf("text query = %#v", hybridStore.textQuery)
	}
	if hybridStore.textQuery.Filter.FilePattern != "a.go" ||
		len(hybridStore.textQuery.Filter.TargetKinds) != 1 ||
		hybridStore.textQuery.Filter.TargetKinds[0] != store.TargetSymbol ||
		hybridStore.textQuery.Filter.Metadata["language"] != "go" {
		t.Fatalf("text query filter = %#v", hybridStore.textQuery.Filter)
	}
	if len(hybridStore.vectorQuery.Vector) != 0 {
		t.Fatalf("vector query = %#v, want vector path skipped by weight", hybridStore.vectorQuery)
	}
}

func TestAnswerDelegatesToProvider(t *testing.T) {
	answerer := &fakeAnswerer{}
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}, answerer: answerer}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:     "hello",
		Template:     AnswerTemplateReview,
		SystemPrompt: "custom system prompt",
		Messages:     []store.AnswerMessage{{Role: "assistant", Content: "prior"}},
		Limit:        3,
		MaxTokens:    128,
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if result.Answer != "fake answer" || result.Provider != "fake" || result.Model != "fake-chat" {
		t.Fatalf("result = %#v", result)
	}
	if result.Template != AnswerTemplateReview {
		t.Fatalf("template = %q, want %q", result.Template, AnswerTemplateReview)
	}
	if answerer.request.Question != "hello" || answerer.request.MaxTokens != 128 {
		t.Fatalf("request = %#v", answerer.request)
	}
	if answerer.request.SystemPrompt != "custom system prompt" || len(answerer.request.Messages) != 1 {
		t.Fatalf("request prompt/messages = %#v", answerer.request)
	}
	if len(answerer.request.Context) != 1 {
		t.Fatalf("request context = %#v", answerer.request.Context)
	}
	if answerer.request.Context[0].Citation != "[1]" {
		t.Fatalf("request context citation = %#v", answerer.request.Context)
	}
}

func TestAnswerTemplatePassesPresetPrompt(t *testing.T) {
	answerer := &fakeAnswerer{}
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}, answerer: answerer}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question: "hello",
		Template: AnswerTemplatePlan,
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if result.Template != AnswerTemplatePlan {
		t.Fatalf("template = %q, want %q", result.Template, AnswerTemplatePlan)
	}
	if !strings.Contains(answerer.request.SystemPrompt, "implementation plan") ||
		!strings.Contains(answerer.request.SystemPrompt, "[1]") {
		t.Fatalf("system prompt = %q, want plan template with citation instruction", answerer.request.SystemPrompt)
	}
}

func TestAnswerGroundingAuditsCitations(t *testing.T) {
	answerer := &fakeAnswerer{answer: "Use Result for the answer [1], but this citation is unknown [9]."}
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}, answerer: answerer}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:         "hello",
		RequireCitations: true,
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if result.Grounding == nil {
		t.Fatalf("grounding = nil")
	}
	if !result.Grounding.Required || result.Grounding.Passed || result.Grounding.Grounded {
		t.Fatalf("grounding flags = %#v", result.Grounding)
	}
	if !reflect.DeepEqual(result.Grounding.ValidCitations, []string{"[1]"}) {
		t.Fatalf("valid citations = %#v", result.Grounding.ValidCitations)
	}
	if !reflect.DeepEqual(result.Grounding.MissingCitations, []string{"[9]"}) {
		t.Fatalf("missing citations = %#v", result.Grounding.MissingCitations)
	}
	if !strings.Contains(result.Grounding.Summary, "unknown sources") {
		t.Fatalf("grounding summary = %q", result.Grounding.Summary)
	}
}

func TestAnswerGroundingHonorsMinCoverage(t *testing.T) {
	answerer := &fakeAnswerer{answer: "Use the lexical match [1]."}
	eng := &Engine{store: &fakeHybridRankStore{}, embedder: fakeEmbedder{}, answerer: answerer}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:            "hello",
		MinCitationCoverage: 0.75,
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if result.Grounding == nil {
		t.Fatalf("grounding = nil")
	}
	if !result.Grounding.Required || result.Grounding.Passed || !result.Grounding.Grounded {
		t.Fatalf("grounding flags = %#v", result.Grounding)
	}
	if result.Grounding.MinCoverage != 0.75 {
		t.Fatalf("min coverage = %v", result.Grounding.MinCoverage)
	}
	if result.Grounding.Coverage >= result.Grounding.MinCoverage {
		t.Fatalf("coverage = %v, want below %v", result.Grounding.Coverage, result.Grounding.MinCoverage)
	}
	if len(result.Grounding.UncitedCitations) == 0 {
		t.Fatalf("uncited citations = %#v, want at least one", result.Grounding.UncitedCitations)
	}
	if !strings.Contains(result.Grounding.Summary, "below required") {
		t.Fatalf("grounding summary = %q", result.Grounding.Summary)
	}
}

func TestAnswerRejectsInvalidMinCitationCoverage(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}, answerer: &fakeAnswerer{}}
	_, err := eng.Answer(context.Background(), AnswerOptions{
		Question:            "hello",
		MinCitationCoverage: 1.5,
	})
	if err == nil || !strings.Contains(err.Error(), "min citation coverage") {
		t.Fatalf("Answer error = %v, want min citation coverage validation", err)
	}
}

func TestAnswerRejectsUnknownTemplate(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}}
	_, err := eng.Answer(context.Background(), AnswerOptions{
		Question:    "hello",
		Template:    "unknown",
		ContextOnly: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported answer template") {
		t.Fatalf("Answer error = %v, want unsupported answer template", err)
	}
}

func TestFormatAnswerMarkdown(t *testing.T) {
	out := FormatAnswerMarkdown(&AnswerResult{
		Question: "Where is Foo?",
		Answer:   "Foo is in a.go [1].",
		Summary:  "Answered question using one source",
		Sources: []AnswerSource{{
			Citation: "[1]",
			Title:    "a.go:3 Foo",
			Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "a.go", Name: "Foo", Line: 3},
			Source:   store.SearchSourceHybrid,
			Score:    0.9,
		}},
		Context: []store.AnswerContext{{
			Citation: "[1]",
			Content:  "func Foo() {}",
		}},
		Grounding: &AnswerGrounding{
			SourceCount:    1,
			HasCitations:   true,
			Grounded:       true,
			Passed:         true,
			ValidCitations: []string{"[1]"},
			Summary:        "Answer cited 1 of 1 retrieved sources.",
		},
		Usage: &store.AnswerUsage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14},
	})
	if !strings.Contains(out, "# Answer") ||
		!strings.Contains(out, "**Question:** Where is Foo?") ||
		!strings.Contains(out, "Foo is in a.go [1].") ||
		!strings.Contains(out, "## Sources") ||
		!strings.Contains(out, "- [1] `a.go:3 Foo` (hybrid, 0.9000)") ||
		!strings.Contains(out, "func Foo() {}") ||
		!strings.Contains(out, "## Grounding") ||
		!strings.Contains(out, "Answer cited 1 of 1 retrieved sources.") ||
		!strings.Contains(out, "prompt_tokens: 10") {
		t.Fatalf("unexpected markdown:\n%s", out)
	}
}

func TestSearchVectorUnsupportedCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	_, err = eng.SearchVector(context.Background(), store.VectorSearchQuery{
		Vector: []float32{1},
		Model:  "fake",
	})
	if !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("SearchVector error = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestSearchVectorTextEmbedsQueryAndSearchesVectorProvider(t *testing.T) {
	vectorStore := &fakeVectorStore{}
	eng := &Engine{store: vectorStore, embedder: fakeEmbedder{}}
	hits, err := eng.SearchVectorText(context.Background(), "hello", store.VectorSearchQuery{Limit: 3})
	if err != nil {
		t.Fatalf("SearchVectorText: %v", err)
	}
	if len(hits) != 1 || hits[0].Target.Name != "Result" {
		t.Fatalf("hits = %#v", hits)
	}
	if vectorStore.query.QueryText != "hello" || vectorStore.query.Model != "fake" || vectorStore.query.Dimensions != 1 {
		t.Fatalf("query = %#v", vectorStore.query)
	}
	if len(vectorStore.query.Vector) != 1 || vectorStore.query.Vector[0] != 1 {
		t.Fatalf("query vector = %#v", vectorStore.query.Vector)
	}
}

func TestSearchVectorTextRequiresEmbedder(t *testing.T) {
	eng := &Engine{store: &fakeVectorStore{}}
	_, err := eng.SearchVectorText(context.Background(), "hello", store.VectorSearchQuery{})
	if !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("SearchVectorText error = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestSearchHybridFusesTextAndVector(t *testing.T) {
	hybridStore := &fakeHybridStore{}
	eng := &Engine{store: hybridStore, embedder: fakeEmbedder{}}
	hits, err := eng.SearchHybrid(context.Background(), store.HybridSearchQuery{Query: "hello", Limit: 5})
	if err != nil {
		t.Fatalf("SearchHybrid: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %#v, want one fused hit", hits)
	}
	if hits[0].Source != store.SearchSourceHybrid || hits[0].Target.Name != "Result" {
		t.Fatalf("unexpected hit: %#v", hits[0])
	}
	if !strings.Contains(hits[0].Metadata["sources"], "text") || !strings.Contains(hits[0].Metadata["sources"], "vector") {
		t.Fatalf("metadata = %#v, want text and vector sources", hits[0].Metadata)
	}
	if hits[0].Metadata["hybrid_fusion"] != "weighted_normalized_sum" {
		t.Fatalf("metadata = %#v, want weighted normalized fusion", hits[0].Metadata)
	}
	if hits[0].Metadata["hybrid_text_normalized_score"] != "1.0000" || hits[0].Metadata["hybrid_vector_normalized_score"] != "1.0000" {
		t.Fatalf("metadata = %#v, want normalized source scores", hits[0].Metadata)
	}
	if hybridStore.vectorQuery.QueryText != "hello" || hybridStore.vectorQuery.Model != "fake" || hybridStore.vectorQuery.Dimensions != 1 {
		t.Fatalf("vector query = %#v", hybridStore.vectorQuery)
	}
}

func TestSearchHybridNormalizesSourceScoreScales(t *testing.T) {
	hybridStore := &fakeHybridRankStore{}
	eng := &Engine{store: hybridStore, embedder: fakeEmbedder{}}
	hits, err := eng.SearchHybrid(context.Background(), store.HybridSearchQuery{Query: "hello", Limit: 5})
	if err != nil {
		t.Fatalf("SearchHybrid: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("hits = %#v, want at least two hits", hits)
	}
	if hits[0].Target.Name != "SemanticMatch" {
		t.Fatalf("top hit = %#v, want SemanticMatch after score normalization", hits[0])
	}
	if hits[0].Metadata["hybrid_text_score"] != "10.0000" ||
		hits[0].Metadata["hybrid_text_normalized_score"] != "0.1000" ||
		hits[0].Metadata["hybrid_text_rank"] != "2" {
		t.Fatalf("text metadata = %#v, want raw score, normalized score, and rank", hits[0].Metadata)
	}
	if hits[0].Metadata["hybrid_vector_normalized_score"] != "1.0000" ||
		hits[0].Metadata["hybrid_vector_rank"] != "1" {
		t.Fatalf("vector metadata = %#v, want normalized score and rank", hits[0].Metadata)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatalf("scores = %.4f <= %.4f, want fused semantic match first", hits[0].Score, hits[1].Score)
	}
}

func TestSearchHybridRequiresSignal(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}}
	_, err := eng.SearchHybrid(context.Background(), store.HybridSearchQuery{})
	if err == nil || !strings.Contains(err.Error(), "requires query") {
		t.Fatalf("SearchHybrid error = %v, want missing query/vector/expand_from", err)
	}
}

func TestCapabilityNamesIncludesHybridForAdvancedProvider(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}}
	got := eng.capabilityNames()
	if !containsString(got, string(store.CapabilityHybridSearch)) {
		t.Fatalf("capabilities = %#v, want hybrid_search", got)
	}
}

func TestTraverseGraphUnsupportedCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	_, err = eng.TraverseGraph(context.Background(), store.GraphTraversalQuery{
		Start: store.TargetRef{Kind: store.TargetFile, Path: "a.go"},
	})
	if !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("TraverseGraph error = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestBestEffortGraphTraversalIgnoresUnsupportedCapability(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	if got := eng.graphTraversalForFile(context.Background(), "a.go", 2); got != nil {
		t.Fatalf("graphTraversalForFile on sqlite = %+v, want nil", got)
	}
}

func TestGraphTraversalDepthBounds(t *testing.T) {
	if got := graphTraversalDepth(0); got != 2 {
		t.Fatalf("graphTraversalDepth(0) = %d, want 2", got)
	}
	if got := graphTraversalDepth(9); got != 3 {
		t.Fatalf("graphTraversalDepth(9) = %d, want 3", got)
	}
}

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, inputs []store.EmbeddingInput) ([]store.EmbeddingVector, error) {
	out := make([]store.EmbeddingVector, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, store.EmbeddingVector{ID: input.ID, Values: []float32{1}, Model: "fake", Dimensions: 1})
	}
	return out, nil
}

func (fakeEmbedder) EmbeddingModel() store.EmbeddingModelInfo {
	return store.EmbeddingModelInfo{Provider: "fake", Model: "fake", Dimensions: 1}
}

type fakeAnswerer struct {
	request store.AnswerRequest
	answer  string
}

func (a *fakeAnswerer) Answer(_ context.Context, req store.AnswerRequest) (*store.AnswerResponse, error) {
	a.request = req
	answer := a.answer
	if answer == "" {
		answer = "fake answer"
	}
	return &store.AnswerResponse{Answer: answer, Model: "fake-chat"}, nil
}

func (a *fakeAnswerer) AnswerModel() store.AnswerModelInfo {
	return store.AnswerModelInfo{Provider: "fake", Model: "fake-chat"}
}

type fakeVectorStore struct {
	store.Store
	query store.VectorSearchQuery
}

func (s *fakeVectorStore) SearchVector(_ context.Context, query store.VectorSearchQuery) ([]store.SearchHit, error) {
	s.query = query
	return []store.SearchHit{{
		Target: store.TargetRef{Kind: store.TargetSymbol, Name: "Result"},
		Score:  1,
		Source: store.SearchSourceVector,
	}}, nil
}

type fakeHybridStore struct {
	store.Store
	textQuery   store.TextSearchQuery
	vectorQuery store.VectorSearchQuery
}

func (s *fakeHybridStore) SearchText(_ context.Context, query store.TextSearchQuery) ([]store.SearchHit, error) {
	s.textQuery = query
	return []store.SearchHit{{
		Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "a.go", Name: "Result", Line: 3},
		Score:    0.8,
		Source:   store.SearchSourceText,
		Evidence: "text evidence",
	}}, nil
}

func (s *fakeHybridStore) SearchVector(_ context.Context, query store.VectorSearchQuery) ([]store.SearchHit, error) {
	s.vectorQuery = query
	return []store.SearchHit{{
		Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "a.go", Name: "Result", Line: 3},
		Score:    0.9,
		Source:   store.SearchSourceVector,
		Evidence: "vector evidence",
	}}, nil
}

type fakeHybridRankStore struct {
	store.Store
}

func (s *fakeHybridRankStore) SearchText(_ context.Context, query store.TextSearchQuery) ([]store.SearchHit, error) {
	return []store.SearchHit{
		{
			Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "a.go", Name: "LexicalMatch", Line: 3},
			Score:    100,
			Source:   store.SearchSourceText,
			Evidence: "lexical evidence",
		},
		{
			Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "b.go", Name: "SemanticMatch", Line: 5},
			Score:    10,
			Source:   store.SearchSourceText,
			Evidence: "weaker lexical evidence",
		},
	}, nil
}

func (s *fakeHybridRankStore) SearchVector(_ context.Context, query store.VectorSearchQuery) ([]store.SearchHit, error) {
	return []store.SearchHit{{
		Target:   store.TargetRef{Kind: store.TargetSymbol, Path: "b.go", Name: "SemanticMatch", Line: 5},
		Score:    0.9,
		Source:   store.SearchSourceVector,
		Evidence: "semantic evidence",
	}}, nil
}
