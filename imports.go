package nozle

import "context"

// CreditBalanceImportParams is the opening balance of a subscription imported with ImportBillingState.
type CreditBalanceImportParams struct {
	ExternalSubscriptionID string  `json:"external_subscription_id"`
	CreditSystemCode       string  `json:"credit_system_code"`
	GrantedAmount          Decimal `json:"granted_amount"`   // more than 0
	RemainingAmount        Decimal `json:"remaining_amount"` // from 0 to GrantedAmount
	ExternalEntityID       string  `json:"external_entity_id,omitempty"`
	DryRun                 bool    `json:"dry_run,omitempty"` // validate without writing
}

// CreditBalanceImport is the Engine's answer; Replayed is true for a retry with the same key.
type CreditBalanceImport struct {
	OperationID            string  `json:"operation_id,omitempty"`
	BalanceSourceID        string  `json:"balance_source_id,omitempty"`
	ExternalCustomerID     string  `json:"external_customer_id"`
	ExternalEntityID       string  `json:"external_entity_id,omitempty"`
	ExternalSubscriptionID string  `json:"external_subscription_id"`
	CreditSystemCode       string  `json:"credit_system_code"`
	GrantedAmount          Decimal `json:"granted_amount"`
	RemainingAmount        Decimal `json:"remaining_amount"`
	CurrentPeriodStart     string  `json:"current_period_start"`
	ExpiresAt              string  `json:"expires_at,omitempty"`
	InvoiceCreated         bool    `json:"invoice_created"`
	DryRun                 bool    `json:"dry_run"`
	Replayed               bool    `json:"replayed"`
}

// BillingStateImportSubscription is a subscription another system already bills.
// Status must be "active" and BillingAuthority "external"; the period (RFC 3339) must contain
// the current time and span exactly one plan interval.
type BillingStateImportSubscription struct {
	PlanCode           string `json:"plan_code"`
	Status             string `json:"status"`
	BillingAuthority   string `json:"billing_authority"`
	ExternalReference  string `json:"external_reference"`
	CurrentPeriodStart string `json:"current_period_start"`
	CurrentPeriodEnd   string `json:"current_period_end"`
}

// BillingStateImportParams is sent as {"billing_state_import": ...}.
type BillingStateImportParams struct {
	Subscription     BillingStateImportSubscription `json:"subscription"`
	ExternalEntityID string                         `json:"external_entity_id,omitempty"`
	DryRun           bool                           `json:"dry_run,omitempty"` // validate without writing
}

// ImportedSubscription is the subscription a billing-state import created or validated.
type ImportedSubscription struct {
	ID                     string `json:"id,omitempty"`
	ExternalSubscriptionID string `json:"external_subscription_id,omitempty"`
	ExternalReference      string `json:"external_reference,omitempty"`
	PlanCode               string `json:"plan_code,omitempty"`
	Status                 string `json:"status,omitempty"`
	BillingAuthority       string `json:"billing_authority,omitempty"`
	CreditImportRequired   bool   `json:"credit_import_required,omitempty"`
	CurrentPeriodStart     string `json:"current_period_start,omitempty"`
	CurrentPeriodEnd       string `json:"current_period_end,omitempty"`
	InvoiceCreated         bool   `json:"invoice_created,omitempty"`
}

// BillingStateImport is Core's answer: Status "completed", or "validated" for a dry run.
type BillingStateImport struct {
	ID                 string               `json:"id,omitempty"`
	Status             string               `json:"status"`
	DryRun             bool                 `json:"dry_run"`
	ExternalCustomerID string               `json:"external_customer_id"`
	ExternalEntityID   string               `json:"external_entity_id,omitempty"`
	Subscription       ImportedSubscription `json:"subscription"`
	Replayed           bool                 `json:"replayed"`
}

// ImportCreditBalance records the opening credit balance of a subscription imported with
// ImportBillingState, without an invoice or usage event (Engine). key is the required
// Idempotency-Key (at most 255 bytes): retry an uncertain call with the same key and params.
func (c *Client) ImportCreditBalance(ctx context.Context, customerID string, p CreditBalanceImportParams, key string) (CreditBalanceImport, error) {
	var out struct {
		Import CreditBalanceImport `json:"credit_balance_import"`
	}
	err := c.importCall(ctx, "customers.import_credit_balance", false, customerID, "credit-balance-imports", p, key, &out)
	return out.Import, err
}

// ImportBillingState imports a subscription another system already bills (billing_authority
// "external"), without an invoice, checkout or payment (Core). key is the required
// Idempotency-Key (at most 255 bytes): retry an uncertain call with the same key and params.
func (c *Client) ImportBillingState(ctx context.Context, customerID string, p BillingStateImportParams, key string) (BillingStateImport, error) {
	var out struct {
		Import BillingStateImport `json:"billing_state_import"`
	}
	body := map[string]any{"billing_state_import": p}
	err := c.importCall(ctx, "customers.import_billing_state", true, customerID, "billing-state-imports", body, key, &out)
	return out.Import, err
}

func (c *Client) importCall(ctx context.Context, op string, core bool, customerID, route string, body any, key string, out any) error {
	if err := validateSecret(c, op); err != nil {
		return err
	}
	if err := required(op, "customer_id", customerID); err != nil {
		return err
	}
	if err := idem(op, key); err != nil {
		return err
	}
	return c.request(ctx, op, "POST", core, customerPath(customerID)+"/"+route, body, out, key)
}
