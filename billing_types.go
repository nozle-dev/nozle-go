package nozle

import "encoding/json"

type CheckoutScope struct{ CustomerID, SubscriptionID string }
type CheckoutStatus struct {
	CheckoutID        string          `json:"checkout_id"`
	Provider          string          `json:"provider"`
	Status            string          `json:"status"`
	FulfillmentStatus string          `json:"fulfillment_status"`
	AmountCents       int             `json:"amount_cents"`
	Currency          string          `json:"currency"`
	InvoiceID         string          `json:"invoice_id"`
	Checkout          *CheckoutResult `json:"checkout"`
}
type RazorpayVerification struct {
	RazorpayOrderID   string `json:"razorpay_order_id"`
	RazorpayPaymentID string `json:"razorpay_payment_id"`
	RazorpaySignature string `json:"razorpay_signature"`
}
type CancelSubscriptionResult struct {
	Subscription   BillingSubscription `json:"subscription"`
	SubscriptionID string              `json:"subscription_id"`
	Status         string              `json:"status"`
	EffectiveAt    string              `json:"effective_at"`
}
type SubscriptionOptions struct {
	Subscription  json.RawMessage   `json:"subscription"`
	PendingChange json.RawMessage   `json:"pending_change"`
	EligiblePlans []json.RawMessage `json:"eligible_plans"`
	Checkout      json.RawMessage   `json:"checkout"`
}
type SubscriptionChangePreview struct {
	QuoteID                   string      `json:"quote_id"`
	TransitionDirection       string      `json:"transition_direction"`
	Timing                    string      `json:"timing"`
	Currency                  string      `json:"currency"`
	CreditAmountCents         json.Number `json:"credit_amount_cents"`
	DebitAmountCents          json.Number `json:"debit_amount_cents"`
	NetAmountCents            json.Number `json:"net_amount_cents"`
	AmountDueNowCents         json.Number `json:"amount_due_now_cents"`
	AmountDueAtEffectiveCents json.Number `json:"amount_due_at_effective_cents"`
	EffectiveAt               string      `json:"effective_at"`
	RenewalAt                 string      `json:"renewal_at"`
}
type SubscriptionTransitionParams struct {
	CustomerID          string `json:"customer_id"`
	SubscriptionID      string `json:"subscription_id"`
	Operation           string `json:"operation"`
	Timing              string `json:"timing,omitempty"`
	TargetPlanCode      string `json:"target_plan_code,omitempty"`
	BillingAnchor       string `json:"billing_anchor,omitempty"`
	ProrationBehavior   string `json:"proration_behavior,omitempty"`
	CreditAction        string `json:"credit_action,omitempty"`
	RefundMode          string `json:"refund_mode,omitempty"`
	FinalInvoiceAction  string `json:"final_invoice_action,omitempty"`
	ExpectedEffectiveAt string `json:"expected_effective_at,omitempty"`
	QuoteID             string `json:"quote_id,omitempty"`
}
type SubscriptionTransitionResult struct {
	SubscriptionTransition SubscriptionTransitionDetails `json:"subscription_transition"`
	Subscription           json.RawMessage               `json:"subscription"`
	Replayed               bool                          `json:"replayed"`
}
type WithdrawResult struct {
	Subscription                   json.RawMessage `json:"subscription"`
	WithdrawnPendingSubscriptionID string          `json:"withdrawn_pending_subscription_id"`
	Replayed                       bool            `json:"replayed"`
}

// SubscriptionTransitionDetails preserves quoted and applied settlement fields.
type SubscriptionTransitionDetails struct {
	ID                     string      `json:"id"`
	Replayed               bool        `json:"replayed"`
	Operation              string      `json:"operation"`
	Timing                 string      `json:"timing"`
	BillingAnchor          string      `json:"billing_anchor"`
	ProrationBehavior      string      `json:"proration_behavior"`
	CreditAction           string      `json:"credit_action"`
	RefundMode             string      `json:"refund_mode"`
	FinalInvoiceAction     string      `json:"final_invoice_action"`
	EffectiveAt            string      `json:"effective_at"`
	RenewalAt              string      `json:"renewal_at"`
	SubscriptionID         string      `json:"subscription_id"`
	ExternalSubscriptionID string      `json:"external_subscription_id"`
	PlanCode               string      `json:"plan_code"`
	Status                 string      `json:"status"`
	CreditNoteID           string      `json:"credit_note_id"`
	InvoiceID              string      `json:"invoice_id"`
	CreditAmountCents      json.Number `json:"credit_amount_cents"`
	RefundAmountCents      json.Number `json:"refund_amount_cents"`
	OffsetAmountCents      json.Number `json:"offset_amount_cents"`
	AmountDueCents         json.Number `json:"amount_due_cents"`
	Currency               string      `json:"currency"`
}
