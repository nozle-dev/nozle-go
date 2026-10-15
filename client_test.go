package nozle_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

// ---------------------------------------------------------------------------
// Client construction
// ---------------------------------------------------------------------------

func TestNewClient_defaults(t *testing.T) {
	c := nozle.New("sk_test-key")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClient_rejectsEmptyAPIKey(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty apiKey")
		}
	}()
	nozle.New("")
}

func TestNewClient_withBaseURL(t *testing.T) {
	c := nozle.New("sk_test-key", nozle.WithBaseURL("https://custom.example.com"))
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClient_withEventsURL(t *testing.T) {
	c := nozle.New("sk_test-key", nozle.WithEventsURL("https://events.example.com"))
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

// ---------------------------------------------------------------------------
// Ping
// ---------------------------------------------------------------------------

func TestClient_Ping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/ping" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test-key" {
			t.Errorf("want Authorization: Bearer sk_test-key, got %s", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"engine":"ok"}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	result, err := c.Ping(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Error("expected ok=true")
	}
	if result.Engine != "ok" {
		t.Errorf("want engine=ok, got %s", result.Engine)
	}
}

func TestClient_Ping_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_bad-key", nozle.WithBaseURL(srv.URL))
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

// ---------------------------------------------------------------------------
// Can (entitlement check)
// ---------------------------------------------------------------------------

func TestClient_Can(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/can" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("customer_id") != "cust-1" {
			t.Errorf("want customer_id=cust-1, got %s", r.URL.Query().Get("customer_id"))
		}
		if r.URL.Query().Get("feature") != "api_calls" {
			t.Errorf("want feature=api_calls, got %s", r.URL.Query().Get("feature"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"allowed":true,"reason":"within_limit","used":5,"limit":100,"remaining":95,"cost_per_use_cents":10,"revenue_per_use_cents":15,"margin_per_use_cents":5}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	result, err := c.Can(context.Background(), "cust-1", "api_calls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed=true")
	}
	if result.Used != 5 {
		t.Errorf("want used=5, got %d", result.Used)
	}
	if result.Limit == nil || *result.Limit != 100 {
		t.Errorf("want limit=100, got %v", result.Limit)
	}
	if result.Remaining == nil || *result.Remaining != 95 {
		t.Errorf("want remaining=95, got %v", result.Remaining)
	}
}

// An unlimited feature has no allowance: the API sends null for limit and remaining (docs: guides/entitlements/can-endpoint),
// and the result keeps that as nil. A plain int read it as 0, which looks like "no allowance left".
func TestClient_Can_unlimitedFeatureHasNoLimitOrRemaining(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"allowed":true,"reason":"unlimited","used":7,"limit":null,"remaining":null}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	result, err := c.Can(context.Background(), "cust-1", "seats")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed || result.Used != 7 {
		t.Errorf("want allowed with used=7, got %+v", result)
	}
	if result.Limit != nil || result.Remaining != nil {
		t.Errorf("want nil limit and remaining for an unlimited feature, got limit=%v remaining=%v", result.Limit, result.Remaining)
	}
}

func TestClient_Can_denied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"allowed":false,"reason":"limit_exceeded","used":100,"limit":100,"remaining":0,"cost_per_use_cents":10,"revenue_per_use_cents":15,"margin_per_use_cents":5}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	result, err := c.Can(context.Background(), "cust-1", "api_calls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected allowed=false")
	}
	if result.Reason != "limit_exceeded" {
		t.Errorf("want reason=limit_exceeded, got %s", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// Track (single usage event)
// ---------------------------------------------------------------------------

func TestClient_Track(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/events" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(201)
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
	if err := c.Track(context.Background(), nozle.TrackParams{CustomerID: "cust-1", Event: "api_call", SubscriptionID: "sub-1", TransactionID: "tx-1", Metadata: map[string]any{"endpoint": "/users"}}); err != nil {
		t.Fatal(err)
	}
	e := got["event"].(map[string]any)
	if e["transaction_id"] != "tx-1" || e["external_subscription_id"] != "sub-1" || e["code"] != "api_call" {
		t.Fatalf("wrong event: %v", e)
	}
}
func TestClient_Track_resolvesSingleSubscription(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == "GET" {
			w.Write([]byte(`{"subscriptions":[{"external_id":"sub-1"}]}`))
			return
		}
		w.WriteHeader(201)
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
	if err := c.Track(context.Background(), nozle.TrackParams{CustomerID: "cust-1", Event: "api_call"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("want lookup + event, got %d calls", calls)
	}
}

func TestClient_Track_rejectsEmptyCustomerID(t *testing.T) {
	c := nozle.New("sk_test-key")
	err := c.Track(context.Background(), nozle.TrackParams{
		Event:          "api_call",
		SubscriptionID: "sub-1",
	})
	if err == nil {
		t.Fatal("expected error for empty customer ID")
	}
}

func TestClient_Track_rejectsEmptyEvent(t *testing.T) {
	c := nozle.New("sk_test-key")
	err := c.Track(context.Background(), nozle.TrackParams{
		CustomerID:     "cust-1",
		SubscriptionID: "sub-1",
	})
	if err == nil {
		t.Fatal("expected error for empty event")
	}
}

// ---------------------------------------------------------------------------
// Meter (batch usage events)
// ---------------------------------------------------------------------------

func TestClient_Meter(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/events/batch" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
	err := c.Meter(context.Background(), []nozle.MeterEvent{
		{CustomerID: "cust-1", Event: "api_call", SubscriptionID: "sub-1", TransactionID: "tx-1"},
		{CustomerID: "cust-2", Event: "api_call", SubscriptionID: "sub-2", TransactionID: "tx-2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events, ok := gotBody["events"].([]any)
	if !ok {
		t.Fatal("expected events array in request body")
	}
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
}

func TestClient_Meter_rejectsEmptyBatch(t *testing.T) {
	c := nozle.New("sk_test-key")
	err := c.Meter(context.Background(), []nozle.MeterEvent{})
	if err == nil {
		t.Fatal("expected error for empty batch")
	}
}

func TestClient_Meter_rejectsOversizedBatch(t *testing.T) {
	c := nozle.New("sk_test-key")
	events := make([]nozle.MeterEvent, 101)
	for i := range events {
		events[i] = nozle.MeterEvent{CustomerID: "c", Event: "e"}
	}
	err := c.Meter(context.Background(), events)
	if err == nil {
		t.Fatal("expected error for batch > 100")
	}
}

// ---------------------------------------------------------------------------
// Plans
// ---------------------------------------------------------------------------

func TestClient_Plans(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/plans" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test-key" {
			t.Errorf("want Authorization: Bearer sk_test-key, got %s", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"plans":[{"code":"starter","name":"Starter","amount_cents":999,"amount_currency":"USD","interval":"monthly"}]}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	plans, err := c.Plans(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("want 1 plan, got %d", len(plans))
	}
	if plans[0].Code != "starter" {
		t.Errorf("want plan code=starter, got %s", plans[0].Code)
	}
	if plans[0].AmountCents != 999 {
		t.Errorf("want amount_cents=999, got %d", plans[0].AmountCents)
	}
}

// ---------------------------------------------------------------------------
// Checkout
// ---------------------------------------------------------------------------

func TestClient_Checkout(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/checkout" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"client_secret":"cs_123","invoice_id":"inv-1","amount_cents":999,"currency":"USD"}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	result, err := c.Checkout(context.Background(), nozle.CheckoutParams{
		OrganizationID: "cust-1",
		PlanCode:       "starter",
		ReturnURL:      "https://example.com/success",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ClientSecret != "cs_123" {
		t.Errorf("want client_secret=cs_123, got %s", result.ClientSecret)
	}
	if result.AmountCents != 999 {
		t.Errorf("want amount_cents=999, got %d", result.AmountCents)
	}
	if gotBody["customer_id"] != "cust-1" {
		t.Errorf("want customer_id=cust-1, got %v", gotBody["customer_id"])
	}
	if gotBody["plan_code"] != "starter" {
		t.Errorf("want plan_code=starter, got %v", gotBody["plan_code"])
	}
}

// ---------------------------------------------------------------------------
// Subscribe
// ---------------------------------------------------------------------------

func TestClient_Subscribe(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/subscribe" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"subscription_id":"sub-123","status":"active"}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	result, err := c.Subscribe(context.Background(), nozle.SubscribeParams{
		CustomerID: "cust-1",
		PlanCode:   "starter",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SubscriptionID != "sub-123" {
		t.Errorf("want subscription_id=sub-123, got %s", result.SubscriptionID)
	}
	if result.Status != "active" {
		t.Errorf("want status=active, got %s", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Error responses
// ---------------------------------------------------------------------------

func TestClient_upstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_bad-key", nozle.WithBaseURL(srv.URL))
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestClient_APIError_statusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	_, err := c.Plans(context.Background())
	if err == nil {
		t.Fatal("expected error for 403 response")
	}

	var apiErr *nozle.APIError
	if ok := nozle.AsAPIError(err, &apiErr); !ok {
		t.Fatal("expected APIError")
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("want status 403, got %d", apiErr.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Debug mode
// ---------------------------------------------------------------------------

func TestClient_debugMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"org_id":"org-1","key_type":"live","env":"test"}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithDebug(true))
	_, err := c.Ping(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
