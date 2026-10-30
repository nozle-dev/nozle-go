package nozle

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// WebhookEvent is a verified webhook from Nozle.
type WebhookEvent struct {
	// ID is webhook-id: the same on every delivery attempt, so use it to skip duplicates.
	ID string
	// Type is webhook_type, for example "subscription.started".
	Type string
	// ObjectType is object_type, for example "subscription".
	ObjectType     string
	OrganizationID string
	// Object is the object the webhook is about: Payload[ObjectType]. Decode it into your own type.
	Object json.RawMessage
	// Payload is the whole body, by top-level key.
	Payload map[string]json.RawMessage
}

// WebhookVerificationError means the request is not a genuine Nozle webhook. Answer it with 400.
// Code is missing_signature, missing_secret, invalid_timestamp, invalid_signature or invalid_payload,
// the same codes as the Node and Python SDKs.
type WebhookVerificationError struct {
	Code    string
	Message string
}

func (e *WebhookVerificationError) Error() string { return "nozle webhook: " + e.Message }

func webhookError(code, message string) error {
	return &WebhookVerificationError{Code: code, Message: message}
}

// defaultWebhookTolerance is how far webhook-timestamp may be from now.
const defaultWebhookTolerance = 300 * time.Second

type webhookConfig struct {
	secrets   []string
	tolerance time.Duration
	now       func() time.Time
}

// WebhookOption configures VerifyWebhook and WebhookHandler.
type WebhookOption func(*webhookConfig)

// WithWebhookSecrets sets the endpoint secrets ("whsec_...") for standard webhooks. Pass the
// current secret and, while you rotate, the previous one too: a delivery is accepted when any
// signature in it matches any secret.
func WithWebhookSecrets(secrets ...string) WebhookOption {
	return func(o *webhookConfig) { o.secrets = append(o.secrets, secrets...) }
}

// WithWebhookTolerance sets how far webhook-timestamp may differ from now (default 300 s).
func WithWebhookTolerance(d time.Duration) WebhookOption {
	return func(o *webhookConfig) { o.tolerance = d }
}

// WithWebhookClock replaces time.Now when checking webhook-timestamp, for tests.
func WithWebhookClock(now func() time.Time) WebhookOption {
	return func(o *webhookConfig) { o.now = now }
}

// VerifyWebhook checks webhook-id, webhook-timestamp and webhook-signature against the raw body
// and returns the event. Pass the body exactly as received and the endpoint secrets with
// WithWebhookSecrets. No network call is made. A request that is not genuine returns a
// *WebhookVerificationError.
func (c *Client) VerifyWebhook(ctx context.Context, body []byte, header http.Header, opts ...WebhookOption) (WebhookEvent, error) {
	cfg := webhookConfig{tolerance: defaultWebhookTolerance, now: time.Now}
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := verifyStandardWebhook(body, header, cfg); err != nil {
		return WebhookEvent{}, err
	}
	return parseWebhook(body, header.Get("webhook-id"))
}

// WebhookHandler verifies each delivery and calls onEvent. It answers 200, 400 for a request
// that is not genuine, or 500 when onEvent returns an error, so Nozle tries again. Options are
// passed to VerifyWebhook.
func (c *Client) WebhookHandler(onEvent func(context.Context, WebhookEvent) error, opts ...WebhookOption) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeCode := func(status int, code string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
		}
		if r.Method != http.MethodPost {
			writeCode(http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
		if err != nil {
			writeCode(http.StatusBadRequest, "invalid_payload")
			return
		}
		event, err := c.VerifyWebhook(r.Context(), body, r.Header, opts...)
		var verification *WebhookVerificationError
		if errors.As(err, &verification) {
			writeCode(http.StatusBadRequest, verification.Code)
			return
		}
		if err := onEvent(r.Context(), event); err != nil {
			writeCode(http.StatusInternalServerError, "handler_failed")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"received":true}`))
	})
}

// verifyStandardWebhook checks a Standard Webhooks delivery: HMAC-SHA256 over "id.timestamp.body"
// with the base64-decoded part of each secret after "whsec_", compared in constant time.
func verifyStandardWebhook(body []byte, header http.Header, cfg webhookConfig) error {
	id, stamp, signatures := header.Get("webhook-id"), header.Get("webhook-timestamp"), header.Get("webhook-signature")
	if id == "" || stamp == "" || signatures == "" {
		return webhookError("missing_signature", "webhook-id, webhook-timestamp or webhook-signature header is missing")
	}
	if len(cfg.secrets) == 0 {
		return webhookError("missing_secret", "standard webhooks need the endpoint secret (WithWebhookSecrets)")
	}
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return webhookError("invalid_timestamp", "webhook-timestamp is not a number")
	}
	age := cfg.now().Sub(time.Unix(seconds, 0))
	if age > cfg.tolerance || -age > cfg.tolerance {
		return webhookError("invalid_timestamp", "webhook-timestamp is outside the allowed tolerance")
	}
	signed := []byte(id + "." + stamp + ".")
	signed = append(signed, body...)
	matched := false
	for _, secret := range cfg.secrets {
		key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
		if err != nil {
			continue
		}
		mac := hmac.New(sha256.New, key)
		mac.Write(signed)
		expected := mac.Sum(nil)
		for _, entry := range strings.Fields(signatures) {
			version, value, ok := strings.Cut(entry, ",")
			if !ok || version != "v1" {
				continue
			}
			received, err := base64.StdEncoding.DecodeString(value)
			if err == nil && hmac.Equal(received, expected) {
				matched = true
			}
		}
	}
	if !matched {
		return webhookError("invalid_signature", "webhook signature does not match")
	}
	return nil
}

func parseWebhook(body []byte, id string) (WebhookEvent, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return WebhookEvent{}, webhookError("invalid_payload", "webhook body is not JSON")
	}
	text := func(key string) string {
		var value string
		_ = json.Unmarshal(payload[key], &value)
		return value
	}
	event := WebhookEvent{ID: id, Type: text("webhook_type"), ObjectType: text("object_type"), OrganizationID: text("organization_id"), Payload: payload}
	if event.Type == "" {
		return WebhookEvent{}, webhookError("invalid_payload", "webhook body has no webhook_type")
	}
	if event.ObjectType != "" {
		event.Object = payload[event.ObjectType]
	}
	return event, nil
}
