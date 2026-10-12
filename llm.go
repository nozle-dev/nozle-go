package nozle

import "context"

// LLM calls the legacy token-price lookup endpoint.
// Deprecated: the managed production API does not provide /api/v1/llm.
// Use configured cost meters with TrackCostEvent or CaptureOpenAIUsage /
// CaptureAnthropicUsage to record provider usage; these are asynchronous
// recording APIs, not a synchronous replacement for this price lookup.
func (c *Client) LLM(ctx context.Context, p LLMParams) (LLMResult, error) {
	var out LLMResult
	err := c.request(ctx, "llm", "POST", false, "/api/v1/llm", map[string]any{"model": p.Model, "input_tokens": p.InputTokens, "output_tokens": p.OutputTokens, "customer_id": p.CustomerID}, &out, "")
	return out, err
}
