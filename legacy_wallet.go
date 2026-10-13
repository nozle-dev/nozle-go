package nozle

import "context"

// CheckAndDeductParams describes a legacy wallet debit. Use TrackUsage for product credits.
type CheckAndDeductParams struct {
	CustomerID string
	// Deprecated: ignored. It is not validated or sent; the deduction depends only on CustomerID and Credits.
	Feature string
	Credits float64
}
type CheckAndDeductResult struct {
	Allowed   bool    `json:"allowed"`
	Remaining float64 `json:"remaining"`
}

// CheckAndDeduct calls the legacy wallet adapter. It does not provide an idempotency key.
func (c *Client) CheckAndDeduct(ctx context.Context, p CheckAndDeductParams) (CheckAndDeductResult, error) {
	var out CheckAndDeductResult
	if err := required("check_and_deduct", "customer_id", p.CustomerID); err != nil {
		return out, err
	}
	if p.Credits <= 0 {
		return out, &ValidationError{"check_and_deduct", "credits must be positive"}
	}
	err := c.request(ctx, "check_and_deduct", "POST", false, "/api/v1/check-and-deduct", map[string]any{"customer_id": p.CustomerID, "credits": p.Credits}, &out, "")
	return out, err
}

// CreateTransactionID returns a random identifier for one logical Feature Event.
func CreateTransactionID() string { return generateUUID() }

// CreateCostEventID returns a random identifier for one logical Cost Event.
func CreateCostEventID() string { return generateUUID() }
