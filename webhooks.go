package nozle

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WebhookEvent is a verified webhook from Nozle.
type WebhookEvent struct {
	// ID is X-Nozle-Unique-Key: the same on every delivery attempt, so use it to skip duplicates.
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
// Code is missing_signature, unsupported_algorithm, missing_hmac_key, missing_webhook_secret,
// invalid_signature or invalid_payload.
type WebhookVerificationError struct {
	Code    string
	Message string
}

func (e *WebhookVerificationError) Error() string { return "nozle webhook: " + e.Message }

func webhookError(code, message string) error {
	return &WebhookVerificationError{Code: code, Message: message}
}

type webhookKeyCache struct {
	mu  sync.Mutex
	key *rsa.PublicKey
}

// defaultWebhookTolerance is how far webhook-timestamp may be from now for a standard webhook.
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

// VerifyWebhook checks the signature against the raw body and returns the event. Pass the
// body exactly as received. A standard webhook (X-Nozle-Signature-Algorithm "standard", or a
// webhook-signature header) is checked from webhook-id, webhook-timestamp and webhook-signature
// with the secrets given by WithWebhookSecrets; no network call is made. Otherwise
// X-Nozle-Signature is checked: hmac endpoints need the organization's hmacKey; jwt endpoints
// need none: the public key is read once from Core with the secret key and cached. A request
// that is not genuine returns a *WebhookVerificationError.
func (c *Client) VerifyWebhook(ctx context.Context, body []byte, header http.Header, hmacKey string, opts ...WebhookOption) (WebhookEvent, error) {
	signature := header.Get("X-Nozle-Signature")
	algorithm := header.Get("X-Nozle-Signature-Algorithm")
	if algorithm == "standard" || header.Get("webhook-signature") != "" {
		cfg := webhookConfig{tolerance: defaultWebhookTolerance, now: time.Now}
		for _, opt := range opts {
			opt(&cfg)
		}
		if err := verifyStandardWebhook(body, header, cfg); err != nil {
			return WebhookEvent{}, err
		}
		id := header.Get("X-Nozle-Unique-Key")
		if id == "" {
			id = header.Get("webhook-id")
		}
		return parseWebhook(body, id)
	}
	if signature == "" || algorithm == "" {
		return WebhookEvent{}, webhookError("missing_signature", "X-Nozle-Signature headers are missing")
	}
	switch algorithm {
	case "hmac":
		if hmacKey == "" {
			return WebhookEvent{}, webhookError("missing_hmac_key", "hmac webhooks need the organization's hmac key")
		}
		if !hmacMatches(body, signature, hmacKey) {
			return WebhookEvent{}, webhookError("invalid_signature", "webhook signature does not match")
		}
	case "jwt":
		key, err := c.webhookPublicKey(ctx, false)
		if err != nil {
			return WebhookEvent{}, err
		}
		valid := jwtMatches(body, signature, key)
		if !valid {
			// The key may have been rotated since it was cached: read it again once.
			if key, err = c.webhookPublicKey(ctx, true); err != nil {
				return WebhookEvent{}, err
			}
			valid = jwtMatches(body, signature, key)
		}
		if !valid {
			return WebhookEvent{}, webhookError("invalid_signature", "webhook signature does not match")
		}
	default:
		return WebhookEvent{}, webhookError("unsupported_algorithm", "unsupported signature algorithm "+algorithm)
	}
	return parseWebhook(body, header.Get("X-Nozle-Unique-Key"))
}

// WebhookHandler verifies each delivery and calls onEvent. It answers 200, 400 for a request
// that is not genuine, or 500 when onEvent returns an error, so Nozle tries again. Options are
// passed to VerifyWebhook.
func (c *Client) WebhookHandler(hmacKey string, onEvent func(context.Context, WebhookEvent) error, opts ...WebhookOption) http.Handler {
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
		event, err := c.VerifyWebhook(r.Context(), body, r.Header, hmacKey, opts...)
		var verification *WebhookVerificationError
		if errors.As(err, &verification) {
			writeCode(http.StatusBadRequest, verification.Code)
			return
		}
		if err != nil {
			writeCode(http.StatusInternalServerError, "public_key_unavailable")
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
		return webhookError("missing_webhook_secret", "standard webhooks need the endpoint secret (WithWebhookSecrets)")
	}
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return webhookError("invalid_signature", "webhook-timestamp is not a number")
	}
	age := cfg.now().Sub(time.Unix(seconds, 0))
	if age > cfg.tolerance || -age > cfg.tolerance {
		return webhookError("invalid_signature", "webhook-timestamp is outside the allowed tolerance")
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

func hmacMatches(body []byte, signature, key string) bool {
	received, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	return hmac.Equal(received, mac.Sum(nil))
}

// jwtMatches checks an RS256 JWT whose data claim is the body.
func jwtMatches(body []byte, token string, key *rsa.PublicKey) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	head, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	claims, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	signature, err3 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(head, &header) != nil || header.Alg != "RS256" {
		return false
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return false
	}
	var payload struct {
		Data *string `json:"data"`
	}
	if json.Unmarshal(claims, &payload) != nil || payload.Data == nil {
		return false
	}
	return hmac.Equal([]byte(*payload.Data), body)
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

func (c *Client) webhookPublicKey(ctx context.Context, refresh bool) (*rsa.PublicKey, error) {
	c.webhookKey.mu.Lock()
	defer c.webhookKey.mu.Unlock()
	if c.webhookKey.key != nil && !refresh {
		return c.webhookKey.key, nil
	}
	if err := validateKey(c.apiKey, "webhook_public_key"); err != nil {
		return nil, err
	}
	var out struct {
		Webhook struct {
			PublicKey string `json:"public_key"`
		} `json:"webhook"`
	}
	if err := c.request(ctx, "webhook_public_key", http.MethodGet, true, "/api/v1/webhooks/json_public_key", nil, &out, ""); err != nil {
		return nil, err
	}
	key, err := parseRSAPublicKey(out.Webhook.PublicKey)
	if err != nil {
		return nil, err
	}
	c.webhookKey.key = key
	return key, nil
}

// parseRSAPublicKey reads Core's Base64-encoded PEM: a PUBLIC KEY or an RSA PUBLIC KEY block.
func parseRSAPublicKey(encoded string) (*rsa.PublicKey, error) {
	text, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("nozle: webhook public key is not Base64: %w", err)
	}
	block, _ := pem.Decode(bytes.TrimSpace(text))
	if block == nil {
		return nil, errors.New("nozle: webhook public key is not PEM")
	}
	if block.Type == "RSA PUBLIC KEY" {
		return x509.ParsePKCS1PublicKey(block.Bytes)
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("nozle: webhook public key is not RSA")
	}
	return key, nil
}
