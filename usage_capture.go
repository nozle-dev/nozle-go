package nozle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// CaptureOptions configures WrapHTTPClient. The customer for a request is, in order: the
// X-Nozle-Customer-Id request header (removed before the request is sent), the customer set on
// the request context with WithCustomer, CustomerIDFunc, then CustomerID. A request without a
// customer passes through and nothing is recorded.
type CaptureOptions struct {
	CustomerID     string
	CustomerIDFunc func(*http.Request) string
	// FeatureCode is the usage event's feature code. Default "llm_tokens".
	FeatureCode string
	// Feature is stored as the feature property; the X-Nozle-Feature request header wins.
	Feature       string
	CostMeterCode string
	// SkipStreamUsage stops asking OpenAI-compatible chat streams for usage
	// (stream_options.include_usage), for a server that rejects the field.
	SkipStreamUsage bool
	DeliveryTimeout time.Duration
	// OnError receives recording failures. The provider response is never affected.
	OnError func(error)
}

type customerContextKey struct{}

// WithCustomer returns a context that names the customer to bill for LLM requests made with it
// through a client from WrapHTTPClient.
func WithCustomer(ctx context.Context, customerID string) context.Context {
	return context.WithValue(ctx, customerContextKey{}, customerID)
}

const (
	captureCustomerHeader = "X-Nozle-Customer-Id"
	captureFeatureHeader  = "X-Nozle-Feature"
)

// WrapHTTPClient returns a copy of providerClient that records the token usage of every
// successful LLM response it carries. Usage is recognised by the shape of the response, so any
// OpenAI-compatible API (including the Responses API), Anthropic and Gemini work without naming
// the provider, for JSON responses and streams. Pass the result as the HTTP client of any LLM SDK
// or use it directly. Requests and responses pass through unchanged, apart from asking chat
// completion streams to include usage.
func WrapHTTPClient(providerClient *http.Client, nozleClient *Client, opts CaptureOptions) (*http.Client, error) {
	if nozleClient == nil {
		return nil, &ValidationError{"wrap_http_client", "Nozle client is required"}
	}
	if providerClient == nil {
		providerClient = &http.Client{}
	}
	wrapped := *providerClient
	base := providerClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	wrapped.Transport = &captureTransport{base: base, nozle: nozleClient, opts: opts}
	return &wrapped, nil
}

type captureTransport struct {
	base    http.RoundTripper
	nozle   *Client
	opts    CaptureOptions
	checked sync.Map // event codes already checked for a Feature
}

// SetupError reports a capture setup problem. Code is "feature_not_found" when no Feature has
// the event code, so the usage is stored but not billed.
type SetupError struct{ Code, Message string }

func (e *SetupError) Error() string { return "nozle: " + e.Message }

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	customer := strings.TrimSpace(req.Header.Get(captureCustomerHeader))
	feature := strings.TrimSpace(req.Header.Get(captureFeatureHeader))
	if req.Header.Get(captureCustomerHeader) != "" || req.Header.Get(captureFeatureHeader) != "" {
		req = req.Clone(req.Context())
		req.Header.Del(captureCustomerHeader)
		req.Header.Del(captureFeatureHeader)
	}
	if customer == "" {
		customer, _ = req.Context().Value(customerContextKey{}).(string)
	}
	if customer == "" && t.opts.CustomerIDFunc != nil {
		customer = t.opts.CustomerIDFunc(req)
	}
	if customer == "" {
		customer = t.opts.CustomerID
	}
	if feature == "" {
		feature = t.opts.Feature
	}
	if customer == "" || req.Method != http.MethodPost {
		return t.base.RoundTrip(req)
	}

	started := time.Now()
	request, model, err := t.prepare(req)
	if err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(request)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.Body == nil {
		return resp, err
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	stream := strings.Contains(contentType, "text/event-stream")
	if !stream && !strings.Contains(contentType, "json") {
		return resp, nil
	}
	reader, writer := io.Pipe()
	resp.Body = &observedBody{ReadCloser: resp.Body, writer: writer}
	call := capturedCall{customer: customer, feature: feature, model: model, host: req.URL.Host, started: started}
	go t.observe(reader, stream, call)
	return resp, nil
}

// prepare asks OpenAI-compatible chat streams to include usage; the request path is the API contract.
func (t *captureTransport) prepare(req *http.Request) (*http.Request, string, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return req, "", nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, "", err
	}
	_ = req.Body.Close()
	model := ""
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(body, &fields) == nil {
		_ = json.Unmarshal(fields["model"], &model)
		var stream bool
		_ = json.Unmarshal(fields["stream"], &stream)
		var messages []json.RawMessage
		chat := json.Unmarshal(fields["messages"], &messages) == nil && fields["messages"] != nil
		if stream && chat && !t.opts.SkipStreamUsage && strings.HasSuffix(req.URL.Path, "/chat/completions") {
			options := map[string]json.RawMessage{}
			_ = json.Unmarshal(fields["stream_options"], &options)
			options["include_usage"] = json.RawMessage("true")
			if encoded, e := json.Marshal(options); e == nil {
				fields["stream_options"] = encoded
				if encoded, e = json.Marshal(fields); e == nil {
					body = encoded
				}
			}
		}
	}
	request := req.Clone(req.Context())
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	return request, model, nil
}

type capturedCall struct {
	customer, feature, model, host string
	started                        time.Time
}

func (t *captureTransport) observe(reader *io.PipeReader, stream bool, call capturedCall) {
	defer reader.Close()
	var found detectedUsage
	var ok bool
	if stream {
		collector := newStreamCollector()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 2<<20)
		for scanner.Scan() && !collector.add(scanner.Bytes()) {
		}
		_, _ = io.Copy(io.Discard, reader)
		found, ok = collector.result()
	} else {
		var body map[string]any
		decoder := json.NewDecoder(reader)
		decoder.UseNumber()
		if decoder.Decode(&body) == nil {
			found, ok = detectUsage(body)
		}
		_, _ = io.Copy(io.Discard, reader)
	}
	if !ok {
		return
	}
	limit := t.opts.DeliveryTimeout
	if limit <= 0 {
		limit = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	model := found.model
	if model == "" {
		model = call.model
	}
	p := ProviderCapture{CustomerID: call.customer, FeatureCode: t.opts.FeatureCode, Feature: call.feature, CostMeterCode: t.opts.CostMeterCode, Model: model, RequestID: found.id, ProviderHost: call.host, Latency: time.Since(call.started)}
	if err := t.nozle.captureProvider(ctx, found.format, p, found.input, found.output, found.categories); err != nil && t.opts.OnError != nil {
		t.opts.OnError(err)
	}
	t.checkFeatureOnce(ctx, p.FeatureCode)
}

// checkFeatureOnce reports, once per event code, when no Feature has the code: the usage is then
// stored but never billed. Any other answer, including a key without read permission, is silent.
func (t *captureTransport) checkFeatureOnce(ctx context.Context, code string) {
	if code == "" {
		code = "llm_tokens"
	}
	if t.opts.OnError == nil {
		return
	}
	if _, seen := t.checked.LoadOrStore(code, true); seen {
		return
	}
	var apiErr *APIError
	err := t.nozle.request(ctx, "feature_lookup", http.MethodGet, true, "/api/v1/features/"+url.PathEscape(code), nil, nil, "")
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		t.opts.OnError(&SetupError{Code: "feature_not_found", Message: `no Feature has the code "` + code + `", so this usage is stored but not billed; create a metered Feature with that code: https://docs.nozle.dev/guides/ai/token-billing`})
	}
}

type detectedUsage struct {
	format, id, model string
	input, output     int
	categories        []tokenCategory
}

// detectUsage recognises token usage by the shape of a provider response, not by provider name:
// an OpenAI-style usage (prompt_tokens, or input_tokens from the Responses API), an Anthropic
// usage (a "message" object or cache token fields), or Gemini usageMetadata.
func detectUsage(body map[string]any) (detectedUsage, bool) {
	found := detectedUsage{id: firstString(body, "id", "responseId"), model: firstString(body, "model", "modelVersion")}
	if meta, ok := body["usageMetadata"].(map[string]any); ok {
		prompt := tokens(meta["promptTokenCount"])
		cached := min(tokens(meta["cachedContentTokenCount"]), prompt)
		output := tokens(meta["candidatesTokenCount"])
		reasoning := tokens(meta["thoughtsTokenCount"])
		found.format, found.input, found.output = "gemini", prompt, output+reasoning
		found.categories = []tokenCategory{{"input", prompt - cached}, {"cached_input", cached}, {"output", output}, {"reasoning", reasoning}}
		return found, true
	}
	usage, ok := body["usage"].(map[string]any)
	if !ok {
		return found, false
	}
	_, cacheRead := usage["cache_read_input_tokens"]
	_, cacheWrite := usage["cache_creation_input_tokens"]
	if body["type"] == "message" || cacheRead || cacheWrite {
		input := tokens(usage["input_tokens"])
		cached := tokens(usage["cache_read_input_tokens"])
		written := tokens(usage["cache_creation_input_tokens"])
		output := tokens(usage["output_tokens"])
		found.format, found.input, found.output = "anthropic", input+cached+written, output
		found.categories = []tokenCategory{{"input", input}, {"cached_input", cached}, {"cache_write", written}, {"output", output}}
		return found, true
	}
	known := false
	for _, key := range []string{"prompt_tokens", "completion_tokens", "input_tokens", "output_tokens"} {
		if _, has := usage[key]; has {
			known = true
		}
	}
	if !known {
		return found, false
	}
	input := tokens(firstPresent(usage, "prompt_tokens", "input_tokens"))
	output := tokens(firstPresent(usage, "completion_tokens", "output_tokens"))
	cached := min(tokens(nested(usage, "prompt_tokens_details", "input_tokens_details")["cached_tokens"]), input)
	reasoning := min(tokens(nested(usage, "completion_tokens_details", "output_tokens_details")["reasoning_tokens"]), output)
	found.format, found.input, found.output = "openai", input, output
	found.categories = []tokenCategory{{"input", input - cached}, {"cached_input", cached}, {"output", output - reasoning}, {"reasoning", reasoning}}
	return found, true
}

// streamCollector folds server-sent events into the final usage. OpenAI sends usage in the last
// chunk, the Responses API in response.completed, Anthropic in message_start and then a
// cumulative message_delta, and Gemini in every chunk with the totals last.
type streamCollector struct{ body map[string]any }

func newStreamCollector() *streamCollector { return &streamCollector{body: map[string]any{}} }

// add reads one stream line and reports whether the stream has ended ("[DONE]").
func (s *streamCollector) add(line []byte) bool {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return false
	}
	data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
	if bytes.Equal(data, []byte("[DONE]")) {
		return true
	}
	var event map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if len(data) == 0 || decoder.Decode(&event) != nil {
		return false
	}
	source := event
	if response, ok := event["response"].(map[string]any); ok {
		source = response
	} else if message, ok := event["message"].(map[string]any); ok {
		source = message
		s.body["type"] = "message"
	}
	for _, key := range []string{"id", "responseId", "model", "modelVersion"} {
		if value, ok := source[key]; ok && value != nil {
			s.body[key] = value
		}
	}
	usage, ok := source["usage"].(map[string]any)
	if !ok {
		usage, ok = event["usage"].(map[string]any)
	}
	if ok {
		merged, _ := s.body["usage"].(map[string]any)
		if merged == nil {
			merged = map[string]any{}
		}
		for key, value := range usage {
			if value != nil {
				merged[key] = value
			}
		}
		s.body["usage"] = merged
	}
	if meta, ok := source["usageMetadata"].(map[string]any); ok {
		s.body["usageMetadata"] = meta
	}
	return false
}

func (s *streamCollector) result() (detectedUsage, bool) { return detectUsage(s.body) }

func tokens(value any) int {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			f, ferr := v.Float64()
			if ferr != nil {
				return 0
			}
			n = int64(f)
		}
		return max(int(n), 0)
	case float64:
		return max(int(v), 0)
	case int:
		return max(v, 0)
	}
	return 0
}

func firstString(body map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := body[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func firstPresent(body map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := body[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func nested(body map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := body[key].(map[string]any); ok {
			return value
		}
	}
	return map[string]any{}
}
