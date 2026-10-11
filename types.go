package nozle

// PingResult is the response from the /api/v1/ping endpoint.
type PingResult struct {
	OK      bool   `json:"ok"`
	OrgID   string `json:"org_id"`
	KeyType string `json:"key_type"`
	Env     string `json:"env"`
}

// CanResult is the response from the /api/v1/can endpoint.
type CanResult struct {
	Allowed            bool    `json:"allowed"`
	Reason             string  `json:"reason,omitempty"`
	Used               int     `json:"used"`
	Limit              int     `json:"limit"`
	Remaining          int     `json:"remaining"`
	CostPerUseCents    float64 `json:"cost_per_use_cents"`
	RevenuePerUseCents float64 `json:"revenue_per_use_cents"`
	MarginPerUseCents  float64 `json:"margin_per_use_cents"`
	MinMarginPercent   float64 `json:"min_margin_percent,omitempty"`
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

// LLMParams holds the fields for an LLM cost lookup.
type LLMParams struct {
	Model        string
	InputTokens  int
	OutputTokens int
	CustomerID   string
}

// LLMResult is the response from the /api/v1/llm endpoint.
type LLMResult struct {
	CostCents       float64 `json:"cost_cents"`
	InputCostCents  float64 `json:"input_cost_cents"`
	OutputCostCents float64 `json:"output_cost_cents"`
	Model           string  `json:"model"`
}

// CheckoutParams holds the fields for creating a checkout session.
type CheckoutParams struct {
	OrganizationID string // Deprecated: use CustomerID
 CustomerID string
 PlanCode string
 ReturnURL string
 SubscriptionID string
 QuoteID string
 ExternalEntityID string
 IdempotencyKey string
 RegisterMandate *bool
}

// CheckoutResult is the response from the /api/v1/checkout endpoint.
type CheckoutResult struct {
	Type string `json:"type"`
 URL string `json:"url"`
 PaymentURL string `json:"payment_url"`
 CheckoutID string `json:"checkout_id"`
 ClientSecret string `json:"client_secret"`
	InvoiceID    string `json:"invoice_id"`
	AmountCents  int    `json:"amount_cents"`
	Currency     string `json:"currency"`
}

// SubscribeParams holds the fields for subscribing a customer.
type SubscribeParams struct {
	CustomerID string
	PlanCode   string
}

// SubscribeResult is the response from the /api/v1/subscribe endpoint.
type SubscribeResult struct {
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
}
