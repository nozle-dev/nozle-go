package nozle

import (
	"context"
	"net/url"
)

// PlansOption sets a query parameter of Plans.
type PlansOption func(url.Values)

// WithCurrency lists family plans in this ISO currency. An unknown currency is a 422 *APIError.
func WithCurrency(code string) PlansOption {
	return func(q url.Values) { setNonEmpty(q, "currency", code) }
}

// WithExternalCustomerID lists family plans in this customer's locked currency when WithCurrency is not given.
func WithExternalCustomerID(id string) PlansOption {
	return func(q url.Values) { setNonEmpty(q, "external_customer_id", id) }
}

func setNonEmpty(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

// Plans returns the current plans. Family plans are listed in the WithCurrency currency, else the locked currency of
// the WithExternalCustomerID customer, else each family's default currency.
func (c *Client) Plans(ctx context.Context, opts ...PlansOption) ([]Plan, error) {
	var out struct {
		Plans []Plan `json:"plans"`
	}
	q := url.Values{}
	for _, opt := range opts {
		opt(q)
	}
	err := c.request(ctx, "plans", "GET", false, query("/api/v1/plans", q), nil, &out, "")
	return out.Plans, err
}
