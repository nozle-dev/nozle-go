package nozle

import "context"

func (c *Client) Ping(ctx context.Context) (PingResult, error) {
	var out PingResult
	err := c.request(ctx, "ping", "GET", false, "/api/v1/ping", nil, &out, "")
	return out, err
}
