package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropic_Summarize_HappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing/invalid x-api-key header: %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing anthropic-version header")
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "claude-haiku-4-5-20251001" {
			t.Errorf("unexpected model: %v", req["model"])
		}
		if temp, _ := req["temperature"].(float64); temp > 0.3 {
			t.Errorf("temperature %.2f exceeds 0.3 cap", temp)
		}
		if _, ok := req["system"]; !ok {
			t.Errorf("expected system prompt in request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Two-sentence summary."}]}`))
	}))
	t.Cleanup(srv.Close)

	c := newAnthropicWithEndpoint("test-key", "claude-haiku-4-5-20251001", srv.URL, srv.Client())
	out, err := c.Summarize(context.Background(), "Source text", Options{MaxTokens: 200})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if !strings.Contains(out, "summary") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestAnthropic_Summarize_HardCapsTemperatureAt03(t *testing.T) {
	t.Parallel()
	var observed float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		observed, _ = req["temperature"].(float64)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	c := newAnthropicWithEndpoint("k", "claude-haiku-4-5-20251001", srv.URL, srv.Client())
	// Caller asks for 0.9 — should be clamped to 0.3.
	if _, err := c.Summarize(context.Background(), "x", Options{Temperature: 0.9}); err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if observed > 0.3 {
		t.Fatalf("temperature was not capped: %.2f", observed)
	}
}

func TestAnthropic_Summarize_HTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"rate limit"}}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	c := newAnthropicWithEndpoint("k", "claude-haiku-4-5-20251001", srv.URL, srv.Client())
	if _, err := c.Summarize(context.Background(), "x", Options{}); err == nil {
		t.Fatalf("expected error on 429, got nil")
	}
}

func TestAnthropic_Summarize_ContextCanceled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"too late"}]}`))
	}))
	t.Cleanup(srv.Close)

	c := newAnthropicWithEndpoint("k", "claude-haiku-4-5-20251001", srv.URL, srv.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Summarize(ctx, "x", Options{}); err == nil {
		t.Fatalf("expected canceled-context error")
	}
}

func TestNoopClient_AlwaysEmpty(t *testing.T) {
	t.Parallel()
	c := NewNoop()
	got, err := c.Summarize(context.Background(), "anything", Options{})
	if err != nil {
		t.Fatalf("noop: %v", err)
	}
	if got != "" {
		t.Fatalf("noop should return empty, got %q", got)
	}
}
