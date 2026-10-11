package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

func TestProviderCaptureClampsNegativeTokenCounts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		capture func(*nozle.Client) error
	}{
		{"OpenAI", func(c *nozle.Client) error {
			return c.CaptureOpenAIUsage(context.Background(), nozle.ProviderCapture{CustomerID: "c", SubscriptionID: "s", CostMeterCode: "ai"}, nozle.OpenAIUsage{PromptTokens: -4, CompletionTokens: -3, CachedInputTokens: -2, ReasoningTokens: -1})
		}},
		{"Anthropic", func(c *nozle.Client) error {
			return c.CaptureAnthropicUsage(context.Background(), nozle.ProviderCapture{CustomerID: "c", SubscriptionID: "s", CostMeterCode: "ai"}, nozle.AnthropicUsage{InputTokens: -1, CachedInputTokens: -2, CacheWriteTokens: -3, OutputTokens: -4})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input, output float64
			var costCalls int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/events":
					var payload struct {
						Event struct {
							Properties map[string]any `json:"properties"`
						} `json:"event"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Errorf("decode event: %v", err)
					}
					input, _ = payload.Event.Properties["input_tokens"].(float64)
					output, _ = payload.Event.Properties["output_tokens"].(float64)
					_, _ = w.Write([]byte(`{"event":{}}`))
				case "/api/v1/cost-events":
					costCalls++
					_, _ = w.Write([]byte(`{"status":"accepted"}`))
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			client := nozle.New("sk_test", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
			if err := tc.capture(client); err != nil {
				t.Fatal(err)
			}
			if input != 0 || output != 0 || costCalls != 0 {
				t.Fatalf("input=%v output=%v cost calls=%d, want all zero", input, output, costCalls)
			}
		})
	}
}

func TestProviderCaptureAttemptsEveryCostCategoryAfterOneFails(t *testing.T) {
	var costCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/events":
			_, _ = w.Write([]byte(`{"event":{}}`))
		case "/api/v1/cost-events":
			costCalls++
			if costCalls == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"publisher unavailable"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"accepted","cost_event_id":"cost-1"}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
	err := c.CaptureOpenAIUsage(context.Background(), nozle.ProviderCapture{
		CustomerID: "c", SubscriptionID: "s", CostMeterCode: "ai", TransactionID: "tx-1",
	}, nozle.OpenAIUsage{PromptTokens: 10, CachedInputTokens: 2, CompletionTokens: 5, ReasoningTokens: 1})
	var apiErr *nozle.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want HTTP 503 APIError, got %v", err)
	}
	if costCalls != 4 {
		t.Fatalf("cost category calls = %d, want 4", costCalls)
	}
}
