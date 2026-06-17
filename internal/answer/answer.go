package answer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sjzsdu/code-context/internal/store"
)

const (
	ProviderNone             = "none"
	ProviderOpenAI           = "openai"
	ProviderOpenAICompatible = "openai-compatible"
)

const (
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	defaultTimeout       = 60 * time.Second
	defaultMaxTokens     = 1024
	defaultTemperature   = 0.2
	maxContextChars      = 24_000
	maxContextItemChars  = 4_000
)

var ErrDisabled = errors.New("answer provider disabled")

type Options struct {
	Provider    string
	BaseURL     string
	APIKey      string
	APIKeyEnv   string
	Model       string
	Timeout     time.Duration
	MaxTokens   int
	Temperature float64
}

func (o Options) ProviderOrDefault() string {
	provider := strings.TrimSpace(strings.ToLower(o.Provider))
	if provider == "" {
		return ProviderNone
	}
	return provider
}

func (o Options) ResolvedAPIKey() string {
	if strings.TrimSpace(o.APIKey) != "" {
		return strings.TrimSpace(o.APIKey)
	}
	if strings.TrimSpace(o.APIKeyEnv) == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(o.APIKeyEnv)))
}

func New(opts Options) (store.Answerer, error) {
	switch opts.ProviderOrDefault() {
	case ProviderNone:
		return nil, nil
	case ProviderOpenAI, ProviderOpenAICompatible:
		return newOpenAICompatible(opts, nil)
	default:
		return nil, fmt.Errorf("unsupported answer provider %q", opts.Provider)
	}
}

type openAICompatible struct {
	provider    string
	baseURL     string
	apiKey      string
	model       string
	maxTokens   int
	temperature float64
	client      *http.Client
}

func newOpenAICompatible(opts Options, client *http.Client) (*openAICompatible, error) {
	provider := opts.ProviderOrDefault()
	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" && provider == ProviderOpenAI {
		baseURL = defaultOpenAIBaseURL
	}
	if baseURL == "" {
		return nil, fmt.Errorf("answer base URL is required for provider %q", provider)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid answer base URL %q", baseURL)
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		return nil, fmt.Errorf("answer model is required for provider %q", provider)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	temperature := opts.Temperature
	if temperature == 0 {
		temperature = defaultTemperature
	}
	return &openAICompatible{
		provider:    provider,
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      opts.ResolvedAPIKey(),
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
		client:      client,
	}, nil
}

func (a *openAICompatible) AnswerModel() store.AnswerModelInfo {
	return store.AnswerModelInfo{
		Provider:    a.provider,
		Model:       a.model,
		BaseURL:     a.baseURL,
		MaxTokens:   a.maxTokens,
		Temperature: a.temperature,
	}
}

func (a *openAICompatible) Capabilities() []store.Capability {
	return []store.Capability{store.CapabilityAnswer}
}

func (a *openAICompatible) Answer(ctx context.Context, req store.AnswerRequest) (*store.AnswerResponse, error) {
	if strings.TrimSpace(req.Question) == "" && len(req.Messages) == 0 {
		return nil, fmt.Errorf("answer question or messages are required")
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = a.maxTokens
	}
	temperature := a.temperature
	if req.Temperature != nil {
		temperature = *req.Temperature
	}
	body := openAIChatCompletionRequest{
		Model:       a.model,
		Messages:    buildMessages(req),
		MaxTokens:   maxTokens,
		Temperature: &temperature,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("answer request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var parsed openAIChatCompletionResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("decode answer response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("answer response contained no choices")
	}
	answer := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if answer == "" {
		return nil, fmt.Errorf("answer response choice was empty")
	}
	model := parsed.Model
	if model == "" {
		model = a.model
	}
	return &store.AnswerResponse{
		Answer: answer,
		Model:  model,
		Usage: &store.AnswerUsage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		},
		Metadata: map[string]string{
			"provider":      a.provider,
			"finish_reason": parsed.Choices[0].FinishReason,
		},
	}, nil
}

func buildMessages(req store.AnswerRequest) []openAIChatMessage {
	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = "Answer using the supplied code-context evidence. Cite sources with [n] when possible. If the evidence is insufficient, say what is missing instead of inventing."
	}
	messages := []openAIChatMessage{{Role: "system", Content: systemPrompt}}
	for _, msg := range req.Messages {
		role := strings.TrimSpace(strings.ToLower(msg.Role))
		content := strings.TrimSpace(msg.Content)
		if role == "" || content == "" {
			continue
		}
		messages = append(messages, openAIChatMessage{Role: role, Content: content})
	}
	if strings.TrimSpace(req.Question) != "" || len(req.Context) > 0 {
		messages = append(messages, openAIChatMessage{Role: "user", Content: buildUserPrompt(req)})
	}
	return messages
}

func buildUserPrompt(req store.AnswerRequest) string {
	var b strings.Builder
	question := strings.TrimSpace(req.Question)
	if question != "" {
		fmt.Fprintf(&b, "Question:\n%s\n\n", question)
	}
	if len(req.Context) == 0 {
		b.WriteString("Context:\n(no retrieved code-context evidence was supplied)\n")
		return b.String()
	}
	b.WriteString("Context:\n")
	remaining := maxContextChars
	for i, item := range req.Context {
		if remaining <= 0 {
			fmt.Fprintf(&b, "\n[%d] ... context truncated ...\n", i+1)
			break
		}
		content := strings.TrimSpace(item.Content)
		if content == "" {
			content = strings.TrimSpace(item.Evidence)
		}
		if content == "" {
			continue
		}
		content = truncateString(content, minInt(maxContextItemChars, remaining))
		remaining -= len(content)
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = answerContextTargetLabel(item.Target)
		}
		fmt.Fprintf(&b, "\n[%d] %s\n", i+1, title)
		if item.Target.Kind != "" || item.Target.Path != "" || item.Target.Name != "" || item.Target.Value != "" {
			fmt.Fprintf(&b, "Target: %s\n", answerContextTargetLabel(item.Target))
		}
		if item.Source != "" || item.Score != 0 {
			fmt.Fprintf(&b, "Source: %s score=%.4f\n", item.Source, item.Score)
		}
		fmt.Fprintf(&b, "Content:\n%s\n", content)
	}
	return b.String()
}

func answerContextTargetLabel(target store.TargetRef) string {
	switch {
	case target.Path != "" && target.Name != "" && target.Line > 0:
		return fmt.Sprintf("%s:%d %s", target.Path, target.Line, target.Name)
	case target.Path != "" && target.Line > 0:
		return fmt.Sprintf("%s:%d", target.Path, target.Line)
	case target.Path != "" && target.Name != "":
		return target.Path + " " + target.Name
	case target.Path != "":
		return target.Path
	case target.Method != "" && target.RoutePath != "":
		return strings.TrimSpace(target.Method + " " + target.RoutePath)
	case target.Name != "":
		return target.Name
	case target.Value != "":
		return target.Value
	case target.Kind != "":
		return string(target.Kind)
	default:
		return "unknown"
	}
}

func truncateString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= len("...<truncated>") {
		return s[:max]
	}
	return s[:max-len("...<truncated>")] + "...<truncated>"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type openAIChatCompletionRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatCompletionResponse struct {
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Message      openAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
