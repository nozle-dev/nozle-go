package nozle

import "context"

// LLM looks up the cost of an LLM invocation.
func (c *Client) LLM(ctx context.Context, p LLMParams) (LLMResult, error) {
	body := map[string]interface{}{
		"model":         p.Model,
		"input_tokens":  p.InputTokens,
		"output_tokens": p.OutputTokens,
		"customer_id":   p.CustomerID,
	}
	resp, err := c.do(ctx, "POST", c.baseURL+"/api/v1/llm", body)
	if err != nil {
		return LLMResult{}, err
	}
	var out LLMResult
	if err := decodeJSON(resp, &out); err != nil {
		return LLMResult{}, err
	}
	return out, nil
}
