package nozle

import "context"

// Ping verifies connectivity and API key validity.
func (c *Client) Ping(ctx context.Context) (PingResult, error) {
	resp, err := c.do(ctx, "GET", c.baseURL+"/api/v1/ping", nil)
	if err != nil {
		return PingResult{}, err
	}
	var out PingResult
	if err := decodeJSON(resp, &out); err != nil {
		return PingResult{}, err
	}
	return out, nil
}
