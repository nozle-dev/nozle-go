package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

func taxClient(t *testing.T, h http.HandlerFunc) *nozle.Client {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return nozle.New("sk_test", nozle.WithEventsURL(s.URL), nozle.WithMaxRetries(0))
}

func TestUpsertCustomerSendsTaxDataAsCoreKeys(t *testing.T) {
	var body map[string]any
	c := taxClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"customer":{"external_id":"c1","address_line1":"Main St 1","country":"DE","tax_identification_number":"DE811128135","tax_identification_type":"eu_vat","tax_identification_status":"pending","tax_treatment":"pending","tax_identification_warnings":["country_mismatch"]}}`))
	})
	out, err := c.UpsertCustomer(context.Background(), nozle.Customer{
		ExternalID: "c1", CustomerType: "company", LegalName: "Acme GmbH",
		Address:         &nozle.CustomerAddress{Line1: "Main St 1", Line2: "Top", City: "Berlin", State: "BE", PostalCode: "10115", Country: "DE"},
		ShippingAddress: &nozle.CustomerAddress{Line1: "Dock 2", Country: "DE"},
		TaxID:           &nozle.CustomerTaxID{Type: nozle.TaxIDTypeEuVat, Value: "DE811128135"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"customer": map[string]any{
		"external_id": "c1", "customer_type": "company", "legal_name": "Acme GmbH",
		"address_line1": "Main St 1", "address_line2": "Top", "city": "Berlin", "state": "BE", "zipcode": "10115", "country": "DE",
		"shipping_address":          map[string]any{"address_line1": "Dock 2", "country": "DE"},
		"tax_identification_number": "DE811128135", "tax_identification_type": "eu_vat",
	}}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %v, want %v", body, want)
	}
	if out.TaxIdentificationStatus != nozle.TaxIDStatusPending || out.TaxTreatment != nozle.TaxTreatmentPending ||
		!reflect.DeepEqual(out.TaxIdentificationWarnings, []string{"country_mismatch"}) ||
		out.TaxID == nil || out.TaxID.Type != nozle.TaxIDTypeEuVat || out.Address == nil || out.Address.Line1 != "Main St 1" {
		t.Fatalf("tax fields not decoded: %+v", out)
	}
}

func TestDecodedCustomerCanBeSentBackWithoutReadOnlyFields(t *testing.T) {
	var customer nozle.Customer
	if err := json.Unmarshal([]byte(`{"external_id":"c1","tax_identification_status":"valid","tax_treatment":"reverse_charge"}`), &customer); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(customer)
	if string(raw) != `{"external_id":"c1"}` {
		t.Fatalf("got %s", raw)
	}
}

func TestClearTaxIDSendsNull(t *testing.T) {
	var raw map[string]any
	c := taxClient(t, func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Customer map[string]any }
		json.NewDecoder(r.Body).Decode(&b)
		raw = b.Customer
		w.Write([]byte(`{"customer":{"external_id":"c1"}}`))
	})
	if _, err := c.UpsertCustomer(context.Background(), nozle.Customer{ExternalID: "c1", ClearTaxID: true}); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["tax_identification_number"]; !ok || v != nil {
		t.Fatalf("body = %v", raw)
	}
	if _, ok := raw["tax_identification_type"]; ok {
		t.Fatal("type must not be sent when clearing")
	}
}

func TestTaxIDWithoutValueIsRefusedLocally(t *testing.T) {
	var calls int32
	c := taxClient(t, func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) })
	_, err := c.UpsertCustomer(context.Background(), nozle.Customer{ExternalID: "c1", TaxID: &nozle.CustomerTaxID{Type: nozle.TaxIDTypeEuVat}})
	var ve *nozle.ValidationError
	if !errors.As(err, &ve) || calls != 0 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestTaxErrorCodes(t *testing.T) {
	c := taxClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/customers" {
			w.WriteHeader(422)
			w.Write([]byte(`{"code":"validation_errors","error_details":{"tax_identification_number":["invalid_format"]}}`))
			return
		}
		w.WriteHeader(409)
		w.Write([]byte(`{"code":"customer_tax_check_pending","retry_after_seconds":30}`))
	})
	_, err := c.UpsertCustomer(context.Background(), nozle.Customer{ExternalID: "c1", TaxID: &nozle.CustomerTaxID{Type: nozle.TaxIDTypeEuVat, Value: "x"}})
	var api *nozle.APIError
	if !errors.As(err, &api) || !api.HasCode(nozle.CodeInvalidFormat) || api.HasCode(nozle.CodeValueIsInvalid) {
		t.Fatalf("err = %v", err)
	}
	_, err = c.GetCustomer(context.Background(), "c1") // any call that answers 409 with the body
	if !errors.As(err, &api) || !api.HasCode(nozle.CodeCustomerTaxCheckPending) || api.RetryAfterSeconds() != 30 {
		t.Fatalf("err = %v", err)
	}
}

func TestGetCustomerAndWaitForTaxCheck(t *testing.T) {
	var polls int32
	c := taxClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/v1/customers/a%2Fb" {
			t.Errorf("wrong request: %s %s", r.Method, r.URL.EscapedPath())
		}
		status := "pending"
		if atomic.AddInt32(&polls, 1) >= 2 {
			status = "valid"
		}
		w.Write([]byte(`{"customer":{"external_id":"a/b","tax_identification_status":"` + status + `"}}`))
	})
	out, err := c.WaitForTaxCheck(context.Background(), "a/b", nozle.WaitForTaxCheckOptions{Interval: time.Millisecond})
	if err != nil || out.TaxIdentificationStatus != nozle.TaxIDStatusValid || polls != 2 {
		t.Fatalf("out = %+v, err = %v, polls = %d", out, err, polls)
	}
}

func TestWaitForTaxCheckTimesOut(t *testing.T) {
	c := taxClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"customer":{"external_id":"c1","tax_identification_status":"pending"}}`))
	})
	_, err := c.WaitForTaxCheck(context.Background(), "c1", nozle.WaitForTaxCheckOptions{Timeout: 20 * time.Millisecond, Interval: 5 * time.Millisecond})
	if !errors.Is(err, nozle.ErrTaxCheckPending) {
		t.Fatalf("err = %v", err)
	}
}

func TestInvoiceTaxFields(t *testing.T) {
	var inv nozle.Invoice
	if err := json.Unmarshal([]byte(`{"nozle_id":"i1","tax_status":"succeeded","tax_treatment":"reverse_charge","tax_details":{"treatment":"reverse_charge","tax_identification_type":"eu_vat","customer_country":"DE"}}`), &inv); err != nil {
		t.Fatal(err)
	}
	if inv.TaxStatus != "succeeded" || inv.TaxTreatment != nozle.TaxTreatmentReverseCharge || inv.TaxDetails == nil || *inv.TaxDetails.TaxIdentificationType != nozle.TaxIDTypeEuVat {
		t.Fatalf("%+v", inv)
	}
}
