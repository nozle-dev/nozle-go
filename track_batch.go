package nozle

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// MaxTrackBatchChunkSize is the most events Engine takes in one /events/batch request by default
// (LAGO_EVENTS_BATCH_MAX_LENGTH, Engine 2.28.0), and the largest WithBatchChunkSize. A request above a
// deployment's lower maximum is split and resent.
const MaxTrackBatchChunkSize = 5000

const (
	defaultBatchConcurrency = 4
	maxBatchConcurrency     = 64
	// One request, plus a resend after each answer that names bad events. A rejected request stores nothing.
	maxBatchRounds = 3
)

// TrackBatchOption configures one TrackBatch call.
type TrackBatchOption func(*trackBatchConfig)

type trackBatchConfig struct{ chunkSize, concurrency int }

// WithBatchChunkSize sets the events per request, 1 to MaxTrackBatchChunkSize (default 5000).
func WithBatchChunkSize(n int) TrackBatchOption { return func(c *trackBatchConfig) { c.chunkSize = n } }

// WithBatchConcurrency sets how many requests are in flight at once, 1 to 64 (default 4).
func WithBatchConcurrency(n int) TrackBatchOption {
	return func(c *trackBatchConfig) { c.concurrency = n }
}

// BatchEventStatus is the outcome of one event of TrackBatch.
type BatchEventStatus string

const (
	// BatchEventAccepted: Engine stored the event.
	BatchEventAccepted BatchEventStatus = "accepted"
	// BatchEventRejected: Engine refused this event. Errors maps a field to codes; resending it unchanged fails again.
	BatchEventRejected BatchEventStatus = "rejected"
	// BatchEventFailed: the request carrying it failed (Err is an *APIError, a *TransportError or a context error)
	// and the event may or may not be stored. Resend it with the same TransactionID.
	BatchEventFailed BatchEventStatus = "failed"
)

// TrackBatchEventResult is the outcome of the event at Index of the input.
type TrackBatchEventResult struct {
	Index         int
	TransactionID string
	Status        BatchEventStatus
	Errors        map[string][]string // set when rejected
	Err           error               // set when failed
}

// TrackBatchResult has one entry per input event, in input order.
type TrackBatchResult struct {
	Total, Accepted, Rejected, Failed int
	Results                           []TrackBatchEventResult
}

// BatchEventError is returned before any request when one event of a TrackBatch is invalid.
type BatchEventError struct {
	ValidationError
	Index int // position of the event, or -1 when the problem is not one event's
}

// Unwrap lets errors.As find the *ValidationError.
func (e *BatchEventError) Unwrap() error { return &e.ValidationError }

func batchInvalid(index int, format string, args ...any) error {
	return &BatchEventError{ValidationError{"track_batch", fmt.Sprintf(format, args...)}, index}
}

type batchItem struct {
	index int
	id    string
	body  map[string]any
}

// TrackBatch records many events through Engine's /events/batch: split into gzip requests of at most
// WithBatchChunkSize (default and maximum 5000), sent WithBatchConcurrency (default 4) at a time. It returns one
// result per event and returns an error only for invalid arguments; a failed request marks its events failed and
// the other requests still run.
//
// Engine stores the valid events of a request and names the refused ones by position; TrackBatch marks those
// rejected. An Engine before 2.28.0 refuses a whole request for one invalid event, storing nothing: TrackBatch
// then marks the named events rejected and resends the rest (at most 3 requests per chunk). A request above the
// Engine's maximum (too_many_events) is split in two and each half resent.
// Every event has a transaction ID (generated when empty, and returned in the result), so a request retried after
// a network error, timeout, 429 or 5xx (WithMaxRetries) records each event once. IDs must be unique in one call.
// Each event keeps its own Timestamp; without one Engine stamps it on arrival. Engine resolves the subscription
// when SubscriptionID is empty. An empty list sends nothing.
func (c *Client) TrackBatch(ctx context.Context, events []TrackParams, opts ...TrackBatchOption) (*TrackBatchResult, error) {
	if err := validateKey(c.apiKey, "track_batch"); err != nil {
		return nil, err
	}
	cfg := trackBatchConfig{chunkSize: MaxTrackBatchChunkSize, concurrency: defaultBatchConcurrency}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.chunkSize < 1 || cfg.chunkSize > MaxTrackBatchChunkSize {
		return nil, batchInvalid(-1, "chunk size must be from 1 to %d", MaxTrackBatchChunkSize)
	}
	if cfg.concurrency < 1 || cfg.concurrency > maxBatchConcurrency {
		return nil, batchInvalid(-1, "concurrency must be from 1 to %d", maxBatchConcurrency)
	}
	items := make([]batchItem, len(events))
	seen := map[string]bool{}
	for i, p := range events {
		if strings.TrimSpace(p.CustomerID) == "" {
			return nil, batchInvalid(i, "events[%d] requires CustomerID", i)
		}
		if strings.TrimSpace(p.Event) == "" {
			return nil, batchInvalid(i, "events[%d] requires Event", i)
		}
		id := p.TransactionID
		if id == "" {
			id = generateUUID()
		}
		if seen[id] {
			return nil, batchInvalid(i, "events[%d] repeats TransactionID %s", i, id)
		}
		seen[id] = true
		properties := p.Metadata
		if properties == nil {
			properties = map[string]any{}
		}
		body := map[string]any{"transaction_id": id, "external_customer_id": p.CustomerID, "code": p.Event, "properties": properties}
		if p.SubscriptionID != "" {
			body["external_subscription_id"] = p.SubscriptionID
		}
		if p.Timestamp != "" {
			normalized, err := eventTimestamp(p.Timestamp)
			if err != nil {
				return nil, batchInvalid(i, "events[%d]: %s", i, err.Error())
			}
			body["timestamp"] = normalized
		}
		items[i] = batchItem{i, id, body}
	}

	results := make([]TrackBatchEventResult, len(items))
	gate := make(chan struct{}, cfg.concurrency)
	var wg sync.WaitGroup
	for start := 0; start < len(items); start += cfg.chunkSize {
		chunk := items[start:min(start+cfg.chunkSize, len(items))]
		wg.Add(1)
		gate <- struct{}{}
		go func() {
			defer func() { <-gate; wg.Done() }()
			c.sendBatchChunk(ctx, chunk, results)
		}()
	}
	wg.Wait()

	out := &TrackBatchResult{Total: len(results), Results: results}
	for _, r := range results {
		switch r.Status {
		case BatchEventAccepted:
			out.Accepted++
		case BatchEventRejected:
			out.Rejected++
		default:
			out.Failed++
		}
	}
	return out, nil
}

// sendBatchChunk sends one chunk and writes a result for each of its items. It never fails the call.
func (c *Client) sendBatchChunk(ctx context.Context, chunk []batchItem, results []TrackBatchEventResult) {
	fail := func(pending []batchItem, err error) {
		for _, item := range pending {
			results[item.index] = TrackBatchEventResult{Index: item.index, TransactionID: item.id, Status: BatchEventFailed, Err: err}
		}
	}
	pending := chunk
	for round := 1; len(pending) > 0; round++ {
		payload := make([]map[string]any, len(pending))
		for i, item := range pending {
			payload[i] = item.body
		}
		var answer map[string]any
		// Every event carries its transaction ID, so a repeat of this request is recorded once.
		err := c.request(ctx, "track_batch", "POST", false, "/api/v1/events/batch", map[string]any{"events": payload}, &answer, "")
		var refused map[int]map[string][]string
		if err == nil {
			// A 200 stored every event except those an overdue payment refused, listed by position.
			refused = batchPositions(answer["error_details"], len(pending))
			for position, item := range pending {
				r := TrackBatchEventResult{Index: item.index, TransactionID: item.id, Status: BatchEventAccepted}
				if fields, ok := refused[position]; ok {
					r.Status, r.Errors = BatchEventRejected, fields
				}
				results[item.index] = r
			}
			return
		}
		var api *APIError
		if AsAPIError(err, &api) {
			details, _ := api.Details.(map[string]any)
			if tooManyEvents(details["error_details"]) && len(pending) > 1 {
				// The Engine takes fewer events per request: send each half on its own.
				half := len(pending) / 2
				c.sendBatchChunk(ctx, pending[:half], results)
				c.sendBatchChunk(ctx, pending[half:], results)
				return
			}
			refused = batchPositions(details["error_details"], len(pending))
		}
		if refused == nil {
			fail(pending, err)
			return
		}
		// Engine named the bad events and stored none: reject those and resend the rest.
		rest := pending[:0:0]
		for position, item := range pending {
			if fields, ok := refused[position]; ok {
				results[item.index] = TrackBatchEventResult{Index: item.index, TransactionID: item.id, Status: BatchEventRejected, Errors: fields}
			} else {
				rest = append(rest, item)
			}
		}
		pending = rest
		if len(pending) > 0 && round >= maxBatchRounds {
			fail(pending, err)
			return
		}
	}
}

// tooManyEvents reports the error_details of a request above the Engine's maximum batch size.
func tooManyEvents(details any) bool {
	fields, _ := details.(map[string]any)
	codes, _ := fields["events"].([]any)
	return len(fields) == 1 && len(codes) == 1 && codes[0] == "too_many_events"
}

// batchPositions reads an error_details keyed by position in the request; nil for any other shape.
func batchPositions(details any, size int) map[int]map[string][]string {
	listed, _ := details.(map[string]any)
	if len(listed) == 0 {
		return nil
	}
	out := map[int]map[string][]string{}
	for key, value := range listed {
		position, err := strconv.Atoi(key)
		fields, ok := value.(map[string]any)
		if err != nil || position < 0 || position >= size || !ok {
			return nil
		}
		codes := map[string][]string{}
		for field, raw := range fields {
			list, _ := raw.([]any)
			for _, code := range list {
				if s, ok := code.(string); ok {
					codes[field] = append(codes[field], s)
				}
			}
		}
		out[position] = codes
	}
	return out
}
