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
	// DisputedAmountCents is what lost payment disputes took back from the invoice's payments (Core 11.45.0),
	// 0 without a lost dispute.
	DisputedAmountCents int64   `json:"disputed_amount_cents"`
	FileURL             *string `json:"file_url"`
	// From Cores with customer tax data. TaxStatus is "pending", "succeeded" or "failed"; TaxDetails is nil on
	// invoices finalized before customer tax data existed.
	TaxStatus    string             `json:"tax_status"`
	TaxTreatment TaxTreatment       `json:"tax_treatment"`
	TaxDetails   *InvoiceTaxDetails `json:"tax_details"`
	Customer     *struct {
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

// Codes for APIError.HasCode on PayInvoiceWithAgentToken, besides "invalid_format" (token not an spt_ id),
// "feature_not_enabled", "invoice_not_payable" and "stripe_required".
const (
	CodeTokenExpired       = "token_expired"
	CodeTokenDeactivated   = "token_deactivated"
	CodeCurrencyMismatch   = "currency_mismatch"
	CodeAmountExceedsLimit = "amount_exceeds_limit"
)

// PayInvoiceWithAgentToken pays a finalized invoice with a Shared Payment Token ("spt_...") an AI agent
// handed over. Needs a secret key and a workspace with the agent payment token feature on (otherwise a 403
// "feature_not_enabled"). Returns the invoice; read PaymentStatus, as a charge can still fail or stay
// pending. A refused token is an *APIError (422): test it with HasCode(CodeTokenExpired) and the others
// above. Not retried.
func (c *Client) PayInvoiceWithAgentToken(ctx context.Context, invoiceID, agentPaymentToken string) (Invoice, error) {
	var out struct {
		Invoice Invoice `json:"invoice"`
	}
	if err := c.invoiceCheck("pay_invoice_with_agent_token", "invoice_id", invoiceID); err != nil {
		return out.Invoice, err
	}
	if err := required("pay_invoice_with_agent_token", "agent_payment_token", agentPaymentToken); err != nil {
		return out.Invoice, err
	}
	body := map[string]string{"agent_payment_token": agentPaymentToken}
	err := c.request(ctx, "pay_invoice_with_agent_token", http.MethodPost, true, "/api/v1/invoices/"+segment(invoiceID)+"/pay_with_agent_token", body, &out, "")
	return out.Invoice, err
}

// Codes for APIError.HasCode on GetInvoiceX402Requirements and PayInvoiceWithX402, besides
// "feature_not_enabled" (403), "invoice_not_payable" (422) and "invalid_format" (transaction hash).
const (
	CodeUSDRequired    = "usd_required"
	CodeStripeRequired = "stripe_required"
)

// X402Requirements is the x402 (v2) payment requirements for an invoice. Relay it to the paying agent in
// an HTTP 402 response.
type X402Requirements struct {
	X402Version int                 `json:"x402Version"`
	Accepts     []X402PaymentOption `json:"accepts"`
	Resource    X402Resource        `json:"resource"`
}

// X402PaymentOption is one way to pay: Amount is in atomic USDC units (6 decimals) as a string.
type X402PaymentOption struct {
	Scheme            string    `json:"scheme"`
	Network           string    `json:"network"`
	Amount            string    `json:"amount"`
	Asset             string    `json:"asset"`
	PayTo             string    `json:"payTo"`
	MaxTimeoutSeconds int       `json:"maxTimeoutSeconds"`
	Extra             X402Extra `json:"extra"`
}

// X402Extra is the USDC EIP-712 domain the "exact" EVM scheme signs against.
type X402Extra struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// X402Resource describes what is being paid for.
type X402Resource struct {
	Description string `json:"description"`
}

// GetInvoiceX402Requirements returns the x402 payment requirements for a finalized USD invoice on a Stripe
// workspace with the x402 feature on (otherwise a 403 "feature_not_enabled"). Refusals are an *APIError
// (422): HasCode(CodeUSDRequired), CodeStripeRequired or "invoice_not_payable". Safe to retry.
func (c *Client) GetInvoiceX402Requirements(ctx context.Context, invoiceID string) (X402Requirements, error) {
	var out struct {
		Requirements X402Requirements `json:"x402_requirements"`
	}
	if err := c.invoiceCheck("get_invoice_x402_requirements", "invoice_id", invoiceID); err != nil {
		return out.Requirements, err
	}
	err := c.request(ctx, "get_invoice_x402_requirements", http.MethodGet, true, "/api/v1/invoices/"+segment(invoiceID)+"/x402_requirements", nil, &out, "")
	return out.Requirements, err
}

// PayInvoiceWithX402 pays a finalized USD invoice with a USDC payment the agent already settled on-chain to
// the address from GetInvoiceX402Requirements. transactionHash is the 0x-prefixed 32-byte hash; one
// transaction pays one invoice once. Returns the invoice; read PaymentStatus, as it is often "processing"
// first. A refusal is an *APIError: HasCode("invalid_format") for a bad hash, CodeUSDRequired,
// CodeStripeRequired, "invoice_not_payable". Not retried (moves money).
func (c *Client) PayInvoiceWithX402(ctx context.Context, invoiceID, transactionHash string) (Invoice, error) {
	var out struct {
		Invoice Invoice `json:"invoice"`
	}
	if err := c.invoiceCheck("pay_invoice_with_x402", "invoice_id", invoiceID); err != nil {
		return out.Invoice, err
	}
	if err := required("pay_invoice_with_x402", "transaction_hash", transactionHash); err != nil {
		return out.Invoice, err
	}
	body := map[string]string{"transaction_hash": transactionHash}
	err := c.request(ctx, "pay_invoice_with_x402", http.MethodPost, true, "/api/v1/invoices/"+segment(invoiceID)+"/pay_with_x402", body, &out, "")
	return out.Invoice, err
}

func (c *Client) invoiceCheck(op, field, value string) error {
	if err := validateKey(c.apiKey, op); err != nil {
		return err
	}
	return required(op, field, value)
}
