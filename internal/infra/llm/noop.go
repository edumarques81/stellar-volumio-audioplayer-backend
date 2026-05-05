package llm

import "context"

// noopClient returns empty strings; used when no provider is configured.
type noopClient struct{}

// NewNoop returns a Client that never errors and always returns "".
// Used when ANTHROPIC_API_KEY is not set so the backend still boots
// cleanly in dev — the bios service degrades to empty summaries.
func NewNoop() Client { return noopClient{} }

func (noopClient) Summarize(_ context.Context, _ string, _ Options) (string, error) {
	return "", nil
}
