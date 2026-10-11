package nozle

import "context"

// ConsumeParams holds the fields for an atomic check-and-meter request.
type ConsumeParams struct {
	CustomerID string
	Feature    string
	Units      int
	Metadata   map[string]string
}

// ConsumeUsage describes whether the usage event was recorded.
type ConsumeUsage struct {
	Recorded bool   `json:"recorded"`
	EventID  string `json:"event_id,omitempty"`
}

// ConsumeResult is the response from the /api/v1/consume endpoint.
type ConsumeResult struct {
	Allowed bool         `json:"allowed"`
	Reason  string       `json:"reason,omitempty"`
	Usage   ConsumeUsage `json:"usage"`
}

// Consume atomically checks entitlements and records usage in a single request.
// If the entitlement check fails, no usage is recorded.
func (c *Client) Consume(ctx context.Context, p ConsumeParams) (ConsumeResult, error) {
	body := map[string]interface{}{
		"customer_id": p.CustomerID,
		"feature":     p.Feature,
	}
	if p.Units > 0 {
		body["units"] = p.Units
	}
	if len(p.Metadata) > 0 {
		body["metadata"] = p.Metadata
	}

	resp, err := c.do(ctx, "POST", c.baseURL+"/api/v1/consume", body)
	if err != nil {
		return ConsumeResult{}, err
	}
	var out ConsumeResult
	if err := decodeJSON(resp, &out); err != nil {
		return ConsumeResult{}, err
	}
	return out, nil
}
