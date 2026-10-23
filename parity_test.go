package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	nozle "github.com/nozle-dev/nozle-go"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCreditTransferPreservesDecimalAndIdempotency(t *testing.T) {
	var body map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/customers/c/entities/e/credit-allocations" || r.Header.Get("Idempotency-Key") != "transfer-1" {
			t.Errorf("wrong request: %s %s", r.URL.Path, r.Header.Get("Idempotency-Key"))
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"transferred":true,"amount":"100.000000000001"}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL))
	out, err := c.AllocateCredits(context.Background(), "c", "e", "ai", "100.000000000001", "transfer-1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Amount != "100.000000000001" || body["amount"] != "100.000000000001" {
		t.Fatalf("precision lost: %v %v", out.Amount, body["amount"])
	}
}
func TestUsageTrackRequiresIdempotencyKey(t *testing.T) {
	c := nozle.New("sk_test")
	_, err := c.TrackUsage(context.Background(), nozle.UsageParams{CustomerID: "c", FeatureCode: "f"}, "")
	if err == nil {
		t.Fatal("expected local validation error")
	}
}
func TestEntityUpsertUsesEscapedPath(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v1/customers/c/entities/user%2F1" {
			t.Errorf("path: %s", r.URL.EscapedPath())
		}
		w.Write([]byte(`{"action":"created","entity":{"external_id":"user/1"}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL))
	_, err := c.UpsertEntity(context.Background(), "c", "user/1", nozle.EntityInput{Status: "active"}, "key")
	if err != nil {
		t.Fatal(err)
	}
}
func TestAPIErrorRedactsNestedSecrets(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"error":{"secret_key":"hidden","message":"sk_test"}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL))
	_, err := c.Ping(context.Background())
	var api *nozle.APIError
	if !errors.As(err, &api) {
		t.Fatalf("expected API error, got %v", err)
	}
	if strings.Contains(api.Body, "hidden") || strings.Contains(api.Body, "sk_test") {
		t.Fatalf("secret leaked: %s", api.Body)
	}
}
func TestCanWithMetadataEncodesJSON(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("metadata") != `{"model":"large"}` {
			t.Errorf("metadata=%q", r.URL.Query().Get("metadata"))
		}
		w.Write([]byte(`{"allowed":true}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL))
	_, err := c.CanWithMetadata(context.Background(), "customer", "model_access", map[string]any{"model": "large"})
	if err != nil {
		t.Fatal(err)
	}
}
func TestSubscribeDecodesNestedSubscription(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"subscription":{"external_id":"sub-local-free","status":"active","plan_code":"free"}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL))
	got, err := c.Subscribe(context.Background(), nozle.SubscribeParams{CustomerID: "local", PlanCode: "free"})
	if err != nil {
		t.Fatal(err)
	}
	if got.SubscriptionID != "sub-local-free" || got.Status != "active" || got.Subscription.PlanCode != "free" {
		t.Fatalf("nested subscription lost: %+v", got)
	}
}
func TestCaptureOpenAIUsesMillisecondTimestamp(t *testing.T) {
	var timestamp string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Event struct {
				Timestamp string `json:"timestamp"`
			} `json:"event"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		timestamp = body.Event.Timestamp
		w.Write([]byte(`{"event":{}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL), nozle.WithEventsURL(s.URL))
	at := time.Date(2026, 10, 11, 12, 0, 0, 123456789, time.UTC)
	err := c.CaptureOpenAIUsage(context.Background(), nozle.ProviderCapture{CustomerID: "c", SubscriptionID: "s", FeatureCode: "llm", Model: "test", Timestamp: at}, nozle.OpenAIUsage{PromptTokens: 1, CompletionTokens: 1})
	if err != nil {
		t.Fatal(err)
	}
	if timestamp != "1791720000.123" {
		t.Fatalf("timestamp=%q", timestamp)
	}
}

func TestCostEventAcceptedResponse(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted","cost_event_id":"cost-1"}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL))
	out, err := c.TrackCostEvent(context.Background(), nozle.CostEventParams{CostMeterCode: "ai", ExternalCustomerID: "customer", CostEventID: "cost-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Accepted || out.Status != "accepted" || out.CostEventID != "cost-1" {
		t.Fatalf("accepted response decoded incorrectly: %+v", out)
	}
}
func TestCancelSubscriptionDecodesNestedResponse(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"subscription":{"external_id":"sub-local","status":"active","ending_at":"2026-10-31T18:30:00Z"}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL))
	out, err := c.CancelSubscription(context.Background(), "local", "sub-local", "end_of_period")
	if err != nil {
		t.Fatal(err)
	}
	if out.SubscriptionID != "sub-local" || out.Status != "active" || out.Subscription.EndingAt == "" {
		t.Fatalf("nested cancellation lost: %+v", out)
	}
}

func TestCancelSubscriptionSendsOnlyTheOptionsGiven(t *testing.T) {
	var got url.Values
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"subscription":{"external_id":"sub-1","status":"terminated"}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL))
	ctx := context.Background()

	// No timing: the organization's cancellation settings decide.
	if _, err := c.CancelSubscription(ctx, "cust-1", "sub-1", ""); err != nil {
		t.Fatal(err)
	}
	if want := (url.Values{"customer_id": {"cust-1"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("query = %v, want %v", got, want)
	}

	options := nozle.CancelSubscriptionOptions{CreditAction: "refund", RefundMode: "full", FinalInvoiceAction: "skip"}
	if _, err := c.CancelSubscription(ctx, "cust-1", "sub-1", "immediate", options); err != nil {
		t.Fatal(err)
	}
	want := url.Values{
		"customer_id": {"cust-1"}, "cancellation_policy": {"immediate"},
		"credit_action": {"refund"}, "refund_mode": {"full"}, "final_invoice_action": {"skip"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query = %v, want %v", got, want)
	}

	if _, err := c.CancelSubscription(ctx, "cust-1", "sub-1", "", options, options); err == nil {
		t.Fatal("two option sets were accepted")
	}
}
