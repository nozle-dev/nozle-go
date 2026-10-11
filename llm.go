package nozle

import "context"

func (c *Client) LLM(ctx context.Context, p LLMParams) (LLMResult, error) {
	var out LLMResult
	err := c.request(ctx, "llm", "POST", false, "/api/v1/llm", map[string]any{"model": p.Model, "input_tokens": p.InputTokens, "output_tokens": p.OutputTokens, "customer_id": p.CustomerID}, &out, "")
	return out, err
}
