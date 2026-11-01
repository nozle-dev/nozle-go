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
	// OnError receives recording failures. The provider response is never affected. Without it failures are logged
	// through slog.Default().
	OnError func(error)
	// Buffer, when set, queues each usage event in this event buffer instead of sending it at once, so Flush and
	// Close deliver it. The event is queued before the response body's last Read returns. Cost Events are still
	// sent directly. The buffer's OnError reports what it does not deliver.
	Buffer *EventBuffer
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
	request, model, inputBytes, err := t.prepare(req)
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
	body := newObservedBody(resp.Body, writer, t.opts.Buffer != nil)
	resp.Body = body
	call := capturedCall{customer: customer, feature: feature, model: model, host: req.URL.Host, started: started, inputBytes: inputBytes}
	go t.observe(reader, body.markQueued, stream, call)
	return resp, nil
}

// prepare asks OpenAI-compatible chat streams to include usage; the request path is the API contract.
func (t *captureTransport) prepare(req *http.Request) (*http.Request, string, int, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return req, "", 0, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, "", 0, err
	}
	_ = req.Body.Close()
	model := ""
	inputBytes := 0
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(body, &fields) == nil {
		inputBytes = requestTextBytes(fields)
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
	return request, model, inputBytes, nil
}

type capturedCall struct {
	customer, feature, model, host string
	started                        time.Time
	inputBytes                     int // UTF-8 size of the request message text, for the estimate of an aborted stream
}

// requestTextBytes is the UTF-8 size of the message text in a request body: chat messages, Anthropic system, Responses
// input, Gemini contents.
func requestTextBytes(fields map[string]json.RawMessage) int {
	total := 0
	for _, name := range []string{"messages", "system", "input", "contents"} {
		var value any
		if raw, ok := fields[name]; ok && json.Unmarshal(raw, &value) == nil {
			total += textBytes(value, 0)
		}
	}
	return total
}

func textBytes(value any, depth int) int {
	switch v := value.(type) {
	case string:
		return len(v)
	case []any:
		total := 0
		for _, item := range v {
			total += textBytes(item, depth+1)
		}
		return total
	case map[string]any:
		if depth > 6 {
			return 0
		}
		return textBytes(v["text"], depth+1) + textBytes(v["content"], depth+1) + textBytes(v["parts"], depth+1) + textBytes(v["input"], depth+1)
	}
	return 0
}

// readObserved reads the observed copy of a response body and returns its usage. A stream that ended before the
// provider's final usage marker (the body was closed early, the connection dropped, the stream was cut) is reported
// as aborted: its partial usage, or an estimate from the delivered text when the provider sent none.
func readObserved(reader io.Reader, stream bool, inputBytes int) (detectedUsage, bool) {
	if !stream {
		var body map[string]any
		decoder := json.NewDecoder(reader)
		decoder.UseNumber()
		var found detectedUsage
		var ok bool
		if decoder.Decode(&body) == nil {
			found, ok = detectUsage(body)
		}
		_, _ = io.Copy(io.Discard, reader)
		return found, ok
	}
	collector := newStreamCollector()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() && !collector.add(scanner.Bytes()) {
	}
	_, _ = io.Copy(io.Discard, reader)
	return collector.result(false, inputBytes)
}

func (t *captureTransport) observe(reader *io.PipeReader, queued func(), stream bool, call capturedCall) {
	defer reader.Close()
	defer queued()
	found, ok := readObserved(reader, stream, call.inputBytes)
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
	reportCaptureError(t.opts.OnError, t.nozle.captureProvider(ctx, t.opts.Buffer, queued, found.format, p, found.usage))
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
	usage             tokenUsage
}

// detectUsage recognises token usage by the shape of a provider response, not by provider name:
// an OpenAI-style usage (prompt_tokens, or input_tokens from the Responses API), an Anthropic
// usage (a "message" object or cache token fields), or Gemini usageMetadata.
func detectUsage(body map[string]any) (detectedUsage, bool) {
	found := detectedUsage{id: firstString(body, "id", "responseId"), model: firstString(body, "model", "modelVersion")}
	if meta, ok := body["usageMetadata"].(map[string]any); ok {
		prompt := tokens(meta["promptTokenCount"])
		candidates := tokens(meta["candidatesTokenCount"])
		reasoning := tokens(meta["thoughtsTokenCount"])
		in, inRest := take(prompt, tokens(meta["cachedContentTokenCount"]), modalityTokens(meta["promptTokensDetails"], "AUDIO"), modalityTokens(meta["promptTokensDetails"], "IMAGE"))
		out, outRest := take(candidates, modalityTokens(meta["candidatesTokensDetails"], "AUDIO"), modalityTokens(meta["candidatesTokensDetails"], "IMAGE"))
		found.format = "gemini"
		found.usage = tokenUsage{input: prompt, output: candidates + reasoning, categories: []tokenCategory{
			{"input", inRest}, {"cached_input", in[0]}, {"audio_input", in[1]}, {"image_input", in[2]},
			{"output", outRest}, {"reasoning", reasoning}, {"audio_output", out[0]}, {"image_output", out[1]},
		}}
		return found, true
	}
	usage, ok := body["usage"].(map[string]any)
	if !ok {
		return found, false
	}
	batch := usage["service_tier"] == "batch" || body["service_tier"] == "batch"
	_, cacheRead := usage["cache_read_input_tokens"]
	_, cacheWrite := usage["cache_creation_input_tokens"]
	if body["type"] == "message" || cacheRead || cacheWrite {
		input := tokens(usage["input_tokens"])
		cached := tokens(usage["cache_read_input_tokens"])
		created, _ := usage["cache_creation"].(map[string]any)
		written1h := tokens(created["ephemeral_1h_input_tokens"])
		written := tokens(created["ephemeral_5m_input_tokens"]) + written1h
		// The flat total wins when both are present; the 1h part never exceeds it.
		if cacheWrite {
			written = tokens(usage["cache_creation_input_tokens"])
		}
		written1h = min(written1h, written)
		output := tokens(usage["output_tokens"])
		found.format = "anthropic"
		found.usage = tokenUsage{input: input + cached + written, output: output, batch: batch, categories: []tokenCategory{
			{"input", input}, {"cached_input", cached}, {"cache_write", written - written1h}, {"cache_write_1h", written1h}, {"output", output},
		}}
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
	inDetails := nested(usage, "prompt_tokens_details", "input_tokens_details")
	outDetails := nested(usage, "completion_tokens_details", "output_tokens_details")
	in, inRest := take(input, tokens(inDetails["cached_tokens"]), tokens(inDetails["cache_write_tokens"]), tokens(inDetails["audio_tokens"]))
	out, outRest := take(output, tokens(outDetails["reasoning_tokens"]), tokens(outDetails["audio_tokens"]))
	found.format = "openai"
	found.usage = tokenUsage{input: input, output: output, batch: batch, categories: []tokenCategory{
		{"input", inRest}, {"cached_input", in[0]}, {"cache_write", in[1]}, {"audio_input", in[2]},
		{"output", outRest}, {"reasoning", out[0]}, {"audio_output", out[1]},
	}}
	return found, true
}

// modalityTokens sums the entries of a Gemini *TokensDetails array that have this modality.
func modalityTokens(details any, modality string) int {
	total := 0
	list, _ := details.([]any)
	for _, item := range list {
		if entry, ok := item.(map[string]any); ok && entry["modality"] == modality {
			total += tokens(entry["tokenCount"])
		}
	}
	return total
}

// streamCollector folds server-sent events into the final usage. OpenAI sends usage in the last
// chunk, the Responses API in response.completed, Anthropic in message_start and then a
// cumulative message_delta, and Gemini in every chunk with the totals last. It also counts the delivered delta text,
// for the estimate of a stream that ended early, and notes the provider's final usage marker.
type streamCollector struct {
	body       map[string]any
	deltaBytes int
	finished   bool
}

func newStreamCollector() *streamCollector { return &streamCollector{body: map[string]any{}} }

// add reads one stream line and reports whether the stream has ended ("[DONE]").
func (s *streamCollector) add(line []byte) bool {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return false
	}
	data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
	if bytes.Equal(data, []byte("[DONE]")) {
		s.finished = true
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
	for _, key := range []string{"id", "responseId", "model", "modelVersion", "service_tier"} {
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
	s.deltaBytes += deltaTextBytes(event)
	// The provider's final usage marker: the OpenAI usage chunk, response.completed, message_stop, Gemini's
	// finishReason chunk.
	switch event["type"] {
	case "message_stop", "response.completed", "response.incomplete":
		s.finished = true
	}
	if choices, ok := event["choices"].([]any); ok && len(choices) == 0 && event["usage"] != nil {
		s.finished = true
	}
	if candidates, ok := event["candidates"].([]any); ok {
		for _, candidate := range candidates {
			if entry, ok := candidate.(map[string]any); ok && entry["finishReason"] != nil {
				s.finished = true
			}
		}
	}
	return false
}

// deltaTextBytes is the UTF-8 size of the text, tool-call arguments and thinking a stream event delivers.
func deltaTextBytes(event map[string]any) int {
	size := func(value any) int { text, _ := value.(string); return len(text) }
	total := 0
	switch delta := event["delta"].(type) {
	case string:
		total += len(delta)
	case map[string]any:
		total += size(delta["text"]) + size(delta["partial_json"]) + size(delta["thinking"])
	}
	choices, _ := event["choices"].([]any)
	for _, choice := range choices {
		entry, _ := choice.(map[string]any)
		delta, _ := entry["delta"].(map[string]any)
		total += size(delta["content"])
		calls, _ := delta["tool_calls"].([]any)
		for _, call := range calls {
			entry, _ := call.(map[string]any)
			function, _ := entry["function"].(map[string]any)
			total += size(function["arguments"])
		}
	}
	candidates, _ := event["candidates"].([]any)
	for _, candidate := range candidates {
		entry, _ := candidate.(map[string]any)
		content, _ := entry["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, part := range parts {
			text, _ := part.(map[string]any)
			total += size(text["text"])
		}
	}
	return total
}

// result is the usage of the stream. ended is false when the stream stopped before the provider's final usage: the
// usage is then the provider's partial report, or an estimate when it sent none. inputBytes is the UTF-8 size of the
// request message text, for the input estimate.
func (s *streamCollector) result(ended bool, inputBytes int) (detectedUsage, bool) {
	found, ok := detectUsage(s.body)
	if ended || s.finished {
		return found, ok
	}
	estimatedOutput := estimateTokens(s.deltaBytes)
	if ok {
		found.usage.aborted = true
		reported := found.usage.output
		// Anthropic's message_start output count is a placeholder; delivered text can be longer.
		if found.format != "anthropic" || estimatedOutput <= reported {
			return found, true
		}
		found.usage.output, found.usage.estimated = estimatedOutput, true
		replaced := false
		for i := range found.usage.categories {
			if found.usage.categories[i].name == "output" {
				found.usage.categories[i].tokens += estimatedOutput - reported
				replaced = true
			}
		}
		if !replaced {
			found.usage.categories = append(found.usage.categories, tokenCategory{"output", estimatedOutput - reported})
		}
		return found, true
	}
	estimatedInput := estimateTokens(inputBytes)
	if estimatedInput == 0 && estimatedOutput == 0 {
		return detectedUsage{}, false
	}
	found = detectedUsage{id: firstString(s.body, "id", "responseId"), model: firstString(s.body, "model", "modelVersion"), format: "openai"}
	if s.body["type"] == "message" {
		found.format = "anthropic"
	} else if _, gemini := s.body["usageMetadata"]; gemini {
		found.format = "gemini"
	}
	found.usage = tokenUsage{input: estimatedInput, output: estimatedOutput, estimated: true, aborted: true, categories: []tokenCategory{{"input", estimatedInput}, {"output", estimatedOutput}}}
	return found, true
}

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
