package nozle

import (
	"context"
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

func TestPastUsage(t *testing.T) {
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core/api/v1/customers/cus_1/past_usage":
			if r.URL.Query().Get("periods_count") != "2" || r.URL.Query().Get("external_subscription_id") != "sub_9" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"usage_periods":[` + usagePeriod + `,` + usagePeriod + `]}`))
		}
	})
	past, err := c.PastUsage(context.Background(), "cus_1", "sub_9", 2)
	if err != nil || len(past) != 2 {
		t.Fatalf("past %v %v", past, err)
	}
}
