package nozle_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

type customerCall struct{ method, path, body string }

// customerServer answers every request with status and body and records what it got.
func customerServer(t *testing.T, status int, body string) (*nozle.Client, *[]customerCall) {
	t.Helper()
	var got []customerCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = append(got, customerCall{r.Method, r.URL.EscapedPath(), string(raw)})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return nozle.New("sk_test-key", nozle.WithEventsURL(srv.URL), nozle.WithMaxRetries(2), nozle.WithRetryDelay(time.Millisecond)), &got
}

func TestEraseCustomer_postsWithoutABodyAndDecodesErasedAt(t *testing.T) {
	c, got := customerServer(t, http.StatusAccepted, `{"customer":{"external_id":"c/1","erased_at":"2026-11-18T10:00:00Z"}}`)
	customer, err := c.EraseCustomer(context.Background(), "c/1")
	if err != nil {
		t.Fatal(err)
	}
	if customer.ExternalID != "c/1" || customer.ErasedAt != "2026-11-18T10:00:00Z" {
		t.Fatalf("customer = %+v", customer)
	}
	if r := (*got)[0]; r.method != "POST" || r.path != "/api/v1/customers/c%2F1/erase" || r.body != "" {
		t.Fatalf("request = %+v", r)
	}
}

func TestEraseCustomer_refusalCodes(t *testing.T) {
	c, _ := customerServer(t, http.StatusUnprocessableEntity,
		`{"code":"validation_errors","error_details":{"customer":["has_active_subscriptions","has_unpaid_invoices"]}}`)
	_, err := c.EraseCustomer(context.Background(), "c1")
	var api *nozle.APIError
	if !errors.As(err, &api) || api.StatusCode != 422 {
		t.Fatalf("err = %v", err)
	}
	if !api.HasCode(nozle.CodeHasActiveSubscriptions) || !api.HasCode(nozle.CodeHasUnpaidInvoices) || api.HasCode(nozle.CodeHasDraftInvoices) {
		t.Fatalf("codes = %v", api.Details)
	}
}

func TestDeleteCustomer_sendsDelete(t *testing.T) {
	c, got := customerServer(t, http.StatusOK, `{"customer":{"external_id":"c1","erased_at":null}}`)
	customer, err := c.DeleteCustomer(context.Background(), "c1")
	if err != nil || customer.ExternalID != "c1" || customer.ErasedAt != "" {
		t.Fatalf("customer = %+v, err = %v", customer, err)
	}
	if r := (*got)[0]; r.method != "DELETE" || r.path != "/api/v1/customers/c1" {
		t.Fatalf("request = %+v", r)
	}
}

func TestEraseAndDeleteCustomer_areNotRetried(t *testing.T) {
	for name, call := range map[string]func(*nozle.Client) error{
		"erase":  func(c *nozle.Client) error { _, err := c.EraseCustomer(context.Background(), "c1"); return err },
		"delete": func(c *nozle.Client) error { _, err := c.DeleteCustomer(context.Background(), "c1"); return err },
	} {
		c, got := customerServer(t, http.StatusServiceUnavailable, `{"error":"unavailable"}`)
		if err := call(c); err == nil || len(*got) != 1 {
			t.Fatalf("%s: err = %v, requests = %d, want one", name, err, len(*got))
		}
	}
}

func TestEraseAndDeleteCustomer_needASecretKeyAndAnID(t *testing.T) {
	public := nozle.New("pk_test")
	if _, err := public.EraseCustomer(context.Background(), "c1"); err == nil {
		t.Fatal("erase with a publishable key must fail")
	}
	if _, err := public.DeleteCustomer(context.Background(), "c1"); err == nil {
		t.Fatal("delete with a publishable key must fail")
	}
	c, got := customerServer(t, http.StatusOK, `{}`)
	if _, err := c.EraseCustomer(context.Background(), ""); err == nil {
		t.Fatal("erase without an id must fail")
	}
	if _, err := c.DeleteCustomer(context.Background(), ""); err == nil {
		t.Fatal("delete without an id must fail")
	}
	if len(*got) != 0 {
		t.Fatalf("requests = %d, want none", len(*got))
	}
}

func TestTrackUsageAndFinalizeCreditHold_returnPeriodInvoicedWithoutARetry(t *testing.T) {
	refusal := `409{"code":"period_invoiced","error_details":{"timestamp":["period_invoiced"]}}`
	srv, got := holdServer(t, refusal, refusal)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithMaxRetries(2), nozle.WithRetryDelay(time.Millisecond))
	_, trackErr := c.TrackUsage(context.Background(), nozle.UsageParams{CustomerID: "cust", FeatureCode: "emails"}, "k1")
	_, finalizeErr := c.FinalizeCreditHold(context.Background(), "hold_1", nozle.FinalizeCreditHoldParams{})
	for _, err := range []error{trackErr, finalizeErr} {
		var api *nozle.APIError
		if !errors.As(err, &api) || api.StatusCode != 409 || !api.HasCode(nozle.CodePeriodInvoiced) {
			t.Fatalf("err = %v", err)
		}
	}
	if len(*got) != 2 {
		t.Fatalf("requests = %d, want one each", len(*got))
	}
}
