package nozle

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestAlertLifecycle(t *testing.T) {
	type call struct {
		method, path string
		body         map[string]any
	}
	var calls []call
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, call{r.Method, r.URL.EscapedPath() + "?" + r.URL.RawQuery, body})
		if r.Method == http.MethodGet && r.URL.Path == "/core/api/v1/subscriptions/sub 1/alerts" {
			_, _ = w.Write([]byte(`{"alerts":[{"code":"api_80"}],"meta":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"alert":{"code":"api_80","thresholds":[{"code":null,"value":"40000.0","recurring":false}]}}`))
	})
	ctx := context.Background()
	target := AlertTarget{SubscriptionID: "sub 1"}
	created, err := c.CreateAlert(ctx, target, AlertInput{
		AlertType: "feature_current_usage_units", Code: "api_80", FeatureCode: "api_calls",
		Thresholds: []AlertThreshold{{Value: "40000"}, {Value: "10000", Recurring: true}},
	})
	if err != nil || created.Thresholds[0].Value != "40000.0" {
		t.Fatalf("create %+v %v", created, err)
	}
	if list, err := c.ListAlerts(ctx, target, 2); err != nil || list[0].Code != "api_80" {
		t.Fatalf("list %v %v", list, err)
	}
	if _, err := c.UpdateAlert(ctx, target, "api_80", AlertInput{Name: "API at 80%"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeleteAlert(ctx, target, "api_80"); err != nil {
		t.Fatal(err)
	}
	want := []call{
		{"POST", "/core/api/v1/subscriptions/sub%201/alerts?", map[string]any{"alert": map[string]any{
			"alert_type": "feature_current_usage_units", "code": "api_80", "feature_code": "api_calls",
			"thresholds": []any{map[string]any{"value": 40000.0}, map[string]any{"value": 10000.0, "recurring": true}},
		}}},
		{"GET", "/core/api/v1/subscriptions/sub%201/alerts?page=2", nil},
		{"PUT", "/core/api/v1/subscriptions/sub%201/alerts/api_80?", map[string]any{"alert": map[string]any{"name": "API at 80%"}}},
		{"DELETE", "/core/api/v1/subscriptions/sub%201/alerts/api_80?", nil},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls\n%v\nwant\n%v", calls, want)
	}
}

func TestWalletAlertPathAndValidation(t *testing.T) {
	var path string
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"alert":{"code":"low"}}`))
	})
	if _, err := c.GetAlert(context.Background(), AlertTarget{CustomerID: "cus_1", WalletCode: "main"}, "low"); err != nil {
		t.Fatal(err)
	}
	if path != "/core/api/v1/customers/cus_1/wallets/main/alerts/low" {
		t.Fatalf("path %s", path)
	}
	if _, err := c.ListAlerts(context.Background(), AlertTarget{CustomerID: "cus_1"}, 0); err == nil {
		t.Fatal("expected wallet_code to be required")
	}
}
