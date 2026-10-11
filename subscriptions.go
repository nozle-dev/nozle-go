package nozle

import (
	"context"
	"net/url"
)

// Checkout creates a payment-aware checkout. QuoteID requires SubscriptionID.
func (c *Client) Checkout(ctx context.Context, p CheckoutParams) (CheckoutResult, error) {
	var out CheckoutResult
	if err := validateKey(c.apiKey, "checkout"); err != nil {
		return out, err
	}
	customer := p.CustomerID
	if customer == "" {
		customer = p.OrganizationID
	}
	if err := required("checkout", "customer_id", customer); err != nil {
		return out, err
	}
	if err := required("checkout", "plan_code", p.PlanCode); err != nil {
		return out, err
	}
	if p.QuoteID != "" && p.SubscriptionID == "" {
		return out, &ValidationError{"checkout", "QuoteID requires SubscriptionID"}
	}
	body := map[string]any{"customer_id": customer, "plan_code": p.PlanCode}
	if p.ReturnURL != "" {
		body["return_url"] = p.ReturnURL
	}
	if p.SubscriptionID != "" {
		body["subscription_id"] = p.SubscriptionID
	}
	if p.QuoteID != "" {
		body["quote_id"] = p.QuoteID
	}
	if p.ExternalEntityID != "" {
		body["external_entity_id"] = p.ExternalEntityID
	}
	if p.RegisterMandate != nil {
		body["register_mandate"] = *p.RegisterMandate
	}
	err := c.request(ctx, "checkout", "POST", false, "/api/v1/checkout", body, &out, p.IdempotencyKey)
	return out, err
}
func (c *Client) CheckoutInvoice(ctx context.Context, invoiceID, key string) (CheckoutResult, error) {
	var out CheckoutResult
	if err := validateKey(c.apiKey, "checkout_invoice"); err != nil {
		return out, err
	}
	if err := required("checkout_invoice", "invoice_id", invoiceID); err != nil {
		return out, err
	}
	err := c.request(ctx, "checkout_invoice", "POST", false, "/api/v1/checkout", map[string]any{"invoice_id": invoiceID}, &out, key)
	return out, err
}
func (c *Client) CheckoutStatus(ctx context.Context, checkoutID string, scope CheckoutScope) (CheckoutStatus, error) {
	var out CheckoutStatus
	if err := validateKey(c.apiKey, "checkout_status"); err != nil {
		return out, err
	}
	q, err := scope.values()
	if err != nil {
		return out, err
	}
	err = c.request(ctx, "checkout_status", "GET", false, query("/api/v1/checkout/"+segment(checkoutID), q), nil, &out, "")
	return out, err
}
func (c *Client) VerifyCheckout(ctx context.Context, checkoutID string, v RazorpayVerification, scope CheckoutScope) (CheckoutStatus, error) {
	var out CheckoutStatus
	q, err := scope.values()
	if err != nil {
		return out, err
	}
	if err := required("verify_checkout", "razorpay_signature", v.RazorpaySignature); err != nil {
		return out, err
	}
	err = c.request(ctx, "verify_checkout", "POST", false, query("/api/v1/checkout/"+segment(checkoutID)+"/verify", q), v, &out, "")
	return out, err
}
func (s CheckoutScope) values() (url.Values, error) {
	if s.CustomerID == "" && s.SubscriptionID == "" {
		return nil, nil
	}
	if s.CustomerID == "" || s.SubscriptionID == "" {
		return nil, &ValidationError{"checkout scope", "both customer and subscription IDs required"}
	}
	return url.Values{"customer_id": {s.CustomerID}, "subscription_id": {s.SubscriptionID}}, nil
}
func (c *Client) Subscribe(ctx context.Context, p SubscribeParams) (SubscribeResult, error) {
	var out SubscribeResult
	if err := validateKey(c.apiKey, "subscribe"); err != nil {
		return out, err
	}
	err := c.request(ctx, "subscribe", "POST", false, "/api/v1/subscribe", map[string]any{"customer_id": p.CustomerID, "plan_code": p.PlanCode}, &out, "")
	if err == nil && out.Subscription.ExternalID != "" {
		out.SubscriptionID = out.Subscription.ExternalID
		out.Status = out.Subscription.Status
	}
	return out, err
}
func (c *Client) CancelSubscription(ctx context.Context, customer, subscription, policy string) (CancelSubscriptionResult, error) {
	var out CancelSubscriptionResult
	if err := validateKey(c.apiKey, "cancel_subscription"); err != nil {
		return out, err
	}
	if policy == "" {
		policy = "end_of_period"
	}
	q := url.Values{"customer_id": {customer}, "cancellation_policy": {policy}}
	err := c.request(ctx, "cancel_subscription", "DELETE", false, query("/api/v1/subscriptions/"+segment(subscription), q), nil, &out, "")
	if err == nil && out.Subscription.ExternalID != "" {
		out.SubscriptionID = out.Subscription.ExternalID
		out.Status = out.Subscription.Status
		out.EffectiveAt = out.Subscription.EndingAt
	}
	return out, err
}
func (c *Client) SubscriptionOptions(ctx context.Context, customer, subscription string) (SubscriptionOptions, error) {
	var out SubscriptionOptions
	q, err := (CheckoutScope{customer, subscription}).values()
	if err != nil {
		return out, err
	}
	err = c.request(ctx, "subscription_options", "GET", false, query("/api/v1/subscriptions/options", q), nil, &out, "")
	return out, err
}
func (c *Client) PreviewSubscriptionChange(ctx context.Context, customer, subscription, plan string) (SubscriptionChangePreview, error) {
	var out SubscriptionChangePreview
	err := c.request(ctx, "preview_subscription_change", "POST", false, "/api/v1/subscriptions/preview", map[string]string{"customer_id": customer, "subscription_id": subscription, "plan_code": plan}, &out, "")
	return out, err
}
func (c *Client) PreviewSubscriptionTransition(ctx context.Context, p SubscriptionTransitionParams) (SubscriptionTransitionResult, error) {
	var out SubscriptionTransitionResult
	if err := p.validate(); err != nil {
		return out, err
	}
	err := c.request(ctx, "preview_subscription_transition", "POST", false, "/api/v1/subscriptions/transitions/preview", p, &out, "")
	return out, err
}
func (c *Client) ApplySubscriptionTransition(ctx context.Context, p SubscriptionTransitionParams, key string) (SubscriptionTransitionResult, error) {
	var out SubscriptionTransitionResult
	if err := p.validate(); err != nil {
		return out, err
	}
	if err := idem("apply_subscription_transition", key); err != nil {
		return out, err
	}
	err := c.request(ctx, "apply_subscription_transition", "POST", false, "/api/v1/subscriptions/transitions", p, &out, key)
	return out, err
}
func (c *Client) WithdrawPendingSubscriptionChange(ctx context.Context, customer, subscription, pending, key string) (WithdrawResult, error) {
	var out WithdrawResult
	if err := idem("withdraw_pending_subscription_change", key); err != nil {
		return out, err
	}
	err := c.request(ctx, "withdraw_pending_subscription_change", "POST", false, "/api/v1/subscriptions/transitions/withdraw", map[string]string{"customer_id": customer, "subscription_id": subscription, "pending_subscription_id": pending}, &out, key)
	return out, err
}
func (p SubscriptionTransitionParams) validate() error {
	if p.Operation != "cancel" && p.Operation != "downgrade" && p.Operation != "uncancel" {
		return &ValidationError{"transition", "invalid operation"}
	}
	if p.Operation == "downgrade" && p.TargetPlanCode == "" {
		return &ValidationError{"transition", "target plan required"}
	}
	if p.Operation != "downgrade" && p.TargetPlanCode != "" {
		return &ValidationError{"transition", "target plan forbidden"}
	}
	if p.QuoteID != "" && p.Operation != "downgrade" {
		return &ValidationError{"transition", "quote requires downgrade"}
	}
	if p.ExpectedEffectiveAt != "" && (p.Operation != "cancel" || p.Timing != "end_of_period") {
		return &ValidationError{"transition", "expected effective date requires end-of-period cancellation"}
	}
	return nil
}
