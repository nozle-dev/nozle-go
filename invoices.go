package nozle

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Invoice is an invoice. The API reference lists every field; these are the ones most code reads.
type Invoice struct {
	NozleID          string  `json:"nozle_id"`
	Number           string  `json:"number"`
	IssuingDate      string  `json:"issuing_date"`
	PaymentDueDate   *string `json:"payment_due_date"`
	PaymentOverdue   bool    `json:"payment_overdue"`
	Status           string  `json:"status"`
	PaymentStatus    string  `json:"payment_status"`
	Currency         string  `json:"currency"`
	FeesAmountCents  int64   `json:"fees_amount_cents"`
	TaxesAmountCents int64   `json:"taxes_amount_cents"`
	TotalAmountCents int64   `json:"total_amount_cents"`
	FileURL          *string `json:"file_url"`
	Customer         *struct {
		ExternalID string `json:"external_id"`
	} `json:"customer"`
}

// InvoicePage is one page of invoices.
type InvoicePage struct {
	Invoices []Invoice `json:"invoices"`
	Meta     struct {
		CurrentPage int  `json:"current_page"`
		NextPage    *int `json:"next_page"`
		PrevPage    *int `json:"prev_page"`
		TotalPages  int  `json:"total_pages"`
		TotalCount  int  `json:"total_count"`
	} `json:"meta"`
}

// ListInvoicesParams filters ListInvoices. Empty fields are not sent; dates are inclusive YYYY-MM-DD.
type ListInvoicesParams struct {
	Status, PaymentStatus, IssuingDateFrom, IssuingDateTo string
	// PaymentOverdue, when set, keeps only unpaid invoices past their due date (true) or the others.
	PaymentOverdue *bool
	Page, PerPage  int
}

// ListInvoices returns a customer's invoices, newest first.
func (c *Client) ListInvoices(ctx context.Context, customerID string, p ListInvoicesParams) (InvoicePage, error) {
	var out InvoicePage
	if err := c.invoiceCheck("list_invoices", "customer_id", customerID); err != nil {
		return out, err
	}
	q := url.Values{"external_customer_id": {customerID}}
	for name, value := range map[string]string{"status": p.Status, "payment_status": p.PaymentStatus, "issuing_date_from": p.IssuingDateFrom, "issuing_date_to": p.IssuingDateTo} {
		if value != "" {
			q.Set(name, value)
		}
	}
	if p.PaymentOverdue != nil {
		q.Set("payment_overdue", strconv.FormatBool(*p.PaymentOverdue))
	}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(p.PerPage))
	}
	err := c.request(ctx, "list_invoices", http.MethodGet, true, query("/api/v1/invoices", q), nil, &out, "")
	return out, err
}

// GetInvoice returns one invoice.
func (c *Client) GetInvoice(ctx context.Context, invoiceID string) (Invoice, error) {
	var out struct {
		Invoice Invoice `json:"invoice"`
	}
	if err := c.invoiceCheck("get_invoice", "invoice_id", invoiceID); err != nil {
		return out.Invoice, err
	}
	err := c.request(ctx, "get_invoice", http.MethodGet, true, "/api/v1/invoices/"+segment(invoiceID), nil, &out, "")
	return out.Invoice, err
}

// InvoiceDownloadURL returns the PDF's URL, or "" while Core is still generating it (ask again
// shortly).
func (c *Client) InvoiceDownloadURL(ctx context.Context, invoiceID string) (string, error) {
	var out struct {
		Invoice struct {
			FileURL *string `json:"file_url"`
		} `json:"invoice"`
	}
	if err := c.invoiceCheck("invoice_download_url", "invoice_id", invoiceID); err != nil {
		return "", err
	}
	if err := c.request(ctx, "invoice_download_url", http.MethodPost, true, "/api/v1/invoices/"+segment(invoiceID)+"/download", nil, &out, ""); err != nil {
		return "", err
	}
	if out.Invoice.FileURL == nil {
		return "", nil
	}
	return *out.Invoice.FileURL, nil
}

func (c *Client) invoiceCheck(op, field, value string) error {
	if err := validateKey(c.apiKey, op); err != nil {
		return err
	}
	return required(op, field, value)
}
