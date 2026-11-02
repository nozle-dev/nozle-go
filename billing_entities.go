package nozle

import (
	"context"
	"net/http"
	"strconv"
)

// TaxProvider says who computes tax on a billing entity's invoices.
type TaxProvider string

const (
	TaxProviderNozle     TaxProvider = "nozle"      // Nozle's own tax rates
	TaxProviderStripeTax TaxProvider = "stripe_tax" // Stripe Tax; needs a connected Stripe account
)

// BillingEntity is a seller identity that issues invoices. TaxProvider and ProviderTaxCode are the Stripe Tax
// settings: ProviderTaxCode is the default Stripe product tax code (for example "txcd_10103001"), nil when unset.
// Taxes and SelectedInvoiceCustomSections are only filled by GetBillingEntity and UpdateBillingEntity. The API
// reference lists every field.
type BillingEntity struct {
	NozleID                                  string      `json:"nozle_id"`
	Code                                     string      `json:"code"`
	Name                                     string      `json:"name"`
	IsDefault                                bool        `json:"is_default"`
	DefaultCurrency                          string      `json:"default_currency"`
	Email                                    *string     `json:"email"`
	LegalName                                *string     `json:"legal_name"`
	LegalNumber                              *string     `json:"legal_number"`
	TaxIdentificationNumber                  *string     `json:"tax_identification_number"`
	AddressLine1                             *string     `json:"address_line1"`
	AddressLine2                             *string     `json:"address_line2"`
	City                                     *string     `json:"city"`
	State                                    *string     `json:"state"`
	Zipcode                                  *string     `json:"zipcode"`
	Country                                  *string     `json:"country"`
	Timezone                                 string      `json:"timezone"`
	NetPaymentTerm                           int         `json:"net_payment_term"`
	DocumentNumbering                        string      `json:"document_numbering"`
	DocumentNumberPrefix                     string      `json:"document_number_prefix"`
	FinalizeZeroAmountInvoice                bool        `json:"finalize_zero_amount_invoice"`
	EUTaxManagement                          bool        `json:"eu_tax_management"`
	TaxProvider                              TaxProvider `json:"tax_provider"`
	ProviderTaxCode                          *string     `json:"provider_tax_code"`
	InvoiceFooter                            *string     `json:"invoice_footer"`
	InvoiceGracePeriod                       int         `json:"invoice_grace_period"`
	SubscriptionInvoiceIssuingDateAnchor     string      `json:"subscription_invoice_issuing_date_anchor"`
	SubscriptionInvoiceIssuingDateAdjustment string      `json:"subscription_invoice_issuing_date_adjustment"`
	DocumentLocale                           string      `json:"document_locale"`
	LogoURL                                  *string     `json:"logo_url"`
	CreatedAt                                string      `json:"created_at"`
	UpdatedAt                                string      `json:"updated_at"`
	Taxes                                    []any       `json:"taxes"`
	SelectedInvoiceCustomSections            []any       `json:"selected_invoice_custom_sections"`
}

// BillingEntityParams creates or updates a billing entity. Empty fields are not sent, so an update changes only
// what is set. Code and Name are required on create; Code cannot change afterwards. TaxProvider is
// TaxProviderNozle or TaxProviderStripeTax; Core refuses stripe_tax without a connected Stripe account
// (CodeStripeAccountRequired). Country is an ISO 3166-1 alpha-2 code.
type BillingEntityParams struct {
	Code                      string      `json:"code,omitempty"`
	Name                      string      `json:"name,omitempty"`
	Email                     string      `json:"email,omitempty"`
	LegalName                 string      `json:"legal_name,omitempty"`
	LegalNumber               string      `json:"legal_number,omitempty"`
	TaxIdentificationNumber   string      `json:"tax_identification_number,omitempty"`
	AddressLine1              string      `json:"address_line1,omitempty"`
	AddressLine2              string      `json:"address_line2,omitempty"`
	City                      string      `json:"city,omitempty"`
	State                     string      `json:"state,omitempty"`
	Zipcode                   string      `json:"zipcode,omitempty"`
	Country                   string      `json:"country,omitempty"`
	DefaultCurrency           string      `json:"default_currency,omitempty"`
	Timezone                  string      `json:"timezone,omitempty"`
	NetPaymentTerm            *int        `json:"net_payment_term,omitempty"`
	FinalizeZeroAmountInvoice *bool       `json:"finalize_zero_amount_invoice,omitempty"`
	EUTaxManagement           *bool       `json:"eu_tax_management,omitempty"`
	TaxProvider               TaxProvider `json:"tax_provider,omitempty"`
	// ProviderTaxCode is the default Stripe product tax code. shortcut: empty is not sent, so it cannot be cleared; use a pointer if needed.
	ProviderTaxCode string `json:"provider_tax_code,omitempty"`
}

// BillingEntityList answers ListBillingEntities.
type BillingEntityList struct {
	BillingEntities []BillingEntity `json:"billing_entities"`
}

func billingEntityPath(code string) string { return "/api/v1/billing_entities/" + segment(code) }

func checkTaxProvider(op string, p TaxProvider) error {
	if p != "" && p != TaxProviderNozle && p != TaxProviderStripeTax {
		return &ValidationError{op, "tax_provider " + strconv.Quote(string(p)) + " must be nozle or stripe_tax"}
	}
	return nil
}

// ListBillingEntities returns every billing entity of the organization. Secret key.
func (c *Client) ListBillingEntities(ctx context.Context) (BillingEntityList, error) {
	var out BillingEntityList
	if err := validateSecret(c, "billing_entities.list"); err != nil {
		return out, err
	}
	err := c.request(ctx, "billing_entities.list", http.MethodGet, true, "/api/v1/billing_entities", nil, &out, "")
	return out, err
}

// GetBillingEntity returns one billing entity by code, with its taxes and invoice sections. Secret key.
func (c *Client) GetBillingEntity(ctx context.Context, code string) (BillingEntity, error) {
	const op = "billing_entities.get"
	var out struct {
		BillingEntity BillingEntity `json:"billing_entity"`
	}
	if err := validateSecret(c, op); err != nil {
		return out.BillingEntity, err
	}
	if err := required(op, "code", code); err != nil {
		return out.BillingEntity, err
	}
	err := c.request(ctx, op, http.MethodGet, true, billingEntityPath(code), nil, &out, "")
	return out.BillingEntity, err
}

// CreateBillingEntity creates a billing entity. It sends no Idempotency-Key, so it is never retried: after a
// transport error or a 5xx, call GetBillingEntity with the code before trying again.
func (c *Client) CreateBillingEntity(ctx context.Context, p BillingEntityParams) (BillingEntity, error) {
	const op = "billing_entities.create"
	var out struct {
		BillingEntity BillingEntity `json:"billing_entity"`
	}
	if err := validateSecret(c, op); err != nil {
		return out.BillingEntity, err
	}
	if err := required(op, "code", p.Code); err != nil {
		return out.BillingEntity, err
	}
	if err := required(op, "name", p.Name); err != nil {
		return out.BillingEntity, err
	}
	if err := checkTaxProvider(op, p.TaxProvider); err != nil {
		return out.BillingEntity, err
	}
	err := c.request(ctx, op, http.MethodPost, true, "/api/v1/billing_entities", map[string]any{"billing_entity": p}, &out, "")
	return out.BillingEntity, err
}

// UpdateBillingEntity changes the set fields of the billing entity with this code. p.Code is ignored: the code
// cannot be changed. An update repeats safely, so it is retried like a read.
func (c *Client) UpdateBillingEntity(ctx context.Context, code string, p BillingEntityParams) (BillingEntity, error) {
	const op = "billing_entities.update"
	var out struct {
		BillingEntity BillingEntity `json:"billing_entity"`
	}
	if err := validateSecret(c, op); err != nil {
		return out.BillingEntity, err
	}
	if err := required(op, "code", code); err != nil {
		return out.BillingEntity, err
	}
	if err := checkTaxProvider(op, p.TaxProvider); err != nil {
		return out.BillingEntity, err
	}
	p.Code = ""
	err := c.request(ctx, op, http.MethodPut, true, billingEntityPath(code), map[string]any{"billing_entity": p}, &out, "")
	return out.BillingEntity, err
}
