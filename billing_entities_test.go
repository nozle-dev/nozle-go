package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

const beJSON = `{"nozle_id":"b1","code":"acme_us","name":"Acme US","is_default":true,"tax_provider":"stripe_tax","provider_tax_code":"txcd_10103001","net_payment_term":30}`

func TestBillingEntitiesReadWrite(t *testing.T) {
	var method, path string
	var body map[string]any
	c := taxClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, body = r.Method, r.URL.EscapedPath(), nil
		json.NewDecoder(r.Body).Decode(&body)
		if r.Method == http.MethodGet && path == "/api/v1/billing_entities" {
			w.Write([]byte(`{"billing_entities":[` + beJSON + `]}`))
			return
		}
		w.Write([]byte(`{"billing_entity":` + beJSON + `}`))
	})
	ctx := context.Background()
	list, err := c.ListBillingEntities(ctx)
	if err != nil || len(list.BillingEntities) != 1 || list.BillingEntities[0].TaxProvider != nozle.TaxProviderStripeTax {
		t.Fatalf("list = %+v, %v", list, err)
	}
	got, err := c.GetBillingEntity(ctx, "a/b")
	if err != nil || method != "GET" || path != "/api/v1/billing_entities/a%2Fb" || got.ProviderTaxCode == nil || *got.ProviderTaxCode != "txcd_10103001" || got.NetPaymentTerm != 30 {
		t.Fatalf("get = %+v, %v (%s %s)", got, err, method, path)
	}
	if _, err = c.CreateBillingEntity(ctx, nozle.BillingEntityParams{Code: "acme_us", Name: "Acme US", TaxProvider: nozle.TaxProviderStripeTax, ProviderTaxCode: "txcd_10103001"}); err != nil {
		t.Fatal(err)
	}
	be, _ := body["billing_entity"].(map[string]any)
	if method != "POST" || be["tax_provider"] != "stripe_tax" || be["provider_tax_code"] != "txcd_10103001" || be["code"] != "acme_us" || be["city"] != nil {
		t.Fatalf("create sent %s %v", method, body)
	}
	if _, err = c.UpdateBillingEntity(ctx, "acme_us", nozle.BillingEntityParams{Code: "ignored", TaxProvider: nozle.TaxProviderNozle}); err != nil {
		t.Fatal(err)
	}
	be, _ = body["billing_entity"].(map[string]any)
	if method != "PUT" || path != "/api/v1/billing_entities/acme_us" || be["tax_provider"] != "nozle" || len(be) != 1 {
		t.Fatalf("update sent %s %s %v", method, path, body)
	}
}

func TestBillingEntitiesRefuseBadInputLocally(t *testing.T) {
	var calls int32
	c := taxClient(t, func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) })
	ctx := context.Background()
	var ve *nozle.ValidationError
	_, e1 := c.CreateBillingEntity(ctx, nozle.BillingEntityParams{Name: "x"})
	_, e2 := c.CreateBillingEntity(ctx, nozle.BillingEntityParams{Code: "x", Name: "x", TaxProvider: "avalara"})
	_, e3 := c.UpdateBillingEntity(ctx, "x", nozle.BillingEntityParams{TaxProvider: "avalara"})
	_, e4 := c.GetBillingEntity(ctx, " ")
	for i, err := range []error{e1, e2, e3, e4} {
		if !errors.As(err, &ve) {
			t.Fatalf("case %d: err = %v", i, err)
		}
	}
	if calls != 0 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestStripeAccountRequiredCode(t *testing.T) {
	c := taxClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"code":"stripe_account_required"}`))
	})
	_, err := c.UpdateBillingEntity(context.Background(), "acme_us", nozle.BillingEntityParams{TaxProvider: nozle.TaxProviderStripeTax})
	var api *nozle.APIError
	if !errors.As(err, &api) || !api.HasCode(nozle.CodeStripeAccountRequired) {
		t.Fatalf("err = %v", err)
	}
}
