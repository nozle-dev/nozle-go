package nozle

import "context"

type listPlansResponse struct {
	Plans []Plan `json:"plans"`
}

// Plans returns all plans visible to the API key.
func (c *Client) Plans(ctx context.Context) ([]Plan, error) {
	resp, err := c.do(ctx, "GET", c.baseURL+"/api/v1/plans", nil)
	if err != nil {
		return nil, err
	}
	var out listPlansResponse
	if err := decodeJSON(resp, &out); err != nil {
		return nil, err
	}
	return out.Plans, nil
}
