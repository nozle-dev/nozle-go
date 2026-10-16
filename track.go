package nozle

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"strconv"
	"time"
)

// Track records a metered event through Engine. Supply a stable TransactionID when retrying.
func (c *Client) Track(ctx context.Context, p TrackParams) error {
	if err := validateKey(c.apiKey, "track"); err != nil {
		return err
	}
	if err := required("track", "customer_id", p.CustomerID); err != nil {
		return err
	}
	if err := required("track", "event", p.Event); err != nil {
		return err
	}
	sub, err := c.subscriptionOrActive(ctx, "track", p.CustomerID, p.SubscriptionID)
	if err != nil {
		return err
	}
	id := p.TransactionID
	if id == "" {
		id = generateUUID()
	}
	properties := p.Metadata
	if properties == nil {
		properties = map[string]any{}
	}
	event := map[string]any{"transaction_id": id, "external_customer_id": p.CustomerID, "external_subscription_id": sub, "code": p.Event, "properties": properties}
	if p.Timestamp != "" {
		normalized, err := eventTimestamp(p.Timestamp)
		if err != nil {
			return err
		}
		event["timestamp"] = normalized
	}
	return c.request(ctx, "track", "POST", false, "/api/v1/events", map[string]any{"event": event}, nil, "")
}
func generateUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	milliseconds := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(milliseconds)
		milliseconds >>= 8
	}
	b[6] = (b[6] & 15) | 0x70
	b[8] = (b[8] & 63) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func eventTimestamp(value string) (string, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		parsed = parsed.UTC()
		return fmt.Sprintf("%d.%03d", parsed.Unix(), parsed.Nanosecond()/1_000_000), nil
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return "", &ValidationError{"event", "timestamp must be RFC3339 or Unix seconds"}
	}
	return value, nil
}
