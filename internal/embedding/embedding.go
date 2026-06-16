package embedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	defaultTimeout       = 30 * time.Second
	defaultBatchSize     = 64
)

var ErrDisabled = errors.New("embedding provider disabled")

type Options struct {
	Provider   string
	BaseURL    string
	APIKey     string
	APIKeyEnv  string
	Model      string
	Dimensions int
	Timeout    time.Duration
	BatchSize  int
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

func New(opts Options) (store.Embedder, error) {
	switch opts.ProviderOrDefault() {
	case ProviderNone:
		return nil, nil
	case ProviderOpenAI, ProviderOpenAICompatible:
		return newOpenAICompatible(opts, nil)
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", opts.Provider)
	}
}

func CacheKey(model string, dimensions int, text string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(model),
		fmt.Sprintf("%d", dimensions),
		text,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

type openAICompatible struct {
	provider   string
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	batchSize  int
	client     *http.Client
}

func newOpenAICompatible(opts Options, client *http.Client) (*openAICompatible, error) {
	provider := opts.ProviderOrDefault()
	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" && provider == ProviderOpenAI {
		baseURL = defaultOpenAIBaseURL
	}
	if baseURL == "" {
		return nil, fmt.Errorf("embedding base URL is required for provider %q", provider)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid embedding base URL %q", baseURL)
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		return nil, fmt.Errorf("embedding model is required for provider %q", provider)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	return &openAICompatible{
		provider:   provider,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     opts.ResolvedAPIKey(),
		model:      model,
		dimensions: opts.Dimensions,
		batchSize:  batchSize,
		client:     client,
	}, nil
}

func (e *openAICompatible) EmbeddingModel() store.EmbeddingModelInfo {
	return store.EmbeddingModelInfo{
		Provider:   e.provider,
		Model:      e.model,
		Dimensions: e.dimensions,
		BaseURL:    e.baseURL,
		BatchSize:  e.batchSize,
	}
}

func (e *openAICompatible) Capabilities() []store.Capability {
	return []store.Capability{store.CapabilityEmbedding}
}

func (e *openAICompatible) Embed(ctx context.Context, inputs []store.EmbeddingInput) ([]store.EmbeddingVector, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	out := make([]store.EmbeddingVector, 0, len(inputs))
	for start := 0; start < len(inputs); start += e.batchSize {
		end := start + e.batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch, err := e.embedBatch(ctx, inputs[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

func (e *openAICompatible) embedBatch(ctx context.Context, inputs []store.EmbeddingInput) ([]store.EmbeddingVector, error) {
	texts := make([]string, len(inputs))
	for i, input := range inputs {
		text := strings.TrimSpace(input.Text)
		if text == "" {
			return nil, fmt.Errorf("embedding input %d is empty", i)
		}
		texts[i] = text
	}

	body := openAIEmbeddingRequest{
		Model: e.model,
		Input: texts,
	}
	if e.dimensions > 0 {
		body.Dimensions = e.dimensions
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var parsed openAIEmbeddingResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(parsed.Data) != len(inputs) {
		return nil, fmt.Errorf("embedding response count = %d, want %d", len(parsed.Data), len(inputs))
	}

	vectors := make([]store.EmbeddingVector, len(inputs))
	seen := make([]bool, len(inputs))
	model := parsed.Model
	if model == "" {
		model = e.model
	}
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(inputs) {
			return nil, fmt.Errorf("embedding response index %d out of range", item.Index)
		}
		if seen[item.Index] {
			return nil, fmt.Errorf("embedding response duplicated index %d", item.Index)
		}
		seen[item.Index] = true
		input := inputs[item.Index]
		values := make([]float32, len(item.Embedding))
		for i, v := range item.Embedding {
			values[i] = float32(v)
		}
		vectors[item.Index] = store.EmbeddingVector{
			ID:         input.ID,
			Values:     values,
			Model:      model,
			Dimensions: len(values),
			Target:     input.Target,
			Metadata:   input.Metadata,
			Usage: &store.EmbeddingUsage{
				PromptTokens: parsed.Usage.PromptTokens,
				TotalTokens:  parsed.Usage.TotalTokens,
			},
		}
	}
	for i, ok := range seen {
		if !ok {
			return nil, fmt.Errorf("embedding response missing index %d", i)
		}
	}
	return vectors, nil
}

type openAIEmbeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIEmbeddingResponse struct {
	Data  []openAIEmbedding `json:"data"`
	Model string            `json:"model"`
	Usage openAIUsage       `json:"usage"`
}

type openAIEmbedding struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type openAIUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
