package nozle

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestListAndGetInvoices(t *testing.T) {
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core/api/v1/invoices":
			want := map[string]string{"external_customer_id": "cus_1", "status": "finalized", "page": "2"}
			for k, v := range want {
				if r.URL.Query().Get(k) != v {
					t.Errorf("%s = %q", k, r.URL.Query().Get(k))
				}
			}
			_, _ = w.Write([]byte(`{"invoices":[{"nozle_id":"inv_1","number":"INV-1","total_amount_cents":2900}],"meta":{"current_page":2,"total_pages":3}}`))
		case "/core/api/v1/invoices/inv_1":
			_, _ = w.Write([]byte(`{"invoice":{"nozle_id":"inv_1","customer":{"external_id":"cus_1"}}}`))
		}
	})
	page, err := c.ListInvoices(context.Background(), "cus_1", ListInvoicesParams{Status: "finalized", Page: 2})
	if err != nil || page.Invoices[0].Number != "INV-1" || page.Meta.TotalPages != 3 {
		t.Fatalf("page %+v %v", page, err)
	}
	inv, err := c.GetInvoice(context.Background(), "inv_1")
	if err != nil || inv.Customer == nil || inv.Customer.ExternalID != "cus_1" {
		t.Fatalf("invoice %+v %v", inv, err)
	}
}

func TestInvoiceDownloadURL(t *testing.T) {
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if r.URL.Path == "/core/api/v1/invoices/inv_1/download" {
			_, _ = w.Write([]byte(`{"invoice":{"file_url":"https://files.example/inv_1.pdf"}}`))
		}
	})
	if u, err := c.InvoiceDownloadURL(context.Background(), "inv_1"); err != nil || u != "https://files.example/inv_1.pdf" {
		t.Fatalf("%q %v", u, err)
	}
	if u, err := c.InvoiceDownloadURL(context.Background(), "inv_2"); err != nil || u != "" {
		t.Fatalf("generating: %q %v", u, err)
	}
}

func TestPreviewInvoiceBodies(t *testing.T) {
	var bodies []map[string]any
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"invoice":{"nozle_id":"preview"}}`))
	})
	ctx := context.Background()
	if _, err := c.PreviewInvoice(ctx, PreviewInvoiceParams{CustomerID: "cus_1", SubscriptionIDs: []string{"sub_1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PreviewInvoice(ctx, PreviewInvoiceParams{CustomerID: "cus_1", PlanCode: "pro", BillingTime: "anniversary"}); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"customer": map[string]any{"external_id": "cus_1"}, "subscriptions": map[string]any{"external_ids": []any{"sub_1"}}},
		{"customer": map[string]any{"external_id": "cus_1"}, "plan_code": "pro", "billing_time": "anniversary"},
	}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("bodies %v", bodies)
	}
	if _, err := c.PreviewInvoice(ctx, PreviewInvoiceParams{CustomerID: "cus_1"}); err == nil {
		t.Fatal("expected a validation error")
	}
}
