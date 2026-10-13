package nozle

import "encoding/json"

// PingResult is the response from the /api/v1/ping endpoint.
type PingResult struct {
	OK      bool   `json:"ok"`
	OrgID   string `json:"org_id"`
	KeyType string `json:"key_type"`
	Env     string `json:"env"`
	Engine  string `json:"engine"`
	Version string `json:"version"`
}

// CanResult is the response from the /api/v1/can endpoint.
type CanResult struct {
	Allowed            bool              `json:"allowed"`
	Reason             string            `json:"reason,omitempty"`
	Used               int               `json:"used"`
	Limit              int               `json:"limit"`
	Remaining          int               `json:"remaining"`
	CostPerUseCents    float64           `json:"cost_per_use_cents"`
	RevenuePerUseCents float64           `json:"revenue_per_use_cents"`
	MarginPerUseCents  float64           `json:"margin_per_use_cents"`
	MinMarginPercent   float64           `json:"min_margin_percent,omitempty"`
	FeatureType        string            `json:"feature_type"`
	Configuration      map[string]string `json:"configuration"`
	Overage            bool              `json:"overage"`
	Economics          *CanEconomics     `json:"economics"`
	Policy             json.RawMessage   `json:"policy"`
}

// Plan is a billing plan returned by the /api/v1/plans endpoint.
type Plan struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	AmountCents    int    `json:"amount_cents"`
	AmountCurrency string `json:"amount_currency"`
	Interval       string `json:"interval"`
}

// TrackParams holds the fields for a single usage event.
type TrackParams struct {
	CustomerID     string
	Event          string
	Metadata       map[string]interface{}
	SubscriptionID string
	TransactionID  string
	Timestamp      string
}

// MeterEvent holds the fields for one event in a batch.
type MeterEvent struct {
	CustomerID     string
	Event          string
	Metadata       map[string]interface{}
	TransactionID  string
	SubscriptionID string
	Timestamp      string
}

// CheckoutParams holds the fields for creating a checkout session.
type CheckoutParams struct {
	OrganizationID   string // Deprecated: use CustomerID
	CustomerID       string
	PlanCode         string
	ReturnURL        string
	SubscriptionID   string
	QuoteID          string
	ExternalEntityID string
	IdempotencyKey   string
	RegisterMandate  *bool
}

// CheckoutResult is the response from the /api/v1/checkout endpoint. Type selects
// which fields are set: "stripe" (ClientSecret), "razorpay" (KeyID, OrderID,
// AmountCents, Currency, ExpiresAt, and for a mandate CustomerID, Recurring and
// MandateMaxAmountCents), "hosted" (PaymentURL), "processing" (CheckoutID and
// Status: Razorpay while the order is created, or Stripe when a quoted upgrade is
// replayed while its webhook is pending), "completed" and "scheduled". Fields that
// do not apply stay at their zero value.
type CheckoutResult struct {
	Type string `json:"type"`
	// URL is not returned by plan and invoice checkout, which never issue a
	// Stripe-hosted URL (credit top-up purchases do return one as PaymentURL).
	URL        string `json:"url"`
	PaymentURL string `json:"payment_url"`
	CheckoutID string `json:"checkout_id"`
	// ClientSecret is the embedded Stripe Checkout Session secret ("cs_...").
	ClientSecret string `json:"client_secret"`
	// PublishableKey and StripeAccount are not returned by Nozle. Use your own
	// Stripe publishable key for the Stripe account connected in Nozle.
	PublishableKey string `json:"publishable_key"`
	StripeAccount  string `json:"stripe_account"`
	KeyID          string `json:"key_id"`
	OrderID        string `json:"order_id"`
	InvoiceID      string `json:"invoice_id"`
	// PlanUpgradeIntentID is set instead of InvoiceID for a paid upgrade.
	PlanUpgradeIntentID string `json:"plan_upgrade_intent_id"`
	// AmountCents is in the currency's minor unit (paise for INR).
	AmountCents    int         `json:"amount_cents"`
	AmountDueCents json.Number `json:"amount_due_cents"`
	Currency       string      `json:"currency"`
	// ExpiresAt is set on Razorpay, hosted and processing results.
	ExpiresAt string `json:"expires_at"`
	// CustomerID, Recurring and MandateMaxAmountCents are set only on a Razorpay
	// mandate checkout. Pass CustomerID and recurring "1" to the Razorpay modal.
	CustomerID            string `json:"customer_id"`
	Recurring             bool   `json:"recurring"`
	MandateMaxAmountCents int    `json:"mandate_max_amount_cents"`
	Status                string `json:"status"`
	// PaymentSource is "credits" or "none" on a completed result.
	PaymentSource  string `json:"payment_source"`
	SubscriptionID string `json:"subscription_id"`
	PlanCode       string `json:"plan_code"`
	// TransitionDirection ("upgrade" or "downgrade") and TransitionPolicy are set
	// on a scheduled result.
	TransitionDirection    string `json:"transition_direction"`
	TransitionPolicy       string `json:"transition_policy"`
	ExternalSubscriptionID string `json:"external_subscription_id"`
	ExternalEntityID       string `json:"external_entity_id"`
	// PendingSubscriptionID is never returned. The pending subscription ID is
	// SubscriptionID.
	PendingSubscriptionID string `json:"pending_subscription_id"`
	EffectiveAt           string `json:"effective_at"`
	RenewalAt             string `json:"renewal_at"`
}

// SubscribeParams holds the fields for subscribing a customer.
type SubscribeParams struct {
	CustomerID string
	PlanCode   string
}

// SubscribeResult is the response from the /api/v1/subscribe endpoint.
type BillingSubscription struct {
	ExternalID   string `json:"external_id"`
	Status       string `json:"status"`
	PlanCode     string `json:"plan_code"`
	EndingAt     string `json:"ending_at"`
	TerminatedAt string `json:"terminated_at"`
}
type SubscribeResult struct {
	Subscription   BillingSubscription `json:"subscription"`
	SubscriptionID string              `json:"subscription_id"`
	Status         string              `json:"status"`
}

// CanEconomics preserves exact decimal estimates returned by Engine.
type CanEconomics struct {
	Status                      string   `json:"status"`
	Currency                    string   `json:"currency"`
	EstimatedIncrementalCost    Decimal  `json:"estimated_incremental_cost"`
	EstimatedIncrementalRevenue Decimal  `json:"estimated_incremental_revenue"`
	EstimatedIncrementalMargin  Decimal  `json:"estimated_incremental_margin"`
	EstimatedMarginPercent      Decimal  `json:"estimated_margin_percent"`
	RuleVersionIDs              []string `json:"rule_version_ids"`
	Includes                    []string `json:"includes"`
	Excludes                    []string `json:"excludes"`
	CalculatedAt                string   `json:"calculated_at"`
}
