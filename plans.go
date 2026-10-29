package nozle

import (
	"context"
	"net/url"
)

// PlansOption sets a query parameter of Plans.
type PlansOption func(url.Values)

// WithCurrency lists only plans in this ISO currency. An unknown currency is a 422 *APIError.
func WithCurrency(code string) PlansOption {
	return func(q url.Values) { setNonEmpty(q, "currency", code) }
}

// WithExternalCustomerID (secret key only) lists only plans in this customer's locked currency when WithCurrency is
// not given. Core ignores it when the organization allows several currencies per customer.
func WithExternalCustomerID(id string) PlansOption {
	return func(q url.Values) { setNonEmpty(q, "external_customer_id", id) }
}

func setNonEmpty(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

// Plans returns the current plans. Without WithCurrency a secret key gets every plan in every currency; WithCurrency
// and WithExternalCustomerID filter to one currency. A publishable key without WithCurrency gets each family's
// default-currency plans.
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
