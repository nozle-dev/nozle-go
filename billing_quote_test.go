package nozle

import (
	"encoding/json"
	"testing"
)

func TestSubscriptionChangePreviewPreservesDeferredAmountAndRenewal(t *testing.T) {
	var quote SubscriptionChangePreview
	input := []byte(`{"quote_id":"quote_123","amount_due_at_effective_cents":12345678901234567890,"renewal_at":"2026-11-01T00:00:00Z"}`)
	if err := json.Unmarshal(input, &quote); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(quote)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["amount_due_at_effective_cents"] == nil || fields["renewal_at"] != "2026-11-01T00:00:00Z" {
		t.Fatalf("quote lost deferred amount or renewal date: %s", encoded)
	}
}
