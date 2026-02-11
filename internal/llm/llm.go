// Package llm provides a provider-agnostic LLM client interface and
// implementations for use by stringer's analysis features.
package llm

import "context"

// Provider abstracts an LLM API behind a single synchronous completion method.
type Provider interface {
	// Complete sends a prompt to the LLM and returns the response.
	// Implementations must respect context cancellation and deadlines.
	Complete(ctx context.Context, req Request) (*Response, error)
}

// Request describes a single completion request.
type Request struct {
	// Prompt is the user message to send.
	Prompt string

	// Model overrides the provider's default model. If empty, the provider
	// uses its configured default.
	Model string

	// MaxTokens limits the response length. If zero, the provider uses its
	// own default.
	MaxTokens int

	// Temperature controls randomness. If nil, the provider uses its default.
	Temperature *float64

	// SystemPrompt sets the system instruction for the completion.
	SystemPrompt string
}

// Response holds the result of a completion call.
type Response struct {
	// Content is the text returned by the model.
	Content string

	// Model is the model that actually served the request (may differ from
	// the requested model if the provider remapped it).
	Model string

	// Usage reports token consumption.
	Usage Usage
}

// Usage tracks input and output token counts for a single request.
type Usage struct {
	InputTokens  int
	OutputTokens int
}
