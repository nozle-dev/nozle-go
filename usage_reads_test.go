package nozle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const usagePeriod = `{"from_datetime":"2026-10-01T00:00:00Z","to_datetime":"2026-10-31T23:59:59Z","issuing_date":"2026-11-01",
"currency":"USD","amount_cents":1240,"taxes_amount_cents":0,"total_amount_cents":1240,
"charges_usage":[{"units":"31200.0","events_count":3,"amount_cents":1240,"amount_currency":"USD",
"charge":{"nozle_id":"c1","charge_model":"standard"},"feature":{"nozle_id":"f1","code":"api_calls","name":"API calls"}}]}`

func usageServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(handle))
	t.Cleanup(server.Close)
	return New("sk_test", WithHost(server.URL))
}

func TestCurrentUsageUsesTheActiveSubscription(t *testing.T) {
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core/api/v1/subscriptions":
			_, _ = w.Write([]byte(`{"subscriptions":[{"external_id":"sub_1"}]}`))
		case "/core/api/v1/customers/cus%201/current_usage", "/core/api/v1/customers/cus 1/current_usage":
			if r.URL.Query().Get("external_subscription_id") != "sub_1" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"customer_usage":` + usagePeriod + `}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	usage, err := c.CurrentUsage(context.Background(), "cus 1", "")
	if err != nil {
		t.Fatal(err)
	}
	if usage.AmountCents != 1240 || usage.ChargesUsage[0].Feature.Code != "api_calls" || usage.ChargesUsage[0].Units != "31200.0" {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestPastAndProjectedUsage(t *testing.T) {
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core/api/v1/customers/cus_1/past_usage":
			if r.URL.Query().Get("periods_count") != "2" || r.URL.Query().Get("external_subscription_id") != "sub_9" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"usage_periods":[` + usagePeriod + `,` + usagePeriod + `]}`))
		case "/core/api/v1/customers/cus_1/projected_usage":
			var period map[string]any
			_ = json.Unmarshal([]byte(usagePeriod), &period)
			period["projected_amount_cents"] = 3100
			_ = json.NewEncoder(w).Encode(map[string]any{"customer_projected_usage": period})
		}
	})
	past, err := c.PastUsage(context.Background(), "cus_1", "sub_9", 2)
	if err != nil || len(past) != 2 {
		t.Fatalf("past %v %v", past, err)
	}
	projected, err := c.ProjectedUsage(context.Background(), "cus_1", "sub_9")
	if err != nil || projected.ProjectedAmountCents != 3100 || projected.AmountCents != 1240 {
		t.Fatalf("projected %+v %v", projected, err)
	}
}

func TestProjectedUsageNotEnabledIsAnAPIError(t *testing.T) {
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":403,"error":"Forbidden","code":"projected_usage_not_enabled"}`))
	})
	_, err := c.ProjectedUsage(context.Background(), "cus_1", "sub_1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code() != "projected_usage_not_enabled" {
		t.Fatalf("err = %v", err)
	}
}
