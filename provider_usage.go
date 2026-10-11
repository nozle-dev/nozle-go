package nozle

import (
	"context"
	"errors"
	"time"
)

// ProviderCapture describes a completed provider call. It contains usage metadata only.
type ProviderCapture struct {
	CustomerID, SubscriptionID, FeatureCode, Feature, CostMeterCode, Model, RequestID, TransactionID string
	Latency                                                                                          time.Duration
	Timestamp                                                                                        time.Time
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
	if p.TransactionID == "" {
		p.TransactionID = generateUUID()
	}
	if p.Timestamp.IsZero() {
		p.Timestamp = time.Now().UTC()
	}
	properties := map[string]any{"provider": provider, "model": p.Model, "input_tokens": input, "output_tokens": output, "latency_ms": p.Latency.Milliseconds()}
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
