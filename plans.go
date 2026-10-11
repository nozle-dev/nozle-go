package nozle

import "context"

func (c *Client) Plans(ctx context.Context) ([]Plan, error) {
	var out struct {
		Plans []Plan `json:"plans"`
	}
	err := c.request(ctx, "plans", "GET", false, "/api/v1/plans", nil, &out, "")
	return out.Plans, err
}
