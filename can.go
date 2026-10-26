package nozle

import (
	"context"
	"encoding/json"
	"net/url"
)

func (c *Client) Can(ctx context.Context, customerID, feature string) (CanResult, error) {
	return c.CanWithMetadata(ctx, customerID, feature, nil)
}

// CanOptions chooses what Can evaluates. All fields are optional.
type CanOptions struct {
	// Metadata is feature-specific metadata, sent as a JSON query value.
	Metadata map[string]any
	// SubscriptionID is the external ID of the subscription to evaluate. A customer with several
	// subscriptions needs it, or EntityID; without one Can returns Allowed=false with Reason
	// "subscription_required".
	SubscriptionID string
	// EntityID is the external ID of the Entity whose subscription is evaluated.
	EntityID string
}

// CanWithMetadata includes feature-specific metadata as a JSON query value.
func (c *Client) CanWithMetadata(ctx context.Context, customerID, feature string, metadata map[string]any) (CanResult, error) {
	return c.CanWithOptions(ctx, customerID, feature, CanOptions{Metadata: metadata})
}

// CanWithOptions is Can with a subscription or Entity selector and optional metadata.
func (c *Client) CanWithOptions(ctx context.Context, customerID, feature string, opts CanOptions) (CanResult, error) {
	var out CanResult
	q := url.Values{"customer_id": {customerID}, "feature": {feature}}
	if opts.SubscriptionID != "" {
		q.Set("subscription_id", opts.SubscriptionID)
	}
	if opts.EntityID != "" {
		q.Set("entity_id", opts.EntityID)
	}
	if opts.Metadata != nil {
		encoded, err := json.Marshal(opts.Metadata)
		if err != nil {
			return out, err
		}
		q.Set("metadata", string(encoded))
	}
	err := c.request(ctx, "can", "GET", false, query("/api/v1/can", q), nil, &out, "")
	return out, err
}
