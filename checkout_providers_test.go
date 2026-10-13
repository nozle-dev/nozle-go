package nozle_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

func checkoutClient(t *testing.T, status int, body string) *nozle.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return nozle.New("sk_contract", nozle.WithBaseURL(server.URL))
}

func TestCheckoutDecodesRazorpayStripeAndTransitionFields(t *testing.T) {
	ctx := context.Background()
	params := nozle.CheckoutParams{CustomerID: "customer-1", PlanCode: "pro"}

	razorpay, err := checkoutClient(t, 200, `{"type":"razorpay","checkout_id":"c1","key_id":"rzp_test_k","order_id":"order_1","amount_cents":99900,"currency":"INR","expires_at":"2026-10-14T09:30:00.000Z","customer_id":"cust_rzp","recurring":true,"mandate_max_amount_cents":500000,"plan_upgrade_intent_id":"intent-1","external_subscription_id":"sub-1","external_entity_id":"entity-1"}`).Checkout(ctx, params)
	if err != nil || razorpay.Type != "razorpay" || razorpay.KeyID != "rzp_test_k" || razorpay.OrderID != "order_1" || razorpay.AmountCents != 99900 || razorpay.Currency != "INR" ||
		razorpay.ExpiresAt != "2026-10-14T09:30:00.000Z" || razorpay.CustomerID != "cust_rzp" || !razorpay.Recurring || razorpay.MandateMaxAmountCents != 500000 ||
		razorpay.PlanUpgradeIntentID != "intent-1" || razorpay.ExternalSubscriptionID != "sub-1" || razorpay.ExternalEntityID != "entity-1" {
		t.Fatalf("razorpay: %+v %v", razorpay, err)
	}

	hosted, err := checkoutClient(t, 200, `{"type":"hosted","payment_url":"https://rzp.io/i/abc","checkout_id":"c2","amount_cents":500,"currency":"INR","expires_at":"2026-10-14T09:30:00.000Z","invoice_id":"inv-1"}`).Checkout(ctx, params)
	if err != nil || hosted.PaymentURL != "https://rzp.io/i/abc" || hosted.CheckoutID != "c2" || hosted.ExpiresAt == "" || hosted.InvoiceID != "inv-1" || hosted.Recurring {
		t.Fatalf("hosted: %+v %v", hosted, err)
	}

	stripe, err := checkoutClient(t, 200, `{"type":"stripe","client_secret":"cs_test_1","clientSecret":"cs_test_1","plan_upgrade_intent_id":"intent-2","amount_cents":1000,"currency":"USD"}`).Checkout(ctx, params)
	if err != nil || stripe.ClientSecret != "cs_test_1" || stripe.PlanUpgradeIntentID != "intent-2" {
		t.Fatalf("stripe: %+v %v", stripe, err)
	}

	completed, err := checkoutClient(t, 200, `{"type":"completed","status":"succeeded","payment_source":"credits","subscription_id":"s1","plan_code":"pro","invoice_id":"inv-2","amount_cents":0,"currency":"USD"}`).Checkout(ctx, params)
	if err != nil || completed.PaymentSource != "credits" || completed.PlanCode != "pro" || completed.InvoiceID != "inv-2" {
		t.Fatalf("completed: %+v %v", completed, err)
	}

	scheduled, err := checkoutClient(t, 200, `{"type":"scheduled","status":"pending","subscription_id":"s2","plan_code":"starter","transition_direction":"downgrade","transition_policy":"next_period","effective_at":"2026-11-01T00:00:00.000000Z"}`).Checkout(ctx, params)
	if err != nil || scheduled.TransitionDirection != "downgrade" || scheduled.TransitionPolicy != "next_period" || scheduled.PlanCode != "starter" || scheduled.SubscriptionID != "s2" || scheduled.EffectiveAt == "" {
		t.Fatalf("scheduled: %+v %v", scheduled, err)
	}

	status, err := checkoutClient(t, 200, `{"checkout_id":"c1","provider":"razorpay","status":"awaiting_payment","fulfillment_status":"pending","amount_cents":99900,"currency":"INR","invoice_id":"inv-1","checkout":{"type":"razorpay","checkout_id":"c1","key_id":"k","order_id":"o","amount_cents":99900,"currency":"INR","expires_at":"2026-10-14T09:30:00.000Z"}}`).CheckoutStatus(ctx, "c1", nozle.CheckoutScope{})
	if err != nil || status.Checkout == nil || status.Checkout.OrderID != "o" || status.Checkout.ExpiresAt == "" || status.Status != "awaiting_payment" {
		t.Fatalf("status: %+v %v", status, err)
	}
}

func TestCheckoutErrorCodes(t *testing.T) {
	client := checkoutClient(t, 422, `{"status":422,"code":"validation_errors","error_details":{"base":["razorpay_requires_inr"]}}`)
	_, err := client.Checkout(context.Background(), nozle.CheckoutParams{CustomerID: "customer-1", PlanCode: "pro"})
	var api *nozle.APIError
	if !nozle.AsAPIError(err, &api) || api.StatusCode != 422 {
		t.Fatalf("not an API error: %v", err)
	}
	if api.Code() != "validation_errors" || !api.HasCode("razorpay_requires_inr") || !api.HasCode("validation_errors") || api.HasCode("payment_provider_required") || api.HasCode("") {
		t.Fatalf("codes: %q %+v", api.Code(), api.Details)
	}

	for _, details := range []any{nil, "plain", []any{"x"}, map[string]any{"code": 5}, map[string]any{"error_details": []any{"x"}}} {
		e := &nozle.APIError{StatusCode: 500, Details: details}
		if e.Code() != "" || e.HasCode("anything") {
			t.Fatalf("details %v: %q", details, e.Code())
		}
	}
}

func TestVerifyCheckoutRequiresEverySignatureField(t *testing.T) {
	client := nozle.New("sk_contract", nozle.WithBaseURL("http://127.0.0.1:1"))
	full := nozle.RazorpayVerification{RazorpayOrderID: "o", RazorpayPaymentID: "p", RazorpaySignature: "s"}
	for field, mutate := range map[string]func(*nozle.RazorpayVerification){
		"razorpay_order_id":   func(v *nozle.RazorpayVerification) { v.RazorpayOrderID = "" },
		"razorpay_payment_id": func(v *nozle.RazorpayVerification) { v.RazorpayPaymentID = "" },
		"razorpay_signature":  func(v *nozle.RazorpayVerification) { v.RazorpaySignature = "" },
	} {
		v := full
		mutate(&v)
		_, err := client.VerifyCheckout(context.Background(), "checkout", v, nozle.CheckoutScope{})
		var validation *nozle.ValidationError
		if !errors.As(err, &validation) || !strings.Contains(validation.Error(), field) {
			t.Fatalf("%s accepted: %v", field, err)
		}
	}
}
