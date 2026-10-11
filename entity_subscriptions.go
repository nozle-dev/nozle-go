package nozle

import (
	"context"
	"encoding/json"
)

func entitySubscriptionPath(customer, entity string) string {
	return entityPath(customer, entity) + "/subscription"
}
func (c *Client) EnsureEntitySubscription(ctx context.Context, customer, entity string) (EntitySubscription, error) {
	var out struct {
		EntitySubscription EntitySubscription `json:"entity_subscription"`
	}
	if err := validateSecret(c, "entity_subscriptions.ensure"); err != nil {
		return out.EntitySubscription, err
	}
	err := c.request(ctx, "entity_subscriptions.ensure", "PUT", true, entitySubscriptionPath(customer, entity), nil, &out, "")
	return out.EntitySubscription, err
}
func (c *Client) GetEntitySubscription(ctx context.Context, customer, entity string) (EntitySubscription, error) {
	var out struct {
		EntitySubscription EntitySubscription `json:"entity_subscription"`
	}
	err := c.request(ctx, "entity_subscriptions.get", "GET", true, entitySubscriptionPath(customer, entity), nil, &out, "")
	return out.EntitySubscription, err
}
func (c *Client) ListEntitySubscriptions(ctx context.Context, customer string) (EntitySubscriptionList, error) {
	var out EntitySubscriptionList
	err := c.request(ctx, "entity_subscriptions.list", "GET", true, customerPath(customer)+"/entity-subscriptions", nil, &out, "")
	return out, err
}
func (c *Client) entityChange(ctx context.Context, action, customer, entity string, p EntitySubscriptionChange) (CheckoutResult, error) {
	var out CheckoutResult
	if err := validateSecret(c, "entity_subscriptions."+action); err != nil {
		return out, err
	}
	if err := required("entity_subscriptions."+action, "plan_code", p.PlanCode); err != nil {
		return out, err
	}
	if p.BillingTime != "" && p.BillingTime != "calendar" && p.BillingTime != "anniversary" {
		return out, &ValidationError{"entity subscription", "invalid billing time"}
	}
	body := map[string]any{"plan_code": p.PlanCode, "return_url": p.ReturnURL}
	if action == "checkout" {
		body["billing_time"] = p.BillingTime
	}
	if p.RegisterMandate != nil {
		body["register_mandate"] = *p.RegisterMandate
	}
	err := c.request(ctx, "entity_subscriptions."+action, "POST", true, entitySubscriptionPath(customer, entity)+"/"+action, body, &out, p.IdempotencyKey)
	return out, err
}
func (c *Client) CheckoutEntitySubscription(ctx context.Context, customer, entity string, p EntitySubscriptionChange) (CheckoutResult, error) {
	return c.entityChange(ctx, "checkout", customer, entity, p)
}
func (c *Client) ChangeEntitySubscriptionPlan(ctx context.Context, customer, entity string, p EntitySubscriptionChange) (CheckoutResult, error) {
	return c.entityChange(ctx, "change-plan", customer, entity, p)
}
func (c *Client) CheckoutEntitySubscriptions(ctx context.Context, customer string, items []EntitySubscriptionCheckoutItem, returnURL, billingTime, key string) (json.RawMessage, error) {
	var out struct {
		EntitySubscriptionCheckout json.RawMessage `json:"entity_subscription_checkout"`
	}
	if err := idem("entity_subscriptions.checkout_many", key); err != nil {
		return nil, err
	}
	if len(items) < 1 || len(items) > 100 {
		return nil, &ValidationError{"entity_subscriptions.checkout_many", "requires 1-100 items"}
	}
	body := map[string]any{"entity_subscription_checkout": map[string]any{"items": items, "return_url": returnURL, "billing_time": billingTime}}
	err := c.request(ctx, "entity_subscriptions.checkout_many", "POST", true, customerPath(customer)+"/entity-subscriptions/checkout", body, &out, key)
	return out.EntitySubscriptionCheckout, err
}
func (c *Client) CancelEntitySubscription(ctx context.Context, customer, entity string, p EntitySubscriptionCancel) (json.RawMessage, error) {
	var out json.RawMessage
	if err := idem("entity_subscriptions.cancel", p.IdempotencyKey); err != nil {
		return nil, err
	}
	err := c.request(ctx, "entity_subscriptions.cancel", "POST", true, entitySubscriptionPath(customer, entity)+"/cancel", p, &out, p.IdempotencyKey)
	return out, err
}
func (c *Client) RemoveEntitySubscription(ctx context.Context, customer, entity string) error {
	return c.request(ctx, "entity_subscriptions.remove", "DELETE", true, entitySubscriptionPath(customer, entity), nil, nil, "")
}
