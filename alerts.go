package nozle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// AlertTarget says whose alerts to manage: a subscription's usage alerts (SubscriptionID), or the
// balance alerts of one of a customer's wallets (CustomerID and WalletCode).
type AlertTarget struct {
	SubscriptionID         string
	CustomerID, WalletCode string
}

// AlertThreshold fires the alert when Value is crossed. Value is an amount in cents, units or
// credits, by alert type. At most one threshold per alert is Recurring.
type AlertThreshold struct {
	Code      *string     `json:"code,omitempty"`
	Value     json.Number `json:"value"`
	Recurring bool        `json:"recurring,omitempty"`
}

// Alert is a usage or wallet-balance alert. Nozle sends the alert.triggered webhook when one fires.
type Alert struct {
	NozleID                string           `json:"nozle_id"`
	AlertType              string           `json:"alert_type"`
	Code                   string           `json:"code"`
	Name                   *string          `json:"name"`
	ExternalSubscriptionID *string          `json:"external_subscription_id"`
	WalletCode             *string          `json:"wallet_code"`
	Direction              string           `json:"direction"`
	LastProcessedAt        *string          `json:"last_processed_at"`
	Thresholds             []AlertThreshold `json:"thresholds"`
	Feature                *struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"feature"`
	CreatedAt string `json:"created_at"`
}

// AlertInput creates or updates an alert. AlertType is only used on create; FeatureCode is
// required for the feature_* types. On update, empty fields are left as they are and Thresholds,
// when given, replace the existing ones.
type AlertInput struct {
	AlertType   string           `json:"alert_type,omitempty"`
	Code        string           `json:"code,omitempty"`
	Name        string           `json:"name,omitempty"`
	FeatureCode string           `json:"feature_code,omitempty"`
	Thresholds  []AlertThreshold `json:"thresholds,omitempty"`
}

// ListAlerts returns one page of alerts (page 0 is the first).
func (c *Client) ListAlerts(ctx context.Context, target AlertTarget, page int) ([]Alert, error) {
	path, err := c.alertPath("list_alerts", target, "")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var out struct {
		Alerts []Alert `json:"alerts"`
	}
	err = c.request(ctx, "list_alerts", http.MethodGet, true, query(path, q), nil, &out, "")
	return out.Alerts, err
}

// GetAlert returns one alert by code.
func (c *Client) GetAlert(ctx context.Context, target AlertTarget, code string) (Alert, error) {
	return c.alertCall(ctx, "get_alert", http.MethodGet, target, code, nil)
}

// CreateAlert creates an alert.
func (c *Client) CreateAlert(ctx context.Context, target AlertTarget, in AlertInput) (Alert, error) {
	if err := required("create_alert", "alert_type", in.AlertType); err != nil {
		return Alert{}, err
	}
	return c.alertCall(ctx, "create_alert", http.MethodPost, target, "", map[string]any{"alert": in})
}

// UpdateAlert changes an alert.
func (c *Client) UpdateAlert(ctx context.Context, target AlertTarget, code string, in AlertInput) (Alert, error) {
	in.AlertType = ""
	return c.alertCall(ctx, "update_alert", http.MethodPut, target, code, map[string]any{"alert": in})
}

// DeleteAlert removes an alert and returns it.
func (c *Client) DeleteAlert(ctx context.Context, target AlertTarget, code string) (Alert, error) {
	return c.alertCall(ctx, "delete_alert", http.MethodDelete, target, code, nil)
}

func (c *Client) alertCall(ctx context.Context, op, method string, target AlertTarget, code string, body any) (Alert, error) {
	if method != http.MethodPost {
		if err := required(op, "code", code); err != nil {
			return Alert{}, err
		}
	}
	path, err := c.alertPath(op, target, code)
	if err != nil {
		return Alert{}, err
	}
	var out struct {
		Alert Alert `json:"alert"`
	}
	err = c.request(ctx, op, method, true, path, body, &out, "")
	return out.Alert, err
}

func (c *Client) alertPath(op string, target AlertTarget, code string) (string, error) {
	if err := validateKey(c.apiKey, op); err != nil {
		return "", err
	}
	var path string
	if target.SubscriptionID != "" {
		path = "/api/v1/subscriptions/" + segment(target.SubscriptionID) + "/alerts"
	} else {
		if err := required(op, "customer_id", target.CustomerID); err != nil {
			return "", err
		}
		if err := required(op, "wallet_code", target.WalletCode); err != nil {
			return "", err
		}
		path = "/api/v1/customers/" + segment(target.CustomerID) + "/wallets/" + segment(target.WalletCode) + "/alerts"
	}
	if code != "" {
		path += "/" + segment(code)
	}
	return path, nil
}
