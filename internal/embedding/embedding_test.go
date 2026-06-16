package embedding

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/store"
)

func TestCacheKeyIncludesModelDimensionsAndText(t *testing.T) {
	a := CacheKey("model-a", 3, "hello")
	b := CacheKey("model-a", 4, "hello")
	c := CacheKey("model-b", 3, "hello")
	d := CacheKey("model-a", 3, "hello")
	if a == b || a == c {
		t.Fatalf("cache key should change with model or dimensions")
	}
	if a != d {
		t.Fatalf("cache key should be deterministic")
	}
}

func TestNewDisabledProvider(t *testing.T) {
	embedder, err := New(Options{})
	if err != nil {
		t.Fatalf("New disabled: %v", err)
	}
	if embedder != nil {
		t.Fatalf("disabled provider returned %#v", embedder)
	}
}

func TestOpenAICompatibleEmbedder(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s", req.Method)
		}
		if req.URL.String() != "http://embedding.local/v1/embeddings" {
			t.Fatalf("url = %s", req.URL.String())
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		var body openAIEmbeddingRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "text-embedding-test" {
			t.Fatalf("model = %q", body.Model)
		}
		if body.Dimensions != 3 {
			t.Fatalf("dimensions = %d", body.Dimensions)
		}
		if len(body.Input) != 2 || body.Input[0] != "hello" || body.Input[1] != "world" {
			t.Fatalf("input = %#v", body.Input)
		}
		return jsonResponse(`{
			"model": "text-embedding-test",
			"data": [
				{"index": 0, "embedding": [0.1, 0.2, 0.3]},
				{"index": 1, "embedding": [0.4, 0.5, 0.6]}
			],
			"usage": {"prompt_tokens": 2, "total_tokens": 2}
		}`), nil
	})
	embedder, err := newOpenAICompatible(Options{
		Provider:   ProviderOpenAICompatible,
		BaseURL:    "http://embedding.local/v1",
		APIKey:     "test-key",
		Model:      "text-embedding-test",
		Dimensions: 3,
		BatchSize:  8,
	}, &http.Client{Transport: rt})
	if err != nil {
		t.Fatalf("newOpenAICompatible: %v", err)
	}

	vectors, err := embedder.Embed(context.Background(), []store.EmbeddingInput{
		{ID: "a", Text: "hello", Kind: store.EmbeddingInputQuery},
		{ID: "b", Text: "world", Kind: store.EmbeddingInputDocument},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("vectors = %d", len(vectors))
	}
	if vectors[0].ID != "a" || vectors[1].ID != "b" {
		t.Fatalf("ids = %#v", vectors)
	}
	if vectors[0].Dimensions != 3 || len(vectors[0].Values) != 3 {
		t.Fatalf("first dimensions = %d len=%d", vectors[0].Dimensions, len(vectors[0].Values))
	}
	if vectors[1].Values[2] != float32(0.6) {
		t.Fatalf("second vector = %#v", vectors[1].Values)
	}
	if vectors[0].Usage == nil || vectors[0].Usage.TotalTokens != 2 {
		t.Fatalf("usage = %#v", vectors[0].Usage)
	}
}

func TestOpenAICompatibleRequiresModelAndBaseURL(t *testing.T) {
	if _, err := New(Options{Provider: ProviderOpenAICompatible, Model: "m"}); err == nil {
		t.Fatalf("expected missing base URL error")
	}
	if _, err := New(Options{Provider: ProviderOpenAICompatible, BaseURL: "http://embedding.local/v1"}); err == nil {
		t.Fatalf("expected missing model error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
