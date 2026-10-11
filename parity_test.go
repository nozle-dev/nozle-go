package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	nozle "github.com/nozle-dev/nozle-go"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
