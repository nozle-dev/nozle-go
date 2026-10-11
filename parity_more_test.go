package nozle_test

import (
	"context"
	"encoding/json"
	nozle "github.com/nozle-dev/nozle-go"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckAndDeductLegacyWallet(t *testing.T) {
	var body map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/check-and-deduct" || r.Method != "POST" {
			t.Errorf("route %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"allowed":true,"remaining":9}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL))
	out, err := c.CheckAndDeduct(context.Background(), nozle.CheckAndDeductParams{CustomerID: "c", Feature: "f", Credits: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Allowed || out.Remaining != 9 || body["customer_id"] != "c" {
		t.Fatalf("wrong result/body: %+v %v", out, body)
	}
}
func TestCreateIDs(t *testing.T) {
	if len(nozle.CreateTransactionID()) != 36 || len(nozle.CreateCostEventID()) != 36 {
		t.Fatal("expected UUID identifiers")
	}
}
func TestCheckoutEntitySubscriptionsTypedResult(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "pool-1" {
			t.Errorf("missing key")
		}
		w.Write([]byte(`{"entity_subscription_checkout":{"id":"pool-1","type":"completed","status":"succeeded","items":[{"external_entity_id":"e","plan_code":"free"}]}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL), nozle.WithEventsURL(s.URL))
	out, err := c.CheckoutEntitySubscriptions(context.Background(), "c", []nozle.EntitySubscriptionCheckoutItem{{ExternalEntityID: "e", PlanCode: "free"}}, "", "calendar", "pool-1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != "completed" || len(out.Items) != 1 {
		t.Fatalf("wrong typed result: %+v", out)
	}
}
func TestCancelEntitySubscriptionTypedResult(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"entity_subscription":{"external_entity_id":"e","status":"active"},"subscription_transition":{"operation":"cancel","invoice_id":"inv_1"}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL), nozle.WithEventsURL(s.URL))
	out, err := c.CancelEntitySubscription(context.Background(), "c", "e", nozle.EntitySubscriptionCancel{IdempotencyKey: "cancel-1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.EntitySubscription.ExternalEntityID != "e" || out.SubscriptionTransition.Operation != "cancel" || out.SubscriptionTransition.InvoiceID != "inv_1" {
		t.Fatalf("wrong typed result: %+v", out)
	}
}

func TestGeneratedIDsUseUUIDv7(t *testing.T) {
	for _, id := range []string{nozle.CreateTransactionID(), nozle.CreateCostEventID()} {
		if len(id) != 36 || id[14] != '7' {
			t.Fatalf("expected UUIDv7, got %q", id)
		}
	}
}

func TestEntitySubscriptionPlansAreTyped(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"entity_subscription":{"external_entity_id":"e","current_plan":{"code":"free","name":"Free","interval":"month","amount_cents":0,"amount_currency":"USD","status":"active"}}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(s.URL), nozle.WithEventsURL(s.URL))
	out, err := c.GetEntitySubscription(context.Background(), "c", "e")
	if err != nil {
		t.Fatal(err)
	}
	if out.CurrentPlan == nil || out.CurrentPlan.Code != "free" {
		t.Fatalf("wrong plan: %+v", out.CurrentPlan)
	}
}
