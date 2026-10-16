package nozle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const DefaultHost = "https://api.nozle.app"
const DefaultEngineURL = DefaultHost + "/engine"
const DefaultCoreURL = DefaultHost + "/core"

type Option func(*Client)

// WithHost sets the root URL of the Nozle API. Engine calls go to host+"/engine"
// and Core calls to host+"/core". WithBaseURL and WithEventsURL override either.
func WithHost(u string) Option { return func(c *Client) { c.host = u } }

// WithBaseURL overrides the Engine URL.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		c.baseURL = u
	}
}

// WithEventsURL overrides the Core billing and subscription discovery URL.
// Track and Meter use the Engine URL configured by WithBaseURL.
func WithEventsURL(u string) Option {
	return func(c *Client) { c.eventsURL = u }
}
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithMaxRetries sets how many extra attempts follow a network error, timeout, 429 or 5xx. The default is 0:
// no retries. Only calls that are safe to repeat are retried.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = max(0, n) } }

// WithRetryDelay sets the first retry delay (default 500ms). It doubles each retry up to 10 seconds, or the
// Retry-After header is used when the service sends one.
func WithRetryDelay(d time.Duration) Option { return func(c *Client) { c.retryDelay = d } }
func WithDebug(on bool) Option              { return func(c *Client) { c.debug = on } }
func WithHTTPClient(h *http.Client) Option  { return func(c *Client) { c.httpClient = h } }

type Client struct {
	maxRetries                       int
	retryDelay                       time.Duration
	host, baseURL, eventsURL, apiKey string
	debug                            bool
	timeout                          time.Duration
	httpClient                       *http.Client
	usageTimestamps                  usageTimestampMemo
	webhookKey                       webhookKeyCache
}

func New(apiKey string, opts ...Option) *Client {
	if strings.TrimSpace(apiKey) == "" {
		apiKey = os.Getenv("NOZLE_SECRET_KEY")
	}
	if strings.TrimSpace(apiKey) == "" {
		panic("nozle: apiKey must not be empty (pass it or set NOZLE_SECRET_KEY)")
	}
	c := &Client{host: DefaultHost, retryDelay: 500 * time.Millisecond, apiKey: apiKey, timeout: 10 * time.Second, httpClient: &http.Client{}}
	for _, o := range opts {
		o(c)
	}
	host := strings.TrimRight(c.host, "/")
	if c.baseURL == "" {
		c.baseURL = host + "/engine"
	}
	if c.eventsURL == "" {
		c.eventsURL = host + "/core"
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	c.eventsURL = strings.TrimRight(c.eventsURL, "/")
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	return c
}

type APIError struct {
	StatusCode                    int
	Method, Path, Operation, Body string
	Details                       any
	// RequestID is the X-Request-Id response header, when the service sent one. Quote it to support.
	RequestID string
	// RetryAfter is how long the service asked callers to wait, from the Retry-After header. Zero when absent.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("nozle: %s %s returned %d", e.Method, e.Path, e.StatusCode)
}

// Code returns the top-level error code of the response, for example
// "payment_provider_required" or "validation_errors". It is empty when the
// response had none.
func (e *APIError) Code() string {
	details, _ := e.Details.(map[string]any)
	code, _ := details["code"].(string)
	return code
}

// HasCode reports whether code is the top-level code or is listed in the
// response's error_details, where a 422 validation_errors response names
// specific codes such as "razorpay_requires_inr" or "invalid_checkout_signature".
func (e *APIError) HasCode(code string) bool {
	if code == "" {
		return false
	}
	if e.Code() == code {
		return true
	}
	details, _ := e.Details.(map[string]any)
	listed, _ := details["error_details"].(map[string]any)
	for _, value := range listed {
		if value == code {
			return true
		}
		if values, ok := value.([]any); ok {
			for _, item := range values {
				if item == code {
					return true
				}
			}
		}
	}
	return false
}
func AsAPIError(err error, target **APIError) bool { return errors.As(err, target) }

type ValidationError struct{ Operation, Message string }

func (e *ValidationError) Error() string { return "nozle: " + e.Operation + ": " + e.Message }

type AuthenticationError struct{ ValidationError }
type TransportError struct {
	Operation string
	Err       error
}

func (e *TransportError) Error() string {
	return "nozle: " + e.Operation + ": transport failed: " + e.Err.Error()
}
func (e *TransportError) Unwrap() error { return e.Err }
func validateKey(key, op string) error {
	if !strings.HasPrefix(key, "sk_") {
		return &AuthenticationError{ValidationError{op, "secret sk_ key required"}}
	}
	return nil
}
func required(op, name, v string) error {
	if strings.TrimSpace(v) == "" {
		return &ValidationError{op, name + " is required"}
	}
	return nil
}
func idem(op, key string) error {
	if err := required(op, "idempotency key", key); err != nil {
		return err
	}
	if len([]byte(key)) > 255 {
		return &ValidationError{op, "idempotency key exceeds 255 bytes"}
	}
	return nil
}
func segment(s string) string       { return url.PathEscape(s) }
func customerPath(id string) string { return "/api/v1/customers/" + segment(id) }
func entityPath(customer, entity string) string {
	return customerPath(customer) + "/entities/" + segment(entity)
}
func query(path string, params url.Values) string {
	if len(params) == 0 {
		return path
	}
	return path + "?" + params.Encode()
}
func (c *Client) endpoint(core bool) string {
	if core {
		return c.eventsURL
	}
	return c.baseURL
}

// requestOps that are safe to repeat although they carry no Idempotency-Key: events and cost events
// are idempotent by their own id, a customer upsert converges, and checks and previews only read.
var repeatableOps = map[string]bool{
	"track": true, "meter": true, "cost_events.track": true, "customers.upsert": true,
	"usage.check": true, "preview_subscription_transition": true, "preview_subscription_change": true,
	"invoice_download_url": true,
}

// request sends one call. With WithMaxRetries set it retries network errors, timeouts, 429 and 5xx,
// but only a call that is safe to repeat: a read, a call with an idempotency key, or a repeatable op.
func (c *Client) request(ctx context.Context, op, method string, core bool, path string, body, out any, key string) error {
	retries := 0
	if method == http.MethodGet || method == http.MethodHead || key != "" || repeatableOps[op] {
		retries = c.maxRetries
	}
	for attempt := 0; ; attempt++ {
		err := c.requestOnce(ctx, op, method, core, path, body, out, key)
		if err == nil || attempt >= retries || ctx.Err() != nil {
			return err
		}
		wait, retryable := c.retryWait(err, attempt)
		if !retryable {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
	}
}

func (c *Client) retryWait(err error, attempt int) (time.Duration, bool) {
	var api *APIError
	var transport *TransportError
	switch {
	case errors.As(err, &api) && (api.StatusCode == http.StatusTooManyRequests || api.StatusCode >= 500):
		if api.RetryAfter > 0 {
			return min(api.RetryAfter, 30*time.Second), true
		}
	case errors.As(err, &transport):
	default:
		return 0, false
	}
	base := min(c.retryDelay<<attempt, 10*time.Second)
	return base/2 + time.Duration(rand.Int63n(int64(base/2)+1)), true
}

func (c *Client) requestOnce(ctx context.Context, op, method string, core bool, path string, body, out any, key string) error {
	if op != "plans" {
		if err := validateKey(c.apiKey, op); err != nil {
			return err
		}
	}
	if key != "" {
		if err := idem(op, key); err != nil {
			return err
		}
	}
	req, err := newJSONRequest(ctx, method, c.endpoint(core)+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if c.debug {
		log.Printf("[nozle] %s %s", method, req.URL.Redacted())
	}
	requestCtx := req.Context()
	cancel := func() {}
	if c.timeout > 0 {
		requestCtx, cancel = context.WithTimeout(requestCtx, c.timeout)
		req = req.WithContext(requestCtx)
	}
	defer cancel()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &TransportError{op, err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		safe := strings.ReplaceAll(string(b), c.apiKey, "[REDACTED]")
		var details any
		if json.Unmarshal([]byte(safe), &details) == nil {
			details = redact(details)
			if redacted, marshalErr := json.Marshal(details); marshalErr == nil {
				safe = string(redacted)
			}
		}
		return &APIError{
			StatusCode: resp.StatusCode, Method: method, Path: req.URL.Path, Operation: op, Body: safe, Details: details,
			RequestID: resp.Header.Get("X-Request-Id"), RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if out == nil || resp.StatusCode == 204 {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	if err = json.Unmarshal(b, out); err != nil {
		return &APIError{StatusCode: resp.StatusCode, Method: method, Path: req.URL.Path, Operation: op, Body: "invalid JSON success response"}
	}
	return nil
}
func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, y := range x {
			n := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
			if n == "authorization" || n == "api_key" || n == "secret" || n == "razorpay_signature" || strings.HasSuffix(n, "_token") || strings.HasSuffix(n, "_secret") || strings.HasSuffix(n, "_key") {
				x[k] = "[REDACTED]"
			} else {
				x[k] = redact(y)
			}
		}
	case []any:
		for i := range x {
			x[i] = redact(x[i])
		}
	}
	return v
}

// parseRetryAfter reads a Retry-After value that is a number of seconds or an HTTP date.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds * float64(time.Second))
	}
	if moment, err := http.ParseTime(value); err == nil {
		if wait := time.Until(moment); wait > 0 {
			return wait
		}
	}
	return 0
}
