package nozle

import "context"

type ConsumeParams struct {
	CustomerID, Feature string
	Units               int
	Metadata            map[string]string
}
type ConsumeUsage struct {
	Recorded bool   `json:"recorded"`
	EventID  string `json:"event_id,omitempty"`
}
type ConsumeResult struct {
	Allowed bool         `json:"allowed"`
	Reason  string       `json:"reason,omitempty"`
	Usage   ConsumeUsage `json:"usage"`
}

// Consume calls the legacy combined entitlement-and-record endpoint.
// Deprecated: the managed production API does not provide /api/v1/consume.
// Use Can and Track for separate entitlement checks and event submission, or
// TrackUsage for atomic product-credit consumption. Can followed by Track is
// not an atomic quota reservation.
func (c *Client) Consume(ctx context.Context, p ConsumeParams) (ConsumeResult, error) {
	var out ConsumeResult
	b := map[string]any{"customer_id": p.CustomerID, "feature": p.Feature}
	if p.Units > 0 {
		b["units"] = p.Units
	}
	if len(p.Metadata) > 0 {
		b["metadata"] = p.Metadata
	}
	err := c.request(ctx, "consume", "POST", false, "/api/v1/consume", b, &out, "")
	return out, err
}
