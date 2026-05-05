package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	anthropicEndpoint = "https://api.anthropic.com/v1/messages"
	anthropicVersion  = "2023-06-01"

	defaultMaxTokens   = 256
	defaultTemperature = 0.3
	maxTemperature     = 0.3 // hard cap per spec decision 59
)

// summarizeSystemPrompt is the locked instruction set for bio summaries.
const summarizeSystemPrompt = `You summarize Wikipedia content for music listeners.
- Output 2 to 3 sentences. No preamble, no bullet points, no quotes.
- Stick strictly to facts present in the source. Do not invent dates, names, or details.
- Keep it factual and neutral. Avoid adjectives like "iconic" or "legendary".`

// anthropicClient implements Client against the Anthropic Messages API.
type anthropicClient struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

// NewAnthropic builds an Anthropic Messages-API client. Pass model
// "claude-haiku-4-5-20251001" for the redesign default.
func NewAnthropic(apiKey, model string) Client {
	return newAnthropicWithEndpoint(apiKey, model, anthropicEndpoint, &http.Client{Timeout: 30 * time.Second})
}

// newAnthropicWithEndpoint is for tests.
func newAnthropicWithEndpoint(apiKey, model, endpoint string, h *http.Client) Client {
	return &anthropicClient{
		apiKey:   apiKey,
		model:    model,
		endpoint: endpoint,
		http:     h,
	}
}

type anthropicMessagesRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicMessagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Summarize sends one Messages-API call and returns the first text block.
// Temperature is hard-capped at 0.3 regardless of caller input.
func (c *anthropicClient) Summarize(ctx context.Context, input string, opts Options) (string, error) {
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = defaultMaxTokens
	}
	if opts.Temperature <= 0 {
		opts.Temperature = defaultTemperature
	}
	if opts.Temperature > maxTemperature {
		opts.Temperature = maxTemperature
	}

	reqBody, err := json.Marshal(anthropicMessagesRequest{
		Model:       c.model,
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
		System:      summarizeSystemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: input},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("anthropic status %d: %s", resp.StatusCode, string(body))
	}

	var out anthropicMessagesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode anthropic response: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("anthropic error: %s", out.Error.Message)
	}
	for _, block := range out.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}
	return "", nil
}
