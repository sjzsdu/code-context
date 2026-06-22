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

func TestAnswerProfileCatalog(t *testing.T) {
	infos := AnswerProfileCatalog()
	if len(infos) != len(AnswerProfiles()) {
		t.Fatalf("catalog len = %d, names len = %d", len(infos), len(AnswerProfiles()))
	}
	var foundReview bool
	for _, info := range infos {
		if info.Name == AnswerProfileReviewChange {
			foundReview = true
			if info.Template != AnswerTemplateReview || !info.RequireCitations || info.MinCitationCoverage <= 0 {
				t.Fatalf("review profile = %#v, want review template and grounding defaults", info)
			}
		}
	}
	if !foundReview {
		t.Fatalf("profiles = %#v, want review-change", infos)
	}
}

func TestAnswerProfileCatalogWithCustomProfiles(t *testing.T) {
	infos := AnswerProfileCatalogWithCustom([]AnswerProfileInfo{
		{
			Name:        "review_change",
			Description: "custom review override",
			Template:    AnswerTemplatePlan,
			Limit:       3,
		},
		{
			Name:        "custom-risk",
			Description: "custom profile",
			Template:    AnswerTemplateReview,
		},
	})
	var foundOverride, foundCustom bool
	for _, info := range infos {
		switch info.Name {
		case AnswerProfileReviewChange:
			foundOverride = true
			if info.Description != "custom review override" || info.Template != AnswerTemplatePlan || info.Limit != 3 {
				t.Fatalf("overridden review profile = %#v", info)
			}
		case "custom-risk":
			foundCustom = true
		}
	}
	if !foundOverride || !foundCustom {
		t.Fatalf("profiles = %#v, want override and custom profile", infos)
	}
}

func TestApplyAnswerProfileDefaultsToGeneral(t *testing.T) {
	profile, opts, err := applyAnswerProfileFromCatalog(AnswerOptions{}, AnswerProfileCatalog(), AnswerProfiles())
	if err != nil {
		t.Fatalf("applyAnswerProfileFromCatalog: %v", err)
	}
	if profile != AnswerProfileGeneral {
		t.Fatalf("profile = %q, want %q", profile, AnswerProfileGeneral)
	}
	if opts.Limit != 8 {
		t.Fatalf("limit = %d, want general default 8", opts.Limit)
	}
	if opts.Template != AnswerTemplateGeneral {
		t.Fatalf("template = %q, want %q", opts.Template, AnswerTemplateGeneral)
	}
}

func TestApplyAnswerProfileDefaultsToCustomGeneralOverride(t *testing.T) {
	catalog := AnswerProfileCatalogWithCustom([]AnswerProfileInfo{{
		Name:           "general",
		Description:    "custom general",
		Template:       AnswerTemplatePlan,
		Limit:          5,
		DedupeContext:  true,
		MaxPerFile:     1,
		TextWeight:     0.6,
		VectorWeight:   0.3,
		GraphWeight:    0.1,
		ExpandMaxDepth: 1,
	}})
	profile, opts, err := applyAnswerProfileFromCatalog(AnswerOptions{}, catalog, AnswerProfiles())
	if err != nil {
		t.Fatalf("applyAnswerProfileFromCatalog: %v", err)
	}
	if profile != AnswerProfileGeneral {
		t.Fatalf("profile = %q, want %q", profile, AnswerProfileGeneral)
	}
	if opts.Template != AnswerTemplatePlan {
		t.Fatalf("template = %q, want %q", opts.Template, AnswerTemplatePlan)
	}
	if opts.Limit != 5 {
		t.Fatalf("limit = %d, want 5", opts.Limit)
	}
	if !opts.DedupeContext {
		t.Fatal("expected dedupe context to be enabled by custom general override")
	}
	if opts.MaxPerFile != 1 {
		t.Fatalf("max_per_file = %d, want 1", opts.MaxPerFile)
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

func TestProviderDiagnosticsDisabled(t *testing.T) {
	root := t.TempDir()
	eng, err := New(root, filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	report, err := eng.ProviderDiagnostics(context.Background())
	if err != nil {
		t.Fatalf("ProviderDiagnostics: %v", err)
	}
	if !report.OK || len(report.Checks) != 2 {
		t.Fatalf("report = %#v, want ok disabled provider checks", report)
	}
	for _, check := range report.Checks {
		if check.Enabled || check.Status != "ok" || len(check.Actions) == 0 {
			t.Fatalf("check = %#v, want disabled ok check with action", check)
		}
	}
}

func TestProviderDiagnosticsReportsOpenAIAPIKeyErrors(t *testing.T) {
	root := t.TempDir()
	eng, err := NewWithOptions(root, Options{
		Store: store.Options{
			Backend: store.BackendSQLite,
			SQLite:  store.SQLiteOptions{Path: filepath.Join(root, "index.db")},
		},
		Embedding: embeddingpkg.Options{
			Provider: embeddingpkg.ProviderOpenAI,
			Model:    "text-embedding-test",
		},
		Answer: answerpkg.Options{
			Provider: answerpkg.ProviderOpenAI,
			Model:    "chat-test",
		},
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}
	defer eng.Close()

	report, err := eng.ProviderDiagnostics(context.Background())
	if err != nil {
		t.Fatalf("ProviderDiagnostics: %v", err)
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false for missing OpenAI API keys: %#v", report)
	}
	for _, check := range report.Checks {
		if check.Status != "error" || !strings.Contains(check.Message, "requires an API key") {
			t.Fatalf("check = %#v, want missing API key error", check)
		}
	}
}

func TestProviderDiagnosticsIncludesAnswerProfileChecks(t *testing.T) {
	root := t.TempDir()
	eng, err := NewWithOptions(root, Options{
		Store: store.Options{
			Backend: store.BackendSQLite,
			SQLite:  store.SQLiteOptions{Path: filepath.Join(root, "index.db")},
		},
		AnswerProfiles: []AnswerProfileInfo{
			{
				Name:        "custom_review",
				Description: "valid custom profile",
				Template:    AnswerTemplateReview,
				Filter:      store.SearchFilter{TargetKinds: []store.TargetKind{store.TargetSymbol}},
				Limit:       4,
			},
			{
				Name:                "bad-profile",
				Template:            "not-a-template",
				Filter:              store.SearchFilter{TargetKinds: []store.TargetKind{"unknown-kind"}},
				Limit:               -1,
				MinCitationCoverage: 1.5,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
	}
	defer eng.Close()

	report, err := eng.ProviderDiagnostics(context.Background())
	if err != nil {
		t.Fatalf("ProviderDiagnostics: %v", err)
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false for invalid answer profile: %#v", report)
	}
	var foundValid, foundInvalid bool
	for _, check := range report.Checks {
		if check.Kind != "answer_profile" {
			continue
		}
		switch check.Profile {
		case "custom-review":
			foundValid = true
			if check.Status != "ok" || !strings.Contains(check.Message, "valid") {
				t.Fatalf("valid profile check = %#v", check)
			}
		case "bad-profile":
			foundInvalid = true
			if check.Status != "error" ||
				!strings.Contains(check.Message, "unsupported template") ||
				!strings.Contains(check.Message, "unsupported target kind") ||
				!strings.Contains(check.Message, "min_citation_coverage") {
				t.Fatalf("invalid profile check = %#v", check)
			}
		}
	}
	if !foundValid || !foundInvalid {
		t.Fatalf("checks = %#v, want valid and invalid answer_profile checks", report.Checks)
	}

	doctor, err := eng.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	var foundDoctorProfile bool
	for _, check := range doctor.Checks {
		if check.Name == "answer_profile:bad-profile" && check.Status == "error" {
			foundDoctorProfile = true
		}
	}
	if !foundDoctorProfile {
		t.Fatalf("doctor checks = %#v, want answer_profile:bad-profile error", doctor.Checks)
	}
}

func TestProviderDiagnosticsIncludesSemanticRerankerCheck(t *testing.T) {
	eng := &Engine{
		embedder: semanticTestEmbedder{},
		options:  Options{AnswerRerankerProvider: AnswerRerankerSemantic},
	}
	check := eng.answerRerankerCheck()
	if check == nil || check.Kind != "answer_reranker" || check.Provider != AnswerRerankerSemantic || check.Status != "ok" {
		t.Fatalf("semantic reranker check = %#v", check)
	}

	eng = &Engine{options: Options{AnswerRerankerProvider: AnswerRerankerSemantic}}
	check = eng.answerRerankerCheck()
	if check == nil || check.Status != "error" || !strings.Contains(check.Message, "requires an embedding provider") {
		t.Fatalf("semantic reranker missing embedder check = %#v", check)
	}
}

func TestProviderDiagnosticsIncludesLLMEvaluatorCheck(t *testing.T) {
	eng := &Engine{
		answerer: &fakeAnswerer{},
		options:  Options{AnswerEvaluatorProvider: AnswerEvaluatorLLM},
	}
	check := eng.answerEvaluatorCheck()
	if check == nil || check.Kind != "answer_evaluator" || check.Provider != AnswerEvaluatorLLM || check.Status != "ok" {
		t.Fatalf("llm evaluator check = %#v", check)
	}

	eng = &Engine{options: Options{AnswerEvaluatorProvider: AnswerEvaluatorLLM}}
	check = eng.answerEvaluatorCheck()
	if check == nil || check.Status != "error" || !strings.Contains(check.Message, "requires an answer provider") {
		t.Fatalf("llm evaluator missing answerer check = %#v", check)
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

func TestSemanticAnswerRerankerUsesEmbeddingSimilarity(t *testing.T) {
	reranker := SemanticAnswerReranker{Embedder: semanticTestEmbedder{}}
	result, err := reranker.RerankAnswerHits(context.Background(), AnswerRerankInput{
		Question: "security authentication",
		Hits: []store.SearchHit{
			{Target: testTarget(store.TargetSymbol, "billing.go", "Billing", 1), Score: 0.9, Evidence: "invoice payment total"},
			{Target: testTarget(store.TargetSymbol, "auth.go", "Auth", 1), Score: 0.1, Evidence: "security authentication token"},
		},
		Options: AnswerRerankOptions{Limit: 2},
	})
	if err != nil {
		t.Fatalf("RerankAnswerHits: %v", err)
	}
	if result.Retrieval == nil || result.Retrieval.Retriever != "semantic-reranker" {
		t.Fatalf("retrieval = %#v, want semantic reranker", result.Retrieval)
	}
	if len(result.Hits) != 2 || result.Hits[0].Target.Name != "Auth" {
		t.Fatalf("hits = %#v, want semantic auth hit first", result.Hits)
	}
	if result.Hits[0].Metadata["rerank_provider"] != AnswerRerankerSemantic || result.Hits[0].Metadata["semantic_score"] == "" {
		t.Fatalf("semantic metadata = %#v", result.Hits[0].Metadata)
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

func TestAnswerProfileAppliesDefaults(t *testing.T) {
	hybridStore := &fakeHybridStore{}
	eng := &Engine{store: hybridStore, embedder: fakeEmbedder{}}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:    "hello",
		Profile:     AnswerProfileExplainCode,
		ContextOnly: true,
	})
	if err != nil {
		t.Fatalf("Answer with profile: %v", err)
	}
	if result.Profile != AnswerProfileExplainCode || result.Template != AnswerTemplateExplain {
		t.Fatalf("profile/template = %q/%q", result.Profile, result.Template)
	}
	if hybridStore.textQuery.Limit < 10 {
		t.Fatalf("text query limit = %d, want profile limit applied", hybridStore.textQuery.Limit)
	}
	if len(hybridStore.textQuery.Filter.TargetKinds) == 0 || hybridStore.textQuery.Filter.TargetKinds[0] != store.TargetSymbol {
		t.Fatalf("text query filter = %#v, want profile target kinds", hybridStore.textQuery.Filter)
	}
	if hybridStore.vectorQuery.QueryText != "hello" {
		t.Fatalf("vector query = %#v, want profile vector weight path", hybridStore.vectorQuery)
	}
}

func TestAnswerProfileExplicitOptionsOverrideDefaults(t *testing.T) {
	hybridStore := &fakeHybridStore{}
	eng := &Engine{store: hybridStore, embedder: fakeEmbedder{}}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:     "hello",
		Profile:      AnswerProfilePlanImplementation,
		Template:     AnswerTemplateExplain,
		ContextOnly:  true,
		TextWeight:   1,
		VectorWeight: 0,
		GraphWeight:  0,
		Filter:       store.SearchFilter{TargetKinds: []store.TargetKind{store.TargetMemory}},
	})
	if err != nil {
		t.Fatalf("Answer with profile override: %v", err)
	}
	if result.Profile != AnswerProfilePlanImplementation || result.Template != AnswerTemplateExplain {
		t.Fatalf("profile/template = %q/%q", result.Profile, result.Template)
	}
	if len(hybridStore.textQuery.Filter.TargetKinds) != 1 || hybridStore.textQuery.Filter.TargetKinds[0] != store.TargetMemory {
		t.Fatalf("text query filter = %#v, want explicit filter", hybridStore.textQuery.Filter)
	}
	if hybridStore.vectorQuery.QueryText != "" {
		t.Fatalf("vector query = %#v, want vector path skipped by explicit weights", hybridStore.vectorQuery)
	}
}

func TestAnswerCustomProfileAppliesRetrievalRerankAndEvaluationDefaults(t *testing.T) {
	hybridStore := &fakeAnswerRerankStore{hits: []store.SearchHit{
		{Target: testTarget(store.TargetSymbol, "a.go", "Keep", 1), Score: 0.9, Source: store.SearchSourceText, Evidence: "kept evidence with many words"},
		{Target: testTarget(store.TargetSymbol, "a.go", "Drop", 2), Score: 0.8, Source: store.SearchSourceText, Evidence: "same file drop"},
		{Target: testTarget(store.TargetSymbol, "b.go", "Low", 3), Score: 0.1, Source: store.SearchSourceText, Evidence: "low score drop"},
	}}
	eng := &Engine{
		store: hybridStore,
		options: Options{AnswerProfiles: []AnswerProfileInfo{{
			Name:                "custom-review",
			Description:         "custom review",
			Template:            AnswerTemplateReview,
			Filter:              store.SearchFilter{TargetKinds: []store.TargetKind{store.TargetSymbol}},
			Limit:               6,
			TextWeight:          1,
			MinContextScore:     0.5,
			DedupeContext:       true,
			MaxPerFile:          1,
			MaxContextItemChars: 16,
			RequireCitations:    true,
			MinCitationCoverage: 0.25,
			Evaluate:            true,
			MinEvaluationScore:  0.2,
		}}},
	}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:    "hello",
		Profile:     "custom_review",
		ContextOnly: true,
	})
	if err != nil {
		t.Fatalf("Answer with custom profile: %v", err)
	}
	if result.Profile != "custom-review" || result.Template != AnswerTemplateReview {
		t.Fatalf("profile/template = %q/%q", result.Profile, result.Template)
	}
	if len(result.Context) != 1 || result.Context[0].Target.Name != "Keep" {
		t.Fatalf("context = %#v", result.Context)
	}
	if result.Retrieval == nil || result.Retrieval.Dropped != 2 || result.Retrieval.MinContextScore != 0.5 || !result.Retrieval.DedupeContext {
		t.Fatalf("retrieval = %#v", result.Retrieval)
	}
	if result.Evaluation == nil || result.Evaluation.MinScore != 0.2 {
		t.Fatalf("evaluation = %#v", result.Evaluation)
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

func TestAnswerEvaluationReportsLocalChecks(t *testing.T) {
	answerer := &fakeAnswerer{answer: "Result uses text evidence from the retrieved symbol [1]."}
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}, answerer: answerer}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:           "hello",
		Evaluate:           true,
		MinEvaluationScore: 0.5,
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if result.Evaluation == nil {
		t.Fatalf("evaluation = nil")
	}
	if !result.Evaluation.Passed || result.Evaluation.Score < 0.5 {
		t.Fatalf("evaluation = %#v, want passing score", result.Evaluation)
	}
	if result.Evaluation.Evaluator != "local-rule" {
		t.Fatalf("evaluator = %q", result.Evaluation.Evaluator)
	}
	if !strings.Contains(FormatAnswerMarkdown(result), "## Evaluation") {
		t.Fatalf("markdown did not include evaluation section")
	}
}

func TestAnswerEvaluationUsesInjectedEvaluator(t *testing.T) {
	evaluator := &fakeAnswerEvaluator{}
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}, answerer: &fakeAnswerer{}, evaluator: evaluator}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question: "hello",
		Evaluate: true,
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if !evaluator.called {
		t.Fatalf("custom evaluator was not called")
	}
	if evaluator.input.Question != "hello" || len(evaluator.input.Context) == 0 {
		t.Fatalf("evaluator input = %#v", evaluator.input)
	}
	if result.Evaluation == nil || result.Evaluation.Evaluator != "fake-evaluator" {
		t.Fatalf("evaluation = %#v", result.Evaluation)
	}
}

func TestLLMAnswerEvaluatorUsesAnswerProviderJSON(t *testing.T) {
	answerer := &fakeLLMEvaluatorAnswerer{}
	eng := &Engine{
		store:     &fakeHybridStore{},
		embedder:  fakeEmbedder{},
		answerer:  answerer,
		evaluator: LLMAnswerEvaluator{Answerer: answerer},
	}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:           "hello",
		Evaluate:           true,
		MinEvaluationScore: 0.7,
		RequireCitations:   true,
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if result.Evaluation == nil || !strings.HasPrefix(result.Evaluation.Evaluator, "llm:fake/fake-chat") {
		t.Fatalf("evaluation = %#v", result.Evaluation)
	}
	if !result.Evaluation.Passed || result.Evaluation.Score < 0.7 {
		t.Fatalf("evaluation = %#v, want passing llm score", result.Evaluation)
	}
	if !answerer.evaluationCalled {
		t.Fatalf("llm evaluator did not call answer provider for evaluation")
	}
	if !answerEvaluationHasCheck(result.Evaluation, "faithfulness") || !answerEvaluationHasCheck(result.Evaluation, "local_guardrails") {
		t.Fatalf("checks = %#v, want llm and local guardrail checks", result.Evaluation.Checks)
	}
}

func TestAnswerRejectsInvalidMinEvaluationScore(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}, answerer: &fakeAnswerer{}}
	_, err := eng.Answer(context.Background(), AnswerOptions{
		Question:           "hello",
		MinEvaluationScore: 1.5,
	})
	if err == nil || !strings.Contains(err.Error(), "min evaluation score") {
		t.Fatalf("Answer error = %v, want min evaluation score validation", err)
	}
}

func TestAnswerRetrievalPostProcessing(t *testing.T) {
	hybridStore := &fakeAnswerRerankStore{hits: []store.SearchHit{
		{
			Target:   testTarget(store.TargetSymbol, "a.go", "Result", 3),
			Score:    0.9,
			Source:   store.SearchSourceText,
			Evidence: "alpha beta gamma delta epsilon zeta eta theta",
		},
		{
			Target:   testTarget(store.TargetSymbol, "a.go", "Result", 3),
			Score:    0.8,
			Source:   store.SearchSourceVector,
			Evidence: "duplicate evidence should be dropped",
		},
		{
			Target:   testTarget(store.TargetSymbol, "a.go", "Other", 9),
			Score:    0.7,
			Source:   store.SearchSourceText,
			Evidence: "same file should be dropped by max per file",
		},
		{
			Target:   testTarget(store.TargetSymbol, "b.go", "Low", 2),
			Score:    0.1,
			Source:   store.SearchSourceText,
			Evidence: "low score should be dropped",
		},
		{
			Target:   testTarget(store.TargetSymbol, "b.go", "Keep", 4),
			Score:    0.6,
			Source:   store.SearchSourceText,
			Evidence: "bravo charlie delta echo foxtrot",
		},
	}}
	eng := &Engine{store: hybridStore}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:            "hello",
		ContextOnly:         true,
		Limit:               10,
		MinContextScore:     0.5,
		DedupeContext:       true,
		MaxPerFile:          1,
		MaxContextItemChars: 20,
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(result.Context) != 2 {
		t.Fatalf("context len = %d, want 2: %#v", len(result.Context), result.Context)
	}
	if result.Context[0].Target.Path != "a.go" || result.Context[1].Target.Path != "b.go" {
		t.Fatalf("context targets = %#v", result.Context)
	}
	if len([]rune(result.Context[0].Content)) > 20 || result.Context[0].Metadata["context_truncated"] != "true" {
		t.Fatalf("first context content/metadata = %q %#v", result.Context[0].Content, result.Context[0].Metadata)
	}
	if result.Retrieval == nil || result.Retrieval.Selected != 2 || result.Retrieval.Dropped != 3 || !result.Retrieval.Truncated {
		t.Fatalf("retrieval = %#v", result.Retrieval)
	}
	if !strings.Contains(FormatAnswerMarkdown(result), "## Retrieval") {
		t.Fatalf("markdown did not include retrieval section")
	}
}

func TestAnswerRetrievalUsesInjectedReranker(t *testing.T) {
	reranker := &fakeAnswerReranker{}
	eng := &Engine{store: &fakeAnswerRerankStore{hits: []store.SearchHit{{
		Target:   testTarget(store.TargetSymbol, "a.go", "Result", 3),
		Score:    0.9,
		Source:   store.SearchSourceText,
		Evidence: "result evidence",
	}}}, reranker: reranker}
	result, err := eng.Answer(context.Background(), AnswerOptions{
		Question:    "hello",
		ContextOnly: true,
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if !reranker.called {
		t.Fatalf("custom reranker was not called")
	}
	if result.Retrieval == nil || result.Retrieval.Retriever != "fake-reranker" {
		t.Fatalf("retrieval = %#v", result.Retrieval)
	}
}

func TestAnswerRejectsInvalidRetrievalOptions(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}}
	_, err := eng.Answer(context.Background(), AnswerOptions{
		Question:        "hello",
		ContextOnly:     true,
		MinContextScore: -0.1,
	})
	if err == nil || !strings.Contains(err.Error(), "min context score") {
		t.Fatalf("Answer error = %v, want min context score validation", err)
	}
}

func TestAnswerRejectsUnknownProfile(t *testing.T) {
	eng := &Engine{store: &fakeHybridStore{}, embedder: fakeEmbedder{}}
	_, err := eng.Answer(context.Background(), AnswerOptions{
		Question:    "hello",
		Profile:     "unknown",
		ContextOnly: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported answer profile") {
		t.Fatalf("Answer error = %v, want unsupported answer profile", err)
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

type semanticTestEmbedder struct{}

func (semanticTestEmbedder) Embed(_ context.Context, inputs []store.EmbeddingInput) ([]store.EmbeddingVector, error) {
	out := make([]store.EmbeddingVector, 0, len(inputs))
	for _, input := range inputs {
		vector := []float32{0, 1}
		text := strings.ToLower(input.Text)
		if strings.Contains(text, "security") || strings.Contains(text, "auth") || strings.Contains(text, "token") {
			vector = []float32{1, 0}
		}
		out = append(out, store.EmbeddingVector{ID: input.ID, Values: vector, Model: "semantic-test", Dimensions: 2})
	}
	return out, nil
}

func (semanticTestEmbedder) EmbeddingModel() store.EmbeddingModelInfo {
	return store.EmbeddingModelInfo{Provider: "fake", Model: "semantic-test", Dimensions: 2}
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

type fakeLLMEvaluatorAnswerer struct {
	requests         []store.AnswerRequest
	evaluationCalled bool
}

func (a *fakeLLMEvaluatorAnswerer) Answer(_ context.Context, req store.AnswerRequest) (*store.AnswerResponse, error) {
	a.requests = append(a.requests, req)
	if req.Metadata["purpose"] == "answer_evaluation" {
		a.evaluationCalled = true
		return &store.AnswerResponse{
			Answer: `{"score":0.92,"passed":true,"summary":"LLM judge found the answer grounded.","checks":[{"name":"faithfulness","status":"pass","score":0.95,"message":"Claims are supported by context."},{"name":"completeness","status":"pass","score":0.9,"message":"The answer addresses the question."},{"name":"citation_quality","status":"pass","score":0.9,"message":"Citations use supplied labels."}]}`,
			Model:  "fake-chat",
		}, nil
	}
	return &store.AnswerResponse{
		Answer: "Result uses text evidence from the retrieved symbol [1].",
		Model:  "fake-chat",
	}, nil
}

func (a *fakeLLMEvaluatorAnswerer) AnswerModel() store.AnswerModelInfo {
	return store.AnswerModelInfo{Provider: "fake", Model: "fake-chat"}
}

func answerEvaluationHasCheck(eval *AnswerEvaluation, name string) bool {
	if eval == nil {
		return false
	}
	for _, check := range eval.Checks {
		if check.Name == name {
			return true
		}
	}
	return false
}

type fakeAnswerEvaluator struct {
	called bool
	input  AnswerEvaluationInput
}

func (e *fakeAnswerEvaluator) EvaluateAnswer(_ context.Context, input AnswerEvaluationInput) (*AnswerEvaluation, error) {
	e.called = true
	e.input = input
	return &AnswerEvaluation{
		Evaluator: "fake-evaluator",
		Score:     1,
		Passed:    true,
		Summary:   "fake evaluation passed",
	}, nil
}

type fakeAnswerReranker struct {
	called bool
	input  AnswerRerankInput
}

func (r *fakeAnswerReranker) RerankAnswerHits(_ context.Context, input AnswerRerankInput) (*AnswerRerankResult, error) {
	r.called = true
	r.input = input
	return &AnswerRerankResult{
		Hits: input.Hits,
		Retrieval: &AnswerRetrieval{
			Retriever:      "fake-reranker",
			RequestedLimit: input.Options.Limit,
			Retrieved:      len(input.Hits),
			Selected:       len(input.Hits),
			Summary:        "fake reranker selected all hits.",
		},
	}, nil
}

type fakeAnswerRerankStore struct {
	store.Store
	hits []store.SearchHit
}

func (s *fakeAnswerRerankStore) SearchHybrid(_ context.Context, query store.HybridSearchQuery) ([]store.SearchHit, error) {
	return append([]store.SearchHit(nil), s.hits...), nil
}

func testTarget(kind store.TargetKind, path string, name string, line int) store.TargetRef {
	return store.TargetRef{Kind: kind, Path: path, Name: name, Line: line}
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
