package nozle

import (
	"context"
	"net/url"
)

// Can checks whether a customer is allowed to use a feature.
func (c *Client) Can(ctx context.Context, customerID, feature string) (CanResult, error) {
	u, err := url.Parse(c.baseURL + "/api/v1/can")
	if err != nil {
		return CanResult{}, err
	}
	q := u.Query()
	q.Set("customer_id", customerID)
	q.Set("feature", feature)
	u.RawQuery = q.Encode()

	resp, err := c.do(ctx, "GET", u.String(), nil)
	if err != nil {
		return CanResult{}, err
	}
	var out CanResult
	if err := decodeJSON(resp, &out); err != nil {
		return CanResult{}, err
	}
	return out, nil
}
