package nozle

import (
	"context"
	"encoding/json"
	"errors"
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

func TestPayInvoiceWithAgentToken(t *testing.T) {
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPost || r.URL.Path != "/core/api/v1/invoices/inv_1/pay_with_agent_token" || body["agent_payment_token"] == "" {
			t.Errorf("wrong request: %s %s %v", r.Method, r.URL.Path, body)
		}
		if body["agent_payment_token"] == "spt_ok" {
			_, _ = w.Write([]byte(`{"invoice":{"nozle_id":"inv_1","payment_status":"succeeded"}}`))
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"status":422,"error":"Unprocessable Entity","code":"validation_errors","error_details":{"agent_payment_token":["token_expired"]}}`))
	})
	ctx := context.Background()
	inv, err := c.PayInvoiceWithAgentToken(ctx, "inv_1", "spt_ok")
	if err != nil || inv.PaymentStatus != "succeeded" {
		t.Fatalf("invoice %+v %v", inv, err)
	}
	var api *APIError
	if _, err = c.PayInvoiceWithAgentToken(ctx, "inv_1", "spt_old"); !errors.As(err, &api) || !api.HasCode(CodeTokenExpired) {
		t.Fatalf("want token_expired, got %v", err)
	}
	var invalid *ValidationError
	if _, err = c.PayInvoiceWithAgentToken(ctx, "", "spt_ok"); !errors.As(err, &invalid) {
		t.Fatalf("empty id: %v", err)
	}
	if _, err = c.PayInvoiceWithAgentToken(ctx, "inv_1", " "); !errors.As(err, &invalid) {
		t.Fatalf("empty token: %v", err)
	}
}

func TestInvoiceX402(t *testing.T) {
	posts := 0
	c := usageServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core/api/v1/invoices/inv_1/x402_requirements":
			_, _ = w.Write([]byte(`{"x402_requirements":{"x402Version":2,"accepts":[{"scheme":"exact","network":"eip155:8453","amount":"10000000","asset":"0xabc","payTo":"0xdef","maxTimeoutSeconds":300,"extra":{"name":"USD Coin","version":"2"}}],"resource":{"description":"Acme - Invoice 1"}}}`))
		case "/core/api/v1/invoices/inv_1/pay_with_x402":
			posts++
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.Method != http.MethodPost || body["transaction_hash"] == "" {
				t.Errorf("wrong request: %s %v", r.Method, body)
			}
			if body["transaction_hash"] == "0xgood" {
				_, _ = w.Write([]byte(`{"invoice":{"nozle_id":"inv_1","payment_status":"pending"}}`))
				return
			}
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"status":422,"error":"Unprocessable Entity","code":"validation_errors","error_details":{"transaction_hash":["invalid_format"]}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	req, err := c.GetInvoiceX402Requirements(ctx, "inv_1")
	if err != nil || req.X402Version != 2 || len(req.Accepts) != 1 {
		t.Fatalf("requirements %+v %v", req, err)
	}
	if a := req.Accepts[0]; a.Amount != "10000000" || a.PayTo != "0xdef" || a.MaxTimeoutSeconds != 300 || a.Extra.Name != "USD Coin" || req.Resource.Description != "Acme - Invoice 1" {
		t.Fatalf("option %+v", a)
	}
	inv, err := c.PayInvoiceWithX402(ctx, "inv_1", "0xgood")
	if err != nil || inv.PaymentStatus != "pending" {
		t.Fatalf("invoice %+v %v", inv, err)
	}
	var api *APIError
	posts = 0
	if _, err = c.PayInvoiceWithX402(ctx, "inv_1", "0xbad"); !errors.As(err, &api) || !api.HasCode("invalid_format") {
		t.Fatalf("want invalid_format, got %v", err)
	}
	if posts != 1 {
		t.Fatalf("money call retried: %d posts", posts)
	}
	var invalid *ValidationError
	if _, err = c.PayInvoiceWithX402(ctx, "inv_1", " "); !errors.As(err, &invalid) {
		t.Fatalf("empty hash: %v", err)
	}
	if _, err = c.GetInvoiceX402Requirements(ctx, ""); !errors.As(err, &invalid) {
		t.Fatalf("empty id: %v", err)
	}
}
