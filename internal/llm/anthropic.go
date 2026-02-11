package llm

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	// defaultAnthropicModel is the model used when no override is provided.
	defaultAnthropicModel = "claude-sonnet-4-5-20250929"

	// defaultMaxTokens is the default maximum output tokens per request.
	defaultMaxTokens = 4096

	// defaultMaxRetries is the number of automatic retries on transient errors
	// (429 rate-limit, 5xx server errors). The SDK handles exponential backoff.
	defaultMaxRetries = 3
)

// AnthropicProvider implements Provider using the official Anthropic SDK.
type AnthropicProvider struct {
	client     anthropic.Client
	model      string
	maxRetries int
}

// Compile-time check that AnthropicProvider satisfies the Provider interface.
var _ Provider = (*AnthropicProvider)(nil)

// AnthropicOption configures an AnthropicProvider.
type AnthropicOption func(*anthropicConfig)

type anthropicConfig struct {
	apiKey     string
	model      string
	maxRetries int
}

// WithAPIKey sets the API key. If not provided, the provider reads
// ANTHROPIC_API_KEY from the environment.
func WithAPIKey(key string) AnthropicOption {
	return func(c *anthropicConfig) {
		c.apiKey = key
	}
}

// WithModel overrides the default model for all requests.
func WithModel(model string) AnthropicOption {
	return func(c *anthropicConfig) {
		c.model = model
	}
}

// WithMaxRetries sets the maximum number of retries for transient errors.
func WithMaxRetries(n int) AnthropicOption {
	return func(c *anthropicConfig) {
		c.maxRetries = n
	}
}

// NewAnthropicProvider creates a new Anthropic provider.
// It returns an error if no API key is available (neither via option nor env).
func NewAnthropicProvider(opts ...AnthropicOption) (*AnthropicProvider, error) {
	cfg := anthropicConfig{
		model:      defaultAnthropicModel,
		maxRetries: defaultMaxRetries,
	}
	for _, o := range opts {
		o(&cfg)
	}

	apiKey := cfg.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, errors.New("llm: ANTHROPIC_API_KEY not set and no API key provided")
	}

	clientOpts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(cfg.maxRetries),
	}

	client := anthropic.NewClient(clientOpts...)

	return &AnthropicProvider{
		client:     client,
		model:      cfg.model,
		maxRetries: cfg.maxRetries,
	}, nil
}

// Complete sends a completion request to the Anthropic Messages API.
func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	maxTokens := int64(defaultMaxTokens)
	if req.MaxTokens > 0 {
		maxTokens = int64(req.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt)),
		},
	}

	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: req.SystemPrompt},
		}
	}

	if req.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Temperature)
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic: completion failed: %w", err)
	}

	// Extract text from content blocks.
	var content string
	for _, block := range msg.Content {
		if variant, ok := block.AsAny().(anthropic.TextBlock); ok {
			content += variant.Text
		}
	}

	return &Response{
		Content: content,
		Model:   string(msg.Model),
		Usage: Usage{
			InputTokens:  int(msg.Usage.InputTokens),
			OutputTokens: int(msg.Usage.OutputTokens),
		},
	}, nil
}

// Model returns the default model configured for this provider.
func (p *AnthropicProvider) Model() string {
	return p.model
}

// MaxRetries returns the configured max retry count.
func (p *AnthropicProvider) MaxRetries() int {
	return p.maxRetries
}
