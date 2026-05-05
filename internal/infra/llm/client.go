// Package llm provides a small, provider-agnostic interface for text
// summarization, plus an Anthropic Claude implementation and a no-op
// fallback for environments without an API key.
package llm

import "context"

// Options narrows summarization parameters that the bio service cares about.
// Provider-specific knobs are exposed via constructor args (NewAnthropic),
// not Options, so the interface stays minimal and provider-agnostic.
type Options struct {
	MaxTokens   int     // upper bound on output tokens (provider clamps if exceeded)
	Temperature float64 // 0–1; bio service passes 0.3
}

// Client summarizes input text in a few sentences. Implementations must
// fail closed on errors (return an error rather than a hallucinated string).
type Client interface {
	Summarize(ctx context.Context, input string, opts Options) (string, error)
}
