package nozle

import (
	"context"
	"encoding/json"
	"net/url"
)

func (c *Client) Can(ctx context.Context, customerID, feature string) (CanResult, error) {
	return c.CanWithMetadata(ctx, customerID, feature, nil)
}

// CanWithMetadata includes feature-specific metadata as a JSON query value.
func (c *Client) CanWithMetadata(ctx context.Context, customerID, feature string, metadata map[string]any) (CanResult, error) {
	var out CanResult
	q := url.Values{"customer_id": {customerID}, "feature": {feature}}
	if metadata != nil {
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return out, err
		}
		q.Set("metadata", string(encoded))
	}
	err := c.request(ctx, "can", "GET", false, query("/api/v1/can", q), nil, &out, "")
	return out, err
}
