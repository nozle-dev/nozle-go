package nozle

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"sync"
)

// ChargeUsage is one charge's usage in a billing period.
type ChargeUsage struct {
	Units          string `json:"units"`
	EventsCount    int64  `json:"events_count"`
	AmountCents    int64  `json:"amount_cents"`
	AmountCurrency string `json:"amount_currency"`
	Charge         struct {
		NozleID            string  `json:"nozle_id"`
		ChargeModel        string  `json:"charge_model"`
		InvoiceDisplayName *string `json:"invoice_display_name"`
	} `json:"charge"`
	Feature struct {
		NozleID string `json:"nozle_id"`
		Code    string `json:"code"`
		Name    string `json:"name"`
	} `json:"feature"`
}

// CustomerUsage is a customer's usage for one billing period: Core current_usage or one
// past_usage item.
type CustomerUsage struct {
	FromDatetime     string        `json:"from_datetime"`
	ToDatetime       string        `json:"to_datetime"`
	IssuingDate      string        `json:"issuing_date"`
	InvoiceID        *string       `json:"invoice_id"`
	Currency         string        `json:"currency"`
	AmountCents      int64         `json:"amount_cents"`
	TaxesAmountCents int64         `json:"taxes_amount_cents"`
	TotalAmountCents int64         `json:"total_amount_cents"`
	ChargesUsage     []ChargeUsage `json:"charges_usage"`
}

// CurrentUsage returns the customer's usage in the current billing period. With an empty
// subscriptionID the customer's one active subscription is used.
func (c *Client) CurrentUsage(ctx context.Context, customerID, subscriptionID string) (CustomerUsage, error) {
	var out struct {
		Usage CustomerUsage `json:"customer_usage"`
	}
	err := c.usageRead(ctx, "current_usage", customerID, subscriptionID, nil, &out)
	return out.Usage, err
}

// PastUsage returns usage of past billing periods, newest first. periods limits how many; 0 uses
// Core's default.
func (c *Client) PastUsage(ctx context.Context, customerID, subscriptionID string, periods int) ([]CustomerUsage, error) {
	var out struct {
		Periods []CustomerUsage `json:"usage_periods"`
	}
	extra := url.Values{}
	if periods > 0 {
		extra.Set("periods_count", strconv.Itoa(periods))
	}
	err := c.usageRead(ctx, "past_usage", customerID, subscriptionID, extra, &out)
	return out.Periods, err
}

func (c *Client) usageRead(ctx context.Context, route, customerID, subscriptionID string, extra url.Values, out any) error {
	if err := validateKey(c.apiKey, route); err != nil {
		return err
	}
	if err := required(route, "customer_id", customerID); err != nil {
		return err
	}
	sub, err := c.subscriptionOrActive(ctx, route, customerID, subscriptionID)
	if err != nil {
		return err
	}
	q := url.Values{"external_subscription_id": {sub}}
	for k, v := range extra {
		q[k] = v
	}
	return c.request(ctx, route, http.MethodGet, true, query("/api/v1/customers/"+segment(customerID)+"/"+route, q), nil, out, "")
}

// subscriptionOrActive returns subscriptionID, or the customer's one active subscription.
func (c *Client) subscriptionOrActive(ctx context.Context, op, customerID, subscriptionID string) (string, error) {
	if subscriptionID != "" {
		return subscriptionID, nil
	}
	if cached := c.subscriptions.get(customerID); cached != "" {
		return cached, nil
	}
	var found struct {
		Subscriptions []struct {
			ExternalID string `json:"external_id"`
		} `json:"subscriptions"`
	}
	q := url.Values{"external_customer_id": {customerID}, "status[]": {"active"}}
	if err := c.request(ctx, op+".subscription_lookup", http.MethodGet, true, query("/api/v1/subscriptions", q), nil, &found, ""); err != nil {
		return "", err
	}
	if len(found.Subscriptions) != 1 {
		// An *APIError with the codes the Engine uses for the same case, so callers match one error type.
		code, message := "active_subscription_not_found", "no active subscription for the customer; supply SubscriptionID"
		if len(found.Subscriptions) > 1 {
			code, message = "active_subscription_ambiguous", "the customer has several active subscriptions; supply SubscriptionID"
		}
		return "", &APIError{
			StatusCode: http.StatusUnprocessableEntity, Method: http.MethodGet, Path: "/api/v1/subscriptions",
			Operation: op + ".subscription_lookup", Body: message,
			Details: map[string]any{"code": code, "error": message},
		}
	}
	c.subscriptions.set(customerID, found.Subscriptions[0].ExternalID)
	return found.Subscriptions[0].ExternalID, nil
}

// subscriptionCache remembers each customer's single active subscription so Track does not look
// it up on every call. Calls that can change the active subscription forget the entry.
type subscriptionCache struct {
	mu    sync.Mutex
	known map[string]string
}

func (s *subscriptionCache) get(customerID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.known[customerID]
}

func (s *subscriptionCache) set(customerID, subscriptionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.known == nil {
		s.known = map[string]string{}
	}
	s.known[customerID] = subscriptionID
}

// forget drops one customer, or every customer when customerID is empty.
func (s *subscriptionCache) forget(customerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if customerID == "" {
		s.known = nil
		return
	}
	delete(s.known, customerID)
}
