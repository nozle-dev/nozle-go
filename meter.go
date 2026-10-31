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
	_, err := c.MeterWithResult(ctx, events)
	return err
}

// MeterResult is the 200 body of a Meter batch: the events Engine accepted, and ErrorDetails for the events it
// refused, keyed by index ("2") then field. Engine 2.28.0 stores the valid events of a batch; an earlier Engine, or
// a batch with no valid event, answers a *APIError whose BatchErrors names the failing indexes and stores nothing.
type MeterResult struct {
	Events       []MeterAccepted                `json:"events"`
	ErrorDetails map[string]map[string][]string `json:"error_details,omitempty"`
}

// MeterAccepted is one accepted event as Engine echoes it.
type MeterAccepted struct {
	TransactionID          string         `json:"transaction_id"`
	Code                   string         `json:"code"`
	ExternalCustomerID     string         `json:"external_customer_id"`
	ExternalSubscriptionID string         `json:"external_subscription_id"`
	Timestamp              string         `json:"timestamp"`
	Properties             map[string]any `json:"properties"`
}

// MeterWithResult is Meter that also returns the accepted events. Same ID and retry rules as Meter.
func (c *Client) MeterWithResult(ctx context.Context, events []MeterEvent) (*MeterResult, error) {
	if err := validateKey(c.apiKey, "meter"); err != nil {
		return nil, err
	}
	if len(events) == 0 || len(events) > 100 {
		return nil, errors.New("nozle: batch size must be 1-100")
	}
	payload := make([]map[string]any, len(events))
	for i, e := range events {
		if e.CustomerID == "" || e.Event == "" || e.SubscriptionID == "" {
			return nil, &ValidationError{"meter", "customer, event and subscription IDs are required"}
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
				return nil, err
			}
			event["timestamp"] = normalized
		}
		payload[i] = event
	}
	var out MeterResult
	if err := c.request(ctx, "meter", "POST", false, "/api/v1/events/batch", map[string]any{"events": payload}, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}
