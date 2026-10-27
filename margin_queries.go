package nozle

import (
	"context"
	"encoding/json"
	"math"
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

type MarginForecastPoint struct {
	Date             string `json:"date"`
	ProjectedRevenue int64  `json:"projected_revenue"`
	ProjectedCost    int64  `json:"projected_cost"`
	ProjectedMargin  int64  `json:"projected_margin"`
}

// MarginForecastResult projects daily revenue, cost and margin (cents) for the 30 days after the
// range; Confidence is "low", "medium" or "high".
type MarginForecastResult struct {
	Points     []MarginForecastPoint `json:"points"`
	Confidence string                `json:"confidence"`
}

type MarginSimulateParams struct {
	MetricCode      string  `json:"metric_code"`
	NewPriceDollars float64 `json:"new_price_dollars"` // 0 or more
}

// MarginSimulation replays one metric's usage at a new unit price; all amounts are cents.
type MarginSimulation struct {
	MetricCode          string `json:"metric_code"`
	CurrentRevenueCents int64  `json:"current_revenue_cents"`
	CurrentCostCents    int64  `json:"current_cost_cents"`
	CurrentMarginCents  int64  `json:"current_margin_cents"`
	NewRevenueCents     int64  `json:"new_revenue_cents"`
	NewCostCents        int64  `json:"new_cost_cents"`
	NewMarginCents      int64  `json:"new_margin_cents"`
	RevenueDeltaCents   int64  `json:"revenue_delta_cents"`
	MarginDeltaCents    int64  `json:"margin_delta_cents"`
	EventCount          int64  `json:"event_count"`
}

// MarginForecast projects the 30 days after the range in q (default: the last month).
func (c *Client) MarginForecast(ctx context.Context, q url.Values) (MarginForecastResult, error) {
	var out MarginForecastResult
	err := c.request(ctx, "margin.forecast", "GET", false, query("/api/v1/margin/forecast", q), nil, &out, "")
	return out, err
}

// MarginSimulate replays one metric's recorded usage in the range q at a new unit price; the
// recorded cost is kept.
func (c *Client) MarginSimulate(ctx context.Context, p MarginSimulateParams, q url.Values) (MarginSimulation, error) {
	var out MarginSimulation
	if err := required("margin.simulate", "metric_code", p.MetricCode); err != nil {
		return out, err
	}
	if p.NewPriceDollars < 0 || math.IsNaN(p.NewPriceDollars) || math.IsInf(p.NewPriceDollars, 0) {
		return out, &ValidationError{"margin.simulate", "new_price_dollars must be a number of 0 or more"}
	}
	err := c.request(ctx, "margin.simulate", "POST", false, query("/api/v1/margin/simulate", q), p, &out, "")
	return out, err
}
