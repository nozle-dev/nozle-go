package nozle

import "encoding/json"

// PingResult is the response from the /api/v1/ping endpoint.
type PingResult struct {
	OK     bool   `json:"ok"`
	Engine string `json:"engine"`
}

// CanResult is the response from the /api/v1/can endpoint.
type CanResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
	Used    int    `json:"used"`
	// Limit and Remaining are nil for an unlimited feature (the API sends null), never 0: a zero allowance means none left.
	Limit         *int              `json:"limit"`
	Remaining     *int              `json:"remaining"`
	FeatureType   string            `json:"feature_type"`
	Configuration map[string]string `json:"configuration"`
	Overage       bool              `json:"overage"`
	Economics     *CanEconomics     `json:"economics"`
	Policy        json.RawMessage   `json:"policy"`
}

// Plan is a billing plan returned by the /api/v1/plans endpoint.
type Plan struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	AmountCents    int    `json:"amount_cents"`
	AmountCurrency string `json:"amount_currency"`
	Interval       string `json:"interval"`
	// Version is the version of the code this plan is (plan versioning); IsCurrent is true for the version new
	// subscriptions get. Plans returns current versions only.
	Version   int  `json:"version"`
	IsCurrent bool `json:"is_current"`
	// Family is the plan family this plan is a variant of; nil for a plan without a family.
	Family *PlanFamilyRef `json:"family,omitempty"`
	// Prices lists every current variant of the family: one price per interval and currency.
	Prices []PlanPrice `json:"prices,omitempty"`
}

// PlanFamilyRef is a plan's family as the catalog returns it: the currencies it is sold in, in order, and the
// default one.
type PlanFamilyRef struct {
	Code            string   `json:"code"`
	Currencies      []string `json:"currencies"`
	DefaultCurrency string   `json:"default_currency"`
}

// PlanPrice is one variant of a plan family: the plan sold for an interval in a currency.
type PlanPrice struct {
	Currency    string `json:"currency"`
	Interval    string `json:"interval"`
	PlanCode    string `json:"plan_code"`
	AmountCents int    `json:"amount_cents"`
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
	OrganizationID string // Deprecated: use CustomerID
	CustomerID     string
	PlanCode       string
	// PlanFamilyCode with Interval (and optional Currency) replaces PlanCode: Core picks the family's variant.
	// Without Currency it uses the customer's locked currency, else the family's default.
	PlanFamilyCode string
	Interval       string
	Currency       string
	ReturnURL      string
	// CancelURL is where Stripe sends a customer who leaves the hosted page. It is validated like
	// ReturnURL and defaults to it. A return URL may contain the literal {CHECKOUT_SESSION_ID}.
	CancelURL        string
	SubscriptionID   string
	QuoteID          string
	ExternalEntityID string
	IdempotencyKey   string
	RegisterMandate  *bool
}

// ManagePaymentMethodResult is the result of ManagePaymentMethod (Stripe only).
type ManagePaymentMethodResult struct {
	// URL is the Stripe billing page to redirect the customer to.
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
	// Provider is "stripe".
	Provider string `json:"provider"`
}

// CheckoutResult is the response from the /api/v1/checkout endpoint. Type selects
// which fields are set: "hosted" (URL, PaymentURL: the Stripe Checkout page or a Razorpay
// link; redirect the customer to URL), "razorpay" (KeyID, OrderID,
// AmountCents, Currency, ExpiresAt, and for a mandate CustomerID, Recurring and
// MandateMaxAmountCents), "processing" (CheckoutID and
// Status: Razorpay while the order is created, or Stripe when a quoted upgrade is
// replayed while its webhook is pending), "completed" and "scheduled". Fields that
// do not apply stay at their zero value.
type CheckoutResult struct {
	Type string `json:"type"`
	// URL is the hosted page to redirect the customer to; PaymentURL is the same address.
	URL        string `json:"url"`
	PaymentURL string `json:"payment_url"`
	CheckoutID string `json:"checkout_id"`
	KeyID      string `json:"key_id"`
	OrderID    string `json:"order_id"`
	InvoiceID  string `json:"invoice_id"`
	// PlanUpgradeIntentID is set instead of InvoiceID for a paid upgrade.
	PlanUpgradeIntentID string `json:"plan_upgrade_intent_id"`
	// AmountCents is in the currency's minor unit (paise for INR).
	AmountCents int `json:"amount_cents"`
	// AmountDueCents is "" when Engine omitted it and "0" when it sent 0.
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
	EffectiveAt            string `json:"effective_at"`
	RenewalAt              string `json:"renewal_at"`
}

// SubscribeParams holds the fields for subscribing a customer.
type SubscribeParams struct {
	CustomerID string
	PlanCode   string
	// PlanFamilyCode with Interval (and optional Currency) replaces PlanCode: Core picks the family's variant.
	PlanFamilyCode string
	Interval       string
	Currency       string
	// PlanVersion, when set, sells that version of PlanCode instead of the current one (for example an older price).
	// The same customer and code again keep the subscription's own version; another version of an existing
	// subscription returns a 422 *APIError.
	PlanVersion int
}

// SubscribeResult is the response from the /api/v1/subscribe endpoint.
type BillingSubscription struct {
	ExternalID   string `json:"external_id"`
	Status       string `json:"status"`
	PlanCode     string `json:"plan_code"`
	PlanVersion  int    `json:"plan_version"`
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
