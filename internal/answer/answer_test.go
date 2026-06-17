package answer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sjzsdu/code-context/internal/store"
)

func TestNewDisabledProvider(t *testing.T) {
	answerer, err := New(Options{})
	if err != nil {
		t.Fatalf("New disabled: %v", err)
	}
	if answerer != nil {
		t.Fatalf("disabled provider returned %#v", answerer)
	}
}

func TestOpenAICompatibleAnswerer(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s", req.Method)
		}
		if req.URL.String() != "http://answer.local/v1/chat/completions" {
			t.Fatalf("url = %s", req.URL.String())
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		var body openAIChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "chat-test" {
			t.Fatalf("model = %q", body.Model)
		}
		if body.MaxTokens != 256 {
			t.Fatalf("max_tokens = %d", body.MaxTokens)
		}
		if body.Temperature == nil || *body.Temperature != 0 {
			t.Fatalf("temperature = %v, want explicit request override 0", body.Temperature)
		}
		if len(body.Messages) != 2 {
			t.Fatalf("messages = %#v", body.Messages)
		}
		if body.Messages[0].Role != "system" || !strings.Contains(body.Messages[0].Content, "evidence") {
			t.Fatalf("system message = %#v", body.Messages[0])
		}
		if !strings.Contains(body.Messages[1].Content, "Where is status handled?") ||
			!strings.Contains(body.Messages[1].Content, "internal/server/server.go:79") ||
			!strings.Contains(body.Messages[1].Content, "handleStatus") ||
			!strings.Contains(body.Messages[1].Content, "Citation: [src-1]") {
			t.Fatalf("user prompt = %s", body.Messages[1].Content)
		}
		return jsonResponse(`{
			"model": "chat-test",
			"choices": [{"message": {"role": "assistant", "content": "Status is handled in server.go [1]."}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18}
		}`), nil
	})
	answerer, err := newOpenAICompatible(Options{
		Provider:    ProviderOpenAICompatible,
		BaseURL:     "http://answer.local/v1",
		APIKey:      "test-key",
		Model:       "chat-test",
		MaxTokens:   512,
		Temperature: 0.7,
	}, &http.Client{Transport: rt})
	if err != nil {
		t.Fatalf("newOpenAICompatible: %v", err)
	}

	temperature := 0.0
	resp, err := answerer.Answer(context.Background(), store.AnswerRequest{
		Question:    "Where is status handled?",
		MaxTokens:   256,
		Temperature: &temperature,
		Context: []store.AnswerContext{{
			Citation: "[src-1]",
			Target:   store.TargetRef{Kind: store.TargetFile, Path: "internal/server/server.go", Line: 79, Name: "handleStatus"},
			Source:   store.SearchSourceText,
			Score:    0.8,
			Content:  "mux.HandleFunc(\"/api/status\", s.handleStatus)",
		}},
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if resp.Answer != "Status is handled in server.go [1]." {
		t.Fatalf("answer = %q", resp.Answer)
	}
	if resp.Model != "chat-test" {
		t.Fatalf("model = %q", resp.Model)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 18 || resp.Usage.CompletionTokens != 8 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
	if resp.Metadata["finish_reason"] != "stop" {
		t.Fatalf("metadata = %#v", resp.Metadata)
	}
}

func TestBuildMessagesUsesCustomSystemPromptAndMessages(t *testing.T) {
	messages := buildMessages(store.AnswerRequest{
		Question:     "What changed?",
		SystemPrompt: "Custom system prompt",
		Messages: []store.AnswerMessage{
			{Role: "assistant", Content: "Prior context"},
		},
		Context: []store.AnswerContext{{Citation: "[1]", Content: "Evidence"}},
	})
	if len(messages) != 3 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].Role != "system" || messages[0].Content != "Custom system prompt" {
		t.Fatalf("system message = %#v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "Prior context" {
		t.Fatalf("prior message = %#v", messages[1])
	}
	if !strings.Contains(messages[2].Content, "Citation: [1]") {
		t.Fatalf("user message = %s", messages[2].Content)
	}
}

func TestOpenAICompatibleRequiresModelAndBaseURL(t *testing.T) {
	if _, err := New(Options{Provider: ProviderOpenAICompatible, Model: "m"}); err == nil {
		t.Fatalf("expected missing base URL error")
	}
	if _, err := New(Options{Provider: ProviderOpenAICompatible, BaseURL: "http://answer.local/v1"}); err == nil {
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
