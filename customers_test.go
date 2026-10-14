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

func TestUpsertCustomerSendsBillingConfiguration(t *testing.T) {
	var body map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/customers" {
			t.Errorf("wrong request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"customer":{"external_id":"acme","billing_configuration":{"payment_provider":"stripe","payment_provider_code":"main","provider_customer_id":"cus_123"}}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithEventsURL(s.URL))

	out, err := c.UpsertCustomer(context.Background(), nozle.Customer{
		ExternalID: "acme",
		BillingConfiguration: &nozle.CustomerBillingConfiguration{
			PaymentProvider:     "stripe",
			PaymentProviderCode: "main",
			ProviderCustomerID:  "cus_123",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"customer": map[string]any{
		"external_id": "acme",
		"billing_configuration": map[string]any{
			"payment_provider":      "stripe",
			"payment_provider_code": "main",
			"provider_customer_id":  "cus_123",
		},
	}}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %v, want %v", body, want)
	}
	if out.BillingConfiguration == nil || out.BillingConfiguration.ProviderCustomerID != "cus_123" {
		t.Fatalf("billing configuration not decoded: %+v", out.BillingConfiguration)
	}
}

func TestUpsertCustomerOmitsBillingConfigurationByDefault(t *testing.T) {
	var body map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"customer":{"external_id":"acme","name":"Acme"}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithEventsURL(s.URL))

	if _, err := c.UpsertCustomer(context.Background(), nozle.Customer{ExternalID: "acme", Name: "Acme"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"customer": map[string]any{"external_id": "acme", "name": "Acme"}}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %v, want %v", body, want)
	}
}

func TestUpsertCustomerRoundTripsCustomerWithoutProvider(t *testing.T) {
	var bodies []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Write([]byte(`{"customer":{"external_id":"acme","name":"Acme","billing_configuration":{"payment_provider":null,"payment_provider_code":null}}}`))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithEventsURL(s.URL))

	got, err := c.UpsertCustomer(context.Background(), nozle.Customer{ExternalID: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if got.BillingConfiguration != nil {
		t.Fatalf("empty billing configuration should decode as nil: %+v", got.BillingConfiguration)
	}
	if _, err := c.UpsertCustomer(context.Background(), got); err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if _, ok := bodies[1]["customer"].(map[string]any)["billing_configuration"]; ok {
		t.Fatalf("round trip sent billing_configuration: %v", bodies[1])
	}
}

func TestUpsertCustomerRequiresPaymentProvider(t *testing.T) {
	c := nozle.New("sk_test", nozle.WithEventsURL("http://127.0.0.1:1"))
	_, err := c.UpsertCustomer(context.Background(), nozle.Customer{
		ExternalID:           "acme",
		BillingConfiguration: &nozle.CustomerBillingConfiguration{ProviderCustomerID: "cus_123"},
	})
	if _, ok := err.(*nozle.ValidationError); !ok {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}
