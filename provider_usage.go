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
//
// The breakdown fields are parts of the totals, and each token is counted once: a part above what is left of its
// total is clamped. CacheWriteTokens is the cache write at the default (5 minute) TTL, CacheWrite1hTokens the 1 hour
// one. ServiceTier "batch" prices the call's text input and output at the batch rates.
type Usage struct {
	Provider                                                                        string
	InputTokens, OutputTokens, CachedInputTokens, CacheWriteTokens, ReasoningTokens int
	CacheWrite1hTokens, AudioInputTokens, AudioOutputTokens                         int
	ImageInputTokens, ImageOutputTokens                                             int
	ServiceTier                                                                     string
}

// OpenAIUsage is the usage object of an OpenAI response. PromptTokens counts all input, including the cached,
// cache-write and audio parts; CompletionTokens counts all output, including reasoning and audio.
type OpenAIUsage struct {
	PromptTokens, CompletionTokens, CachedInputTokens, ReasoningTokens int
	CacheWriteTokens, AudioInputTokens, AudioOutputTokens              int
	ServiceTier                                                        string
}

// AnthropicUsage is the usage object of an Anthropic response. InputTokens is the input outside the cache;
// CacheWriteTokens is the cache write at the 5 minute TTL and CacheWrite1hTokens the 1 hour one.
type AnthropicUsage struct {
	InputTokens, CachedInputTokens, CacheWriteTokens, CacheWrite1hTokens, OutputTokens int
	ServiceTier                                                                        string
}
type tokenCategory struct {
	name   string
	tokens int
}

// tokenUsage is one call's usage: the inclusive totals and the disjoint token buckets.
type tokenUsage struct {
	input, output int
	categories    []tokenCategory
	batch         bool // the call was billed at the provider's batch tier
	estimated     bool // Nozle counted the tokens itself (an aborted stream)
	aborted       bool // the stream ended before the provider sent its final usage
}

// estimateBytesPerToken is the bytes per token for the estimate of an aborted stream. The same constant and rule in
// every Nozle SDK and the Engine.
const estimateBytesPerToken = 4

func estimateTokens(byteCount int) int {
	return (byteCount + estimateBytesPerToken - 1) / estimateBytesPerToken
}

// breakdownProperty maps the buckets that are parts of a total to the usage event's breakdown properties.
var breakdownProperty = map[string]string{
	"cached_input": "cached_input_tokens", "cache_write": "cache_write_tokens", "cache_write_1h": "cache_write_1h_tokens",
	"reasoning": "reasoning_tokens", "audio_input": "audio_input_tokens", "audio_output": "audio_output_tokens",
	"image_input": "image_input_tokens", "image_output": "image_output_tokens",
}

// properties are the usage event's properties that come from the usage: the breakdowns of the totals (left out when
// 0), service_tier for the batch tier, usage_source, and stream_aborted when true.
func (u tokenUsage) properties() map[string]any {
	properties := map[string]any{"usage_source": "reported"}
	for _, cat := range u.categories {
		if name, ok := breakdownProperty[cat.name]; ok && cat.tokens > 0 {
			properties[name] = cat.tokens
		}
	}
	if u.batch {
		properties["service_tier"] = "batch"
	}
	if u.estimated {
		properties["usage_source"] = "estimated"
	}
	if u.aborted {
		properties["stream_aborted"] = true
	}
	return properties
}

// costBucket is the cost-event type of a bucket: a batch-tier call prices its text tokens at the batch rates.
func (u tokenUsage) costBucket(name string) string {
	if u.batch && (name == "input" || name == "output") {
		return "batch_" + name
	}
	return name
}

// take takes each wanted part in order out of total, so no token lands in two buckets. rest is what no part took.
func take(total int, wanted ...int) (parts []int, rest int) {
	rest = total
	for _, w := range wanted {
		part := min(max(w, 0), rest)
		rest -= part
		parts = append(parts, part)
	}
	return parts, rest
}

// captureProvider records the usage event, through buf when it is set, then the cost events. queued, when set, runs
// once the usage event is sent or queued, before any cost event request.
func (c *Client) captureProvider(ctx context.Context, buf *EventBuffer, queued func(), provider string, p ProviderCapture, u tokenUsage) error {
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
	properties := map[string]any{"provider": provider, "model": p.Model, "input_tokens": u.input, "output_tokens": u.output, "latency_ms": p.Latency.Milliseconds()}
	for key, value := range u.properties() {
		properties[key] = value
	}
	if provider == "" {
		delete(properties, "provider")
	}
	if p.ProviderHost != "" {
		properties["provider_host"] = p.ProviderHost
	}
	if p.Feature != "" {
		properties["feature"] = p.Feature
	}
	event := TrackParams{CustomerID: p.CustomerID, SubscriptionID: p.SubscriptionID, Event: p.FeatureCode, Metadata: properties, TransactionID: p.TransactionID, Timestamp: p.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z")}
	var err error
	if buf != nil {
		err = buf.Track(event)
	} else {
		err = c.Track(ctx, event)
	}
	if queued != nil {
		queued()
	}
	if err != nil {
		return err
	}
	var captureErrors []error
	for _, cat := range u.categories {
		if cat.tokens <= 0 || p.CostMeterCode == "" {
			continue
		}
		bucket := u.costBucket(cat.name)
		ts := float64(p.Timestamp.Unix())
		_, err := c.TrackCostEvent(ctx, CostEventParams{CostEventID: p.TransactionID + ":" + bucket, CostMeterCode: p.CostMeterCode, ParentTransactionID: p.TransactionID, RequestID: p.RequestID, OperationKey: bucket, Properties: map[string]any{"tokens": cat.tokens, "provider": provider, "model": p.Model, "type": bucket}, Timestamp: &ts})
		if err != nil {
			captureErrors = append(captureErrors, err)
		}
	}
	return errors.Join(captureErrors...)
}

// CaptureOpenAIUsage records a completed OpenAI invocation and optional categorized cost events.
func (c *Client) CaptureOpenAIUsage(ctx context.Context, p ProviderCapture, u OpenAIUsage) error {
	return c.captureProvider(ctx, nil, nil, "openai", p, openAITokens(u))
}

func openAITokens(u OpenAIUsage) tokenUsage {
	input := max(u.PromptTokens, 0)
	output := max(u.CompletionTokens, 0)
	in, inRest := take(input, u.CachedInputTokens, u.CacheWriteTokens, u.AudioInputTokens)
	out, outRest := take(output, u.ReasoningTokens, u.AudioOutputTokens)
	return tokenUsage{input: input, output: output, batch: u.ServiceTier == "batch", categories: []tokenCategory{
		{"input", inRest}, {"cached_input", in[0]}, {"cache_write", in[1]}, {"audio_input", in[2]},
		{"output", outRest}, {"reasoning", out[0]}, {"audio_output", out[1]},
	}}
}

// CaptureAnthropicUsage records a completed Anthropic invocation and optional categorized cost events.
func (c *Client) CaptureAnthropicUsage(ctx context.Context, p ProviderCapture, u AnthropicUsage) error {
	return c.captureProvider(ctx, nil, nil, "anthropic", p, anthropicTokens(u))
}

func anthropicTokens(u AnthropicUsage) tokenUsage {
	input := max(u.InputTokens, 0)
	cached := max(u.CachedInputTokens, 0)
	write5m := max(u.CacheWriteTokens, 0)
	write1h := max(u.CacheWrite1hTokens, 0)
	output := max(u.OutputTokens, 0)
	return tokenUsage{input: input + cached + write5m + write1h, output: output, batch: u.ServiceTier == "batch", categories: []tokenCategory{
		{"input", input}, {"cached_input", cached}, {"cache_write", write5m}, {"cache_write_1h", write1h}, {"output", output},
	}}
}

// RecordUsage records one LLM call's token usage that your code read itself, with optional
// categorized cost events. Recording the same RequestID twice records one event.
func (c *Client) RecordUsage(ctx context.Context, p ProviderCapture, u Usage) error {
	if p.CustomerID == "" {
		return &ValidationError{"record_usage", "customer ID is required"}
	}
	input := max(u.InputTokens, 0)
	output := max(u.OutputTokens, 0)
	in, inRest := take(input, u.CachedInputTokens, u.CacheWriteTokens, u.CacheWrite1hTokens, u.AudioInputTokens, u.ImageInputTokens)
	out, outRest := take(output, u.ReasoningTokens, u.AudioOutputTokens, u.ImageOutputTokens)
	return c.captureProvider(ctx, nil, nil, u.Provider, p, tokenUsage{input: input, output: output, batch: u.ServiceTier == "batch", categories: []tokenCategory{
		{"input", inRest}, {"cached_input", in[0]}, {"cache_write", in[1]}, {"cache_write_1h", in[2]}, {"audio_input", in[3]}, {"image_input", in[4]},
		{"output", outRest}, {"reasoning", out[0]}, {"audio_output", out[1]}, {"image_output", out[2]},
	}})
}
