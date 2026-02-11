package llm

import (
	"context"
	"sync"
)

// MockResponse defines a canned response for the mock provider.
type MockResponse struct {
	Content string
	Err     error
}

// MockProvider is a test double that returns pre-configured responses in
// sequence. After all responses are exhausted, it keeps returning the last one.
// It records every request for later assertion.
type MockProvider struct {
	mu        sync.Mutex
	responses []MockResponse
	calls     []Request
	idx       int
}

// Compile-time check that MockProvider satisfies the Provider interface.
var _ Provider = (*MockProvider)(nil)

// NewMockProvider creates a mock that returns the given responses in order.
// If no responses are provided, Complete returns an empty Response.
func NewMockProvider(responses ...MockResponse) *MockProvider {
	return &MockProvider{
		responses: responses,
	}
}

// Complete returns the next canned response and records the request.
// It respects context cancellation.
func (m *MockProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, req)

	if len(m.responses) == 0 {
		return &Response{Content: "", Model: "mock"}, nil
	}

	r := m.responses[m.idx]
	if m.idx < len(m.responses)-1 {
		m.idx++
	}

	if r.Err != nil {
		return nil, r.Err
	}

	return &Response{
		Content: r.Content,
		Model:   "mock",
		Usage:   Usage{InputTokens: 10, OutputTokens: 5},
	}, nil
}

// Calls returns a copy of all requests received by this mock.
func (m *MockProvider) Calls() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Request, len(m.calls))
	copy(out, m.calls)
	return out
}

// Reset clears call history and resets the response index to zero.
func (m *MockProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = nil
	m.idx = 0
}
