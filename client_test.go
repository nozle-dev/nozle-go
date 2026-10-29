package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

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

func TestClient_Track_cachesSubscriptionUntilItChanges(t *testing.T) {
	lookups := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET":
			lookups++
			w.Write([]byte(`{"subscriptions":[{"external_id":"sub-1"}]}`))
		case r.Method == "DELETE":
			w.Write([]byte(`{"subscription":{"external_id":"sub-1","status":"terminated"}}`))
		default:
			w.WriteHeader(201)
		}
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := c.Track(ctx, nozle.TrackParams{CustomerID: "cust-1", Event: "api_call"}); err != nil {
			t.Fatal(err)
		}
	}
	if lookups != 1 {
		t.Fatalf("want one lookup for two tracks, got %d", lookups)
	}
	if _, err := c.CancelSubscription(ctx, "cust-1", "sub-1", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.Track(ctx, nozle.TrackParams{CustomerID: "cust-1", Event: "api_call"}); err != nil {
		t.Fatal(err)
	}
	if lookups != 2 {
		t.Fatalf("want a fresh lookup after cancel, got %d", lookups)
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
		w.Write([]byte(`{"type":"hosted","url":"https://checkout.stripe.com/c/pay/cs_123","payment_url":"https://checkout.stripe.com/c/pay/cs_123","invoice_id":"inv-1","amount_cents":999,"currency":"USD"}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	result, err := c.Checkout(context.Background(), nozle.CheckoutParams{
		OrganizationID: "cust-1",
		PlanCode:       "starter",
		ReturnURL:      "https://example.com/success?s={CHECKOUT_SESSION_ID}",
		CancelURL:      "https://example.com/pricing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != "hosted" || result.URL != "https://checkout.stripe.com/c/pay/cs_123" || result.PaymentURL != result.URL {
		t.Errorf("want hosted result with url, got %+v", result)
	}
	if gotBody["cancel_url"] != "https://example.com/pricing" || gotBody["return_url"] != "https://example.com/success?s={CHECKOUT_SESSION_ID}" {
		t.Errorf("cancel_url / return_url not sent: %v", gotBody)
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

func TestClient_APIError_requestIDAndRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		name, retryAfter string
		wantMin, wantMax time.Duration
	}{
		{"seconds", "30", 30 * time.Second, 30 * time.Second},
		{"date", time.Now().Add(20 * time.Second).UTC().Format(http.TimeFormat), time.Second, 20 * time.Second},
		{"invalid", "soon", 0, 0},
		{"absent", "", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-Id", "req-1")
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer srv.Close()
			_, err := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL)).Plans(context.Background())
			var apiErr *nozle.APIError
			if !nozle.AsAPIError(err, &apiErr) {
				t.Fatal("expected APIError")
			}
			if apiErr.RequestID != "req-1" {
				t.Errorf("RequestID = %q", apiErr.RequestID)
			}
			if apiErr.RetryAfter < tc.wantMin || apiErr.RetryAfter > tc.wantMax {
				t.Errorf("RetryAfter = %v, want between %v and %v", apiErr.RetryAfter, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func retryServer(t *testing.T, statuses ...int) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := statuses[min(calls, len(statuses)-1)]
		calls++
		w.WriteHeader(status)
		if status == 200 {
			w.Write([]byte(`{"plans":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestRetries_offByDefault(t *testing.T) {
	srv, calls := retryServer(t, 503)
	_, err := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL)).Plans(context.Background())
	if err == nil || *calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, *calls)
	}
}

func TestRetries_aReadIsRetriedUntilItSucceeds(t *testing.T) {
	srv, calls := retryServer(t, 503, 429, 500, 200)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithMaxRetries(3), nozle.WithRetryDelay(time.Millisecond))
	if _, err := c.Plans(context.Background()); err != nil || *calls != 4 {
		t.Fatalf("err = %v, calls = %d", err, *calls)
	}
}

func TestRetries_stopAtMaxRetriesAndSkipA4xx(t *testing.T) {
	srv, calls := retryServer(t, 500)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithMaxRetries(2), nozle.WithRetryDelay(time.Millisecond))
	if _, err := c.Plans(context.Background()); err == nil || *calls != 3 {
		t.Fatalf("err = %v, calls = %d", err, *calls)
	}
	srv, calls = retryServer(t, 422)
	c = nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithMaxRetries(2), nozle.WithRetryDelay(time.Millisecond))
	if _, err := c.Plans(context.Background()); err == nil || *calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, *calls)
	}
}

func TestRetries_aWriteIsRetriedOnlyWhenItIsSafeToRepeat(t *testing.T) {
	srv, calls := retryServer(t, 503)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithMaxRetries(2), nozle.WithRetryDelay(time.Millisecond))
	_, _ = c.Subscribe(context.Background(), nozle.SubscribeParams{CustomerID: "cust", PlanCode: "pro"})
	if *calls != 1 {
		t.Fatalf("subscribe calls = %d, want 1", *calls)
	}
	*calls = 0
	_, _ = c.TrackUsage(context.Background(), nozle.UsageParams{CustomerID: "c", FeatureCode: "f", Timestamp: "2026-10-15T10:00:00Z"}, "k1")
	if *calls != 3 {
		t.Fatalf("usage.track calls = %d, want 3", *calls)
	}
}

func TestRetries_waitForRetryAfter(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"plans":[]}`))
	}))
	defer srv.Close()
	started := time.Now()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithMaxRetries(1), nozle.WithRetryDelay(time.Millisecond))
	if _, err := c.Plans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(started); waited < 900*time.Millisecond {
		t.Fatalf("waited %v, want about 1s", waited)
	}
}

func TestTrackUsage_reusesItsTimestampForARetriedKey(t *testing.T) {
	var stamps []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		stamps = append(stamps, body["timestamp"].(string))
		w.Write([]byte(`{"allowed":true}`))
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	p := nozle.UsageParams{CustomerID: "c", FeatureCode: "f"}
	for _, key := range []string{"k1", "k1", "k2"} {
		if _, err := c.TrackUsage(context.Background(), p, key); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	p.Timestamp = "2026-01-01T00:00:00Z"
	if _, err := c.TrackUsage(context.Background(), p, "k3"); err != nil {
		t.Fatal(err)
	}
	if stamps[0] != stamps[1] || stamps[2] == stamps[0] || stamps[3] != "2026-01-01T00:00:00Z" {
		t.Fatalf("timestamps = %v", stamps)
	}
}

func TestEntitySubscriptionRequests_leaveOutUnsetOptionalFields(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Write([]byte(`{"type":"completed","entity_subscription_checkout":{"id":"batch-1"}}`))
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithEventsURL(srv.URL))
	ctx := context.Background()

	_, _ = c.CheckoutEntitySubscription(ctx, "cust", "user-42", nozle.EntitySubscriptionChange{PlanCode: "pro"})
	_, _ = c.ChangeEntitySubscriptionPlan(ctx, "cust", "user-42", nozle.EntitySubscriptionChange{PlanCode: "team", BillingTime: "calendar"})
	_, _ = c.CheckoutEntitySubscriptions(ctx, "cust", []nozle.EntitySubscriptionCheckoutItem{{ExternalEntityID: "user-42", PlanCode: "pro"}}, "", "", "batch-1")

	if len(bodies) != 3 {
		t.Fatalf("requests = %d", len(bodies))
	}
	if _, set := bodies[0]["billing_time"]; set || len(bodies[0]) != 1 {
		t.Errorf("checkout body = %v, want only plan_code", bodies[0])
	}
	if bodies[1]["billing_time"] != "calendar" {
		t.Errorf("change-plan body = %v, want billing_time calendar", bodies[1])
	}
	batch := bodies[2]["entity_subscription_checkout"].(map[string]any)
	if _, set := batch["billing_time"]; set {
		t.Errorf("batch body = %v, want no billing_time", batch)
	}
	if _, set := batch["return_url"]; set {
		t.Errorf("batch body = %v, want no return_url", batch)
	}
}

func TestClient_PlansCurrencyAndFamily(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		w.Write([]byte(`{"plans":[{"code":"pro_eur","name":"Pro","amount_cents":4500,"amount_currency":"EUR","interval":"monthly",
			"family":{"code":"pro","currencies":["USD","EUR"],"default_currency":"USD"},
			"prices":[{"currency":"USD","interval":"monthly","plan_code":"pro","amount_cents":4900},
			          {"currency":"EUR","interval":"monthly","plan_code":"pro_eur","amount_cents":4500}]}]}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	plans, err := c.Plans(context.Background(), nozle.WithCurrency("EUR"), nozle.WithExternalCustomerID("cus 1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Plans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"currency=EUR&external_customer_id=cus+1", ""}; !reflect.DeepEqual(queries, want) {
		t.Errorf("queries = %q, want %q", queries, want)
	}
	want := nozle.Plan{
		Code: "pro_eur", Name: "Pro", AmountCents: 4500, AmountCurrency: "EUR", Interval: "monthly",
		Family: &nozle.PlanFamilyRef{Code: "pro", Currencies: []string{"USD", "EUR"}, DefaultCurrency: "USD"},
		Prices: []nozle.PlanPrice{
			{Currency: "USD", Interval: "monthly", PlanCode: "pro", AmountCents: 4900},
			{Currency: "EUR", Interval: "monthly", PlanCode: "pro_eur", AmountCents: 4500},
		},
	}
	if !reflect.DeepEqual(plans, []nozle.Plan{want}) {
		t.Errorf("plans = %+v", plans)
	}
}

func TestClient_CheckoutAndSubscribeByPlanFamily(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Write([]byte(`{"type":"completed","status":"succeeded"}`))
	}))
	defer srv.Close()

	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	ctx := context.Background()
	if _, err := c.Checkout(ctx, nozle.CheckoutParams{CustomerID: "cust", PlanFamilyCode: "pro", Interval: "yearly", Currency: "EUR"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Subscribe(ctx, nozle.SubscribeParams{CustomerID: "cust", PlanFamilyCode: "pro", Interval: "monthly"}); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"customer_id": "cust", "plan_family_code": "pro", "interval": "yearly", "currency": "EUR"},
		{"customer_id": "cust", "plan_family_code": "pro", "interval": "monthly"},
	}
	if !reflect.DeepEqual(bodies, want) {
		t.Errorf("bodies = %v, want %v", bodies, want)
	}

	invalid := []nozle.CheckoutParams{
		{CustomerID: "cust", PlanCode: "pro", PlanFamilyCode: "pro", Interval: "monthly"},
		{CustomerID: "cust", PlanFamilyCode: "pro"},
		{CustomerID: "cust", PlanCode: "pro", Currency: "EUR"},
	}
	for _, p := range invalid {
		var ve *nozle.ValidationError
		if _, err := c.Checkout(ctx, p); !errors.As(err, &ve) {
			t.Errorf("Checkout(%+v) err = %v, want ValidationError", p, err)
		}
		if _, err := c.Subscribe(ctx, nozle.SubscribeParams{CustomerID: p.CustomerID, PlanCode: p.PlanCode, PlanFamilyCode: p.PlanFamilyCode, Interval: p.Interval, Currency: p.Currency}); !errors.As(err, &ve) {
			t.Errorf("Subscribe(%+v) err = %v, want ValidationError", p, err)
		}
	}
	if len(bodies) != 2 {
		t.Errorf("invalid params reached the server: %d requests", len(bodies))
	}
}
