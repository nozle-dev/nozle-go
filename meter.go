package nozle

import (
	"context"
	"errors"
)

// Meter sends 1-100 metered events to Engine in one batch. An event without a TransactionID gets
// a generated one, written back into the caller's slice (events[i].TransactionID) before the first
// attempt. Retrying with the same slice, after an error, a timeout or a cancelled context, sends the
// same IDs, so Engine records each event once. Do not use the slice from another goroutine during
// the call.
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
		if e.TransactionID == "" {
			events[i].TransactionID = generateUUID()
		}
		id := events[i].TransactionID
		p := e.Metadata
		if p == nil {
			p = map[string]any{}
		}
		event := map[string]any{"transaction_id": id, "external_customer_id": e.CustomerID, "external_subscription_id": e.SubscriptionID, "code": e.Event, "properties": p}
		if e.Timestamp != "" {
			normalized, err := eventTimestamp(e.Timestamp)
			if err != nil {
				return err
			}
			event["timestamp"] = normalized
		}
		payload[i] = event
	}
	return c.request(ctx, "meter", "POST", false, "/api/v1/events/batch", map[string]any{"events": payload}, nil, "")
}
