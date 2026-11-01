package nozle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ProviderWrapOptions configures automatic usage capture for provider HTTP clients.
// OnError receives telemetry failures without changing the provider response; without it they are logged through
// slog.Default().
type ProviderWrapOptions struct {
	CustomerID, SubscriptionID, FeatureCode, Feature, CostMeterCode string
	DeliveryTimeout                                                 time.Duration
	OnError                                                         func(error)
	OnResult                                                        func(error)
	// Buffer, when set, queues each usage event in this event buffer instead of sending it at once, so Flush and
	// Close deliver it. The event is queued before the response body's last Read returns. Cost Events are still
	// sent directly. The buffer's OnError reports what it does not deliver.
	Buffer *EventBuffer
}

type providerHTTPTransport struct {
	base     http.RoundTripper
	nozle    *Client
	opts     ProviderWrapOptions
	provider string
}

// WrapOpenAIHTTPClient returns a copy of providerClient that automatically captures
// usage from OpenAI chat completions, including final streaming usage chunks.
func WrapOpenAIHTTPClient(providerClient *http.Client, nozleClient *Client, opts ProviderWrapOptions) (*http.Client, error) {
	return wrapProviderHTTPClient(providerClient, nozleClient, opts, "openai")
}

// WrapAnthropicHTTPClient returns a copy of providerClient that automatically
// captures Anthropic message usage, including completed streams.
func WrapAnthropicHTTPClient(providerClient *http.Client, nozleClient *Client, opts ProviderWrapOptions) (*http.Client, error) {
	return wrapProviderHTTPClient(providerClient, nozleClient, opts, "anthropic")
}

func wrapProviderHTTPClient(providerClient *http.Client, nozleClient *Client, opts ProviderWrapOptions, provider string) (*http.Client, error) {
	if nozleClient == nil {
		return nil, &ValidationError{"wrap_" + provider, "Nozle client is required"}
	}
	if strings.TrimSpace(opts.CustomerID) == "" {
		return nil, &ValidationError{"wrap_" + provider, "customer ID is required"}
	}
	if providerClient == nil {
		providerClient = &http.Client{}
	}
	wrapped := *providerClient
	base := providerClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	wrapped.Transport = &providerHTTPTransport{base: base, nozle: nozleClient, opts: opts, provider: provider}
	return &wrapped, nil
}

func (t *providerHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || !t.matches(req.URL.Path) {
		return t.base.RoundTrip(req)
	}
	started := time.Now()
	request, model, inputBytes, err := prepareProviderRequest(req, t.provider)
	if err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.Body == nil {
		return resp, nil
	}
	reader, writer := io.Pipe()
	body := newObservedBody(resp.Body, writer, t.opts.Buffer != nil)
	resp.Body = body
	stream := strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	go t.observe(reader, body.markQueued, stream, model, inputBytes, started)
	return resp, nil
}

func (t *providerHTTPTransport) matches(path string) bool {
	if t.provider == "openai" {
		return strings.HasSuffix(path, "/chat/completions")
	}
	return strings.HasSuffix(path, "/messages")
}

func prepareProviderRequest(req *http.Request, provider string) (*http.Request, string, int, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return req, "", 0, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, "", 0, err
	}
	_ = req.Body.Close()
	fields := map[string]json.RawMessage{}
	model := ""
	inputBytes := 0
	if json.Unmarshal(body, &fields) == nil {
		inputBytes = requestTextBytes(fields)
		_ = json.Unmarshal(fields["model"], &model)
		if provider == "openai" {
			var stream bool
			_ = json.Unmarshal(fields["stream"], &stream)
			if stream {
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
	}
	request := req.Clone(req.Context())
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	return request, model, inputBytes, nil
}

type observedBody struct {
	io.ReadCloser
	writer   *io.PipeWriter
	mu       sync.Mutex
	finished bool
	// queued, when set, is closed once the observer queued the usage event in a buffer or gave up; the body's end
	// and Close wait for it, so the event is in the buffer before the caller can Flush or Close it.
	queued     chan struct{}
	queuedOnce sync.Once
}

func newObservedBody(body io.ReadCloser, writer *io.PipeWriter, buffered bool) *observedBody {
	b := &observedBody{ReadCloser: body, writer: writer}
	if buffered {
		b.queued = make(chan struct{})
	}
	return b
}

func (b *observedBody) markQueued() {
	if b.queued != nil {
		b.queuedOnce.Do(func() { close(b.queued) })
	}
}

func (b *observedBody) waitQueued() {
	if b.queued != nil {
		<-b.queued
	}
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		_, _ = b.writer.Write(p[:n])
	}
	if err != nil {
		b.mu.Lock()
		if !b.finished {
			b.finished = true
			if errors.Is(err, io.EOF) {
				_ = b.writer.Close()
			} else {
				_ = b.writer.CloseWithError(err)
			}
		}
		b.mu.Unlock()
		b.waitQueued()
	}
	return n, err
}

func (b *observedBody) Close() error {
	b.mu.Lock()
	if !b.finished {
		b.finished = true
		_ = b.writer.CloseWithError(io.ErrClosedPipe)
	}
	b.mu.Unlock()
	b.waitQueued()
	return b.ReadCloser.Close()
}

func (t *providerHTTPTransport) observe(reader *io.PipeReader, queued func(), stream bool, model string, inputBytes int, started time.Time) {
	defer reader.Close()
	defer queued()
	found, ok := readObserved(reader, stream, inputBytes)
	if !ok {
		return
	}
	if found.model == "" {
		found.model = model
	}
	limit := t.opts.DeliveryTimeout
	if limit <= 0 {
		limit = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	p := ProviderCapture{CustomerID: t.opts.CustomerID, SubscriptionID: t.opts.SubscriptionID, FeatureCode: t.opts.FeatureCode, Feature: t.opts.Feature, CostMeterCode: t.opts.CostMeterCode, Model: found.model, RequestID: found.id, Latency: time.Since(started)}
	err := t.nozle.captureProvider(ctx, t.opts.Buffer, queued, t.provider, p, found.usage)
	if t.opts.OnResult != nil {
		t.opts.OnResult(err)
	}
	reportCaptureError(t.opts.OnError, err)
}
