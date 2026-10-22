package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

// These responses exercise SDK decoding and request scoping, not provider settlement.
func TestCheckoutLifecycleContracts(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer sk_contract" {
			t.Error("missing authentication")
		}
		if calls == 1 {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if r.Method != "POST" || r.URL.Path != "/api/v1/checkout" || r.Header.Get("Idempotency-Key") != "invoice-retry" || !reflect.DeepEqual(body, map[string]any{"invoice_id": "invoice-1"}) {
				t.Errorf("wrong invoice request: %s %s %v", r.Method, r.URL, body)
			}
			w.Write([]byte(`{"type":"stripe","checkout_id":"checkout/1","client_secret":"simulated","amount_cents":100,"currency":"USD"}`))
			return
		}
		if r.URL.Query().Get("customer_id") != "customer-1" || r.URL.Query().Get("subscription_id") != "subscription-1" {
			t.Errorf("missing checkout scope: %s", r.URL)
		}
		if calls == 2 {
			if r.Method != "GET" || r.URL.EscapedPath() != "/api/v1/checkout/checkout%2F1" {
				t.Errorf("wrong status request: %s %s", r.Method, r.URL)
			}
			w.Write([]byte(`{"checkout_id":"checkout/1","provider":"stripe","status":"processing","fulfillment_status":"pending"}`))
			return
		}
		var verification nozle.RazorpayVerification
		if err := json.NewDecoder(r.Body).Decode(&verification); err != nil {
			t.Fatal(err)
		}
		if r.Method != "POST" || r.URL.EscapedPath() != "/api/v1/checkout/checkout%2F1/verify" || verification.RazorpaySignature != "simulated-signature" || verification.RazorpayPaymentID != "payment-1" || verification.RazorpayOrderID != "order-1" {
			t.Errorf("wrong verification: %s %s %+v", r.Method, r.URL, verification)
		}
		w.Write([]byte(`{"checkout_id":"checkout/1","provider":"razorpay","status":"succeeded","fulfillment_status":"fulfilled","amount_cents":100,"currency":"USD","invoice_id":"invoice-1"}`))
	}))
	defer server.Close()
	client := nozle.New("sk_contract", nozle.WithBaseURL(server.URL))
	ctx := context.Background()
	checkout, err := client.CheckoutInvoice(ctx, "invoice-1", "invoice-retry")
	if err != nil || checkout.CheckoutID != "checkout/1" || checkout.AmountCents != 100 {
		t.Fatalf("checkout: %+v %v", checkout, err)
	}
	scope := nozle.CheckoutScope{CustomerID: "customer-1", SubscriptionID: "subscription-1"}
	status, err := client.CheckoutStatus(ctx, checkout.CheckoutID, scope)
	if err != nil || status.Status != "processing" || status.FulfillmentStatus != "pending" {
		t.Fatalf("status: %+v %v", status, err)
	}
	status, err = client.VerifyCheckout(ctx, checkout.CheckoutID, nozle.RazorpayVerification{RazorpayOrderID: "order-1", RazorpayPaymentID: "payment-1", RazorpaySignature: "simulated-signature"}, scope)
	if err != nil || status.Status != "succeeded" || status.FulfillmentStatus != "fulfilled" || status.InvoiceID != "invoice-1" {
		t.Fatalf("verification: %+v %v", status, err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

type deadlineTransport struct{ called bool }

func (t *deadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.called = true
	if _, ok := r.Context().Deadline(); !ok {
		return nil, errors.New("SDK failed to set a deadline")
	}
	<-r.Context().Done()
	return nil, r.Context().Err()
}
func TestCustomTransportTimeoutAndPublicErrors(t *testing.T) {
	transport := &deadlineTransport{}
	client := nozle.New("sk_contract", nozle.WithTimeout(5*time.Millisecond), nozle.WithHTTPClient(&http.Client{Transport: transport}))
	_, err := client.Ping(context.Background())
	var failure *nozle.TransportError
	if !transport.called || !errors.As(err, &failure) || !errors.Is(failure, context.DeadlineExceeded) || !strings.Contains(failure.Error(), "transport failed") {
		t.Fatalf("timeout lost: %v", err)
	}
	var api *nozle.APIError
	wrapped := errors.Join(errors.New("call failed"), &nozle.APIError{StatusCode: 503, Method: "GET", Path: "/api/v1/plans"})
	if !nozle.AsAPIError(wrapped, &api) || !strings.Contains(api.Error(), "503") {
		t.Fatal("API error not discoverable")
	}
	_, err = client.CheckoutStatus(context.Background(), "checkout", nozle.CheckoutScope{CustomerID: "customer-only"})
	var validation *nozle.ValidationError
	if !errors.As(err, &validation) || !strings.Contains(validation.Error(), "both customer and subscription") {
		t.Fatalf("scope accepted: %v", err)
	}
}

func TestManagePaymentMethod(t *testing.T) {
	var body string
	var key string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body, key = string(raw), r.Header.Get("Idempotency-Key")
		if r.URL.Path == "/api/v1/customers/rzp/payment-method/manage" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"status":422,"error":"Unprocessable Entity","code":"validation_errors","error_details":{"base":["razorpay_pays_by_link"]}}`))
			return
		}
		if r.Method != "POST" || r.URL.EscapedPath() != "/api/v1/customers/cust%2F1/payment-method/manage" {
			t.Errorf("wrong request: %s %s", r.Method, r.URL)
		}
		w.Write([]byte(`{"manage_payment_method":{"url":"https://billing.stripe.com/x","expires_at":null,"provider":"stripe"}}`))
	}))
	defer server.Close()
	client := nozle.New("sk_contract", nozle.WithBaseURL(server.URL))
	ctx := context.Background()
	got, err := client.ManagePaymentMethod(ctx, "cust/1", "https://app.example/back", "k1")
	if err != nil || got.URL != "https://billing.stripe.com/x" || got.Provider != "stripe" || key != "k1" || body != `{"return_url":"https://app.example/back"}` {
		t.Fatalf("stripe: %+v %v key=%q body=%q", got, err, key, body)
	}
	_, err = client.ManagePaymentMethod(ctx, "rzp", "", "")
	var apiErr *nozle.APIError
	if !nozle.AsAPIError(err, &apiErr) || apiErr.StatusCode != 422 || !apiErr.HasCode("razorpay_pays_by_link") || body != "" {
		t.Fatalf("razorpay: %v body=%q", err, body)
	}
	if _, err := nozle.New("pk_browser", nozle.WithBaseURL(server.URL)).ManagePaymentMethod(ctx, "cust", "", ""); err == nil {
		t.Fatal("publishable key accepted")
	}
	if _, err := client.ManagePaymentMethod(ctx, "", "", ""); err == nil {
		t.Fatal("blank customer accepted")
	}
}
