package nozle

import (
	"context"
	"encoding/json"
	"net/url"
)

func (c *Client) marginQuery(ctx context.Context, path string, filters url.Values) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.request(ctx, "margin."+path, "GET", false, query("/api/v1/margin/"+path, filters), nil, &out, "")
	return out, err
}
func (c *Client) MarginSummary(ctx context.Context, q url.Values) (json.RawMessage, error) {
	return c.marginQuery(ctx, "summary", q)
}
func (c *Client) MarginByCustomer(ctx context.Context, q url.Values) (json.RawMessage, error) {
	return c.marginQuery(ctx, "customers", q)
}
func (c *Client) MarginByMetric(ctx context.Context, q url.Values) (json.RawMessage, error) {
	return c.marginQuery(ctx, "metrics", q)
}
func (c *Client) MarginByPlan(ctx context.Context, q url.Values) (json.RawMessage, error) {
	return c.marginQuery(ctx, "plans", q)
}
func (c *Client) MarginByModel(ctx context.Context, q url.Values) (json.RawMessage, error) {
	return c.marginQuery(ctx, "models", q)
}
func (c *Client) MarginTrend(ctx context.Context, granularity string, q url.Values) (json.RawMessage, error) {
	if granularity == "" {
		granularity = "day"
	}
	if granularity != "hour" && granularity != "day" && granularity != "week" && granularity != "month" {
		return nil, &ValidationError{"margin.trend", "invalid granularity"}
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("granularity", granularity)
	return c.marginQuery(ctx, "trend", q)
}
