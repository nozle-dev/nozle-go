package nozle

import (
	"context"
	"errors"
)

// Meter sends 1-100 Core events in one batch.
func (c *Client) Meter(ctx context.Context, events []MeterEvent) error {
	if err := validateKey(c.apiKey, "meter"); err != nil {
		return err
	}
	if len(events) == 0 || len(events) > 100 {
		return errors.New("nozle: batch size must be 1-100")
	}
	payload := make([]map[string]any, len(events))
	for i, e := range events {
		if e.CustomerID == "" || e.Event == "" || e.SubscriptionID == "" {
			return &ValidationError{"meter", "customer, event and subscription IDs are required"}
		}
		id := e.TransactionID
		if id == "" {
			id = generateUUID()
		}
		p := e.Metadata
		if p == nil {
			p = map[string]any{}
		}
		event := map[string]any{"transaction_id": id, "external_customer_id": e.CustomerID, "external_subscription_id": e.SubscriptionID, "code": e.Event, "properties": p}
		if e.Timestamp != "" {
			event["timestamp"] = e.Timestamp
		}
		payload[i] = event
	}
	return c.request(ctx, "meter", "POST", true, "/api/v1/events/batch", map[string]any{"events": payload}, nil, "")
}
