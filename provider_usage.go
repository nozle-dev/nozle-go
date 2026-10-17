package nozle

import (
	"context"
	"errors"
	"time"
)

// ProviderCapture describes a completed provider call. It contains usage metadata only.
// ProviderHost is the host the provider request went to, for example "openrouter.ai".
type ProviderCapture struct {
	CustomerID, SubscriptionID, FeatureCode, Feature, CostMeterCode, Model, RequestID, TransactionID string
	ProviderHost                                                                                     string
	Latency                                                                                          time.Duration
	Timestamp                                                                                        time.Time
}

// Usage is one LLM call's token usage, for RecordUsage. InputTokens counts all input, including
// cached input and cache writes; OutputTokens counts all output, including reasoning tokens.
type Usage struct {
	Provider                                                                        string
	InputTokens, OutputTokens, CachedInputTokens, CacheWriteTokens, ReasoningTokens int
}
type OpenAIUsage struct{ PromptTokens, CompletionTokens, CachedInputTokens, ReasoningTokens int }
type AnthropicUsage struct{ InputTokens, CachedInputTokens, CacheWriteTokens, OutputTokens int }
type tokenCategory struct {
	name   string
	tokens int
}

func (c *Client) captureProvider(ctx context.Context, provider string, p ProviderCapture, input, output int, categories []tokenCategory) error {
	if p.FeatureCode == "" {
		p.FeatureCode = "llm_tokens"
	}
	if p.TransactionID == "" && p.RequestID != "" {
		// The provider's response id makes the event id stable, so a response is recorded once.
		p.TransactionID = "llm-" + provider + "-" + p.RequestID
	}
	if p.TransactionID == "" {
		p.TransactionID = generateUUID()
	}
	if p.Timestamp.IsZero() {
		p.Timestamp = time.Now().UTC()
	}
	properties := map[string]any{"provider": provider, "model": p.Model, "input_tokens": input, "output_tokens": output, "latency_ms": p.Latency.Milliseconds()}
	if provider == "" {
		delete(properties, "provider")
	}
	if p.ProviderHost != "" {
		properties["provider_host"] = p.ProviderHost
	}
	if p.Feature != "" {
		properties["feature"] = p.Feature
	}
	if err := c.Track(ctx, TrackParams{CustomerID: p.CustomerID, SubscriptionID: p.SubscriptionID, Event: p.FeatureCode, Metadata: properties, TransactionID: p.TransactionID, Timestamp: p.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z")}); err != nil {
		return err
	}
	var captureErrors []error
	for _, cat := range categories {
		if cat.tokens <= 0 || p.CostMeterCode == "" {
			continue
		}
		ts := float64(p.Timestamp.Unix())
		_, err := c.TrackCostEvent(ctx, CostEventParams{CostEventID: p.TransactionID + ":" + cat.name, CostMeterCode: p.CostMeterCode, ParentTransactionID: p.TransactionID, RequestID: p.RequestID, OperationKey: cat.name, Properties: map[string]any{"tokens": cat.tokens, "provider": provider, "model": p.Model, "type": cat.name}, Timestamp: &ts})
		if err != nil {
			captureErrors = append(captureErrors, err)
		}
	}
	return errors.Join(captureErrors...)
}

// CaptureOpenAIUsage records a completed OpenAI invocation and optional categorized cost events.
func (c *Client) CaptureOpenAIUsage(ctx context.Context, p ProviderCapture, u OpenAIUsage) error {
	input := max(u.PromptTokens, 0)
	output := max(u.CompletionTokens, 0)
	cached := min(max(u.CachedInputTokens, 0), input)
	reasoning := min(max(u.ReasoningTokens, 0), output)
	return c.captureProvider(ctx, "openai", p, input, output, []tokenCategory{{"input", input - cached}, {"cached_input", cached}, {"output", output - reasoning}, {"reasoning", reasoning}})
}

// CaptureAnthropicUsage records a completed Anthropic invocation and optional categorized cost events.
func (c *Client) CaptureAnthropicUsage(ctx context.Context, p ProviderCapture, u AnthropicUsage) error {
	input := max(u.InputTokens, 0)
	cached := max(u.CachedInputTokens, 0)
	cacheWrite := max(u.CacheWriteTokens, 0)
	output := max(u.OutputTokens, 0)
	return c.captureProvider(ctx, "anthropic", p, input+cached+cacheWrite, output, []tokenCategory{{"input", input}, {"cached_input", cached}, {"cache_write", cacheWrite}, {"output", output}})
}

// RecordUsage records one LLM call's token usage that your code read itself, with optional
// categorized cost events. Recording the same RequestID twice records one event.
func (c *Client) RecordUsage(ctx context.Context, p ProviderCapture, u Usage) error {
	if p.CustomerID == "" {
		return &ValidationError{"record_usage", "customer ID is required"}
	}
	input := max(u.InputTokens, 0)
	output := max(u.OutputTokens, 0)
	cached := min(max(u.CachedInputTokens, 0), input)
	cacheWrite := min(max(u.CacheWriteTokens, 0), input-cached)
	reasoning := min(max(u.ReasoningTokens, 0), output)
	return c.captureProvider(ctx, u.Provider, p, input, output, []tokenCategory{{"input", input - cached - cacheWrite}, {"cached_input", cached}, {"cache_write", cacheWrite}, {"output", output - reasoning}, {"reasoning", reasoning}})
}
