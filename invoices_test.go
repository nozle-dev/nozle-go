package nozle

import (
	"context"
	"net/http"
	"testing"
)

func TestListAndGetInvoices(t *testing.T) {
	overdue := true
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core/api/v1/invoices":
			want := map[string]string{"external_customer_id": "cus_1", "status": "finalized", "payment_overdue": "true", "page": "2"}
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
	page, err := c.ListInvoices(context.Background(), "cus_1", ListInvoicesParams{Status: "finalized", PaymentOverdue: &overdue, Page: 2})
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
