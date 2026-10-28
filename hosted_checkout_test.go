package nozle_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

func TestCheckoutInvoiceWithOptions_sendsCancelURL(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"type":"hosted","url":"https://checkout.stripe.com/c/pay/cs_1","payment_url":"https://checkout.stripe.com/c/pay/cs_1"}`))
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	got, err := c.CheckoutInvoiceWithOptions(context.Background(), "inv-1", nozle.CheckoutInvoiceOptions{CancelURL: "https://merchant.example/pricing", IdempotencyKey: "k1"})
	if err != nil || got.Type != "hosted" || got.URL != "https://checkout.stripe.com/c/pay/cs_1" {
		t.Fatalf("result = %+v, err = %v", got, err)
	}
	if want := map[string]any{"invoice_id": "inv-1", "cancel_url": "https://merchant.example/pricing"}; !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %v, want %v", body, want)
	}
}
