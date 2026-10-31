package nozle

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	defaultBufferBatchSize     = 500
	defaultBufferFlushInterval = 5 * time.Second
	defaultBufferQueueSize     = 10_000
	defaultBufferMaxAttempts   = 3
	// After a flush with failed events, automatic flushes pause 1 s, doubling per such flush up to 60 s.
	bufferBackoffBase = time.Second
	bufferBackoffCap  = time.Minute
)

// ErrBufferFull is returned by EventBuffer.Track when the queue holds MaxQueueSize events. The event is not queued.
var ErrBufferFull = errors.New("nozle: event buffer is full")

// ErrBufferClosed is returned by EventBuffer.Track after Close. The event is not queued.
var ErrBufferClosed = errors.New("nozle: event buffer is closed")

// EventBufferOptions configures NewEventBuffer. A zero field takes its default.
type EventBufferOptions struct {
	// MaxBatchSize is the most events per request, 1 to MaxTrackBatchChunkSize (default 500). A flush starts as soon
	// as this many events are waiting.
	MaxBatchSize int
	// FlushInterval is how often the waiting events are flushed (default 5 s). A negative value disables the timer.
	FlushInterval time.Duration
	// MaxQueueSize bounds the events held, waiting or being sent (default 10 000). When it is reached Track drops the
	// new event, reports buffer_full and returns ErrBufferFull.
	MaxQueueSize int
	// MaxAttempts is how many flushes an event may fail before it is dropped and reported as failed (default 3).
	// After a flush with a failed event, size and timer flushes pause 1 s, doubling after each such flush up to
	// 60 s, and resume at once after a flush without failures. Flush and Close do not wait for the pause.
	MaxAttempts int
	// OnError receives every event the buffer does not deliver, with an *EventBufferError whose Code is
	// "buffer_full", "rejected", "failed" or "closed". Without it the buffer logs through the standard log package.
	// It runs on the caller's goroutine for buffer_full and closed, and on the buffer's goroutine otherwise.
	OnError func(err error, events []TrackParams)
}

// EventBufferError reports events an EventBuffer did not deliver.
type EventBufferError struct {
	// Code is "buffer_full" (the queue was full), "rejected" (Engine refused the event; not retried), "failed" (its
	// requests failed MaxAttempts flushes) or "closed" (Track after Close).
	Code string
	// Errors maps a field to Engine's codes when Code is "rejected".
	Errors map[string][]string
	// Err is the last request error when Code is "failed", ErrBufferFull or ErrBufferClosed.
	Err error
}

func (e *EventBufferError) Error() string {
	switch {
	case e.Err != nil:
		return "nozle: event buffer: " + e.Code + ": " + e.Err.Error()
	case len(e.Errors) > 0:
		return fmt.Sprintf("nozle: event buffer: %s: %v", e.Code, e.Errors)
	}
	return "nozle: event buffer: " + e.Code
}

// Unwrap lets errors.Is find ErrBufferFull, ErrBufferClosed or the request error.
func (e *EventBufferError) Unwrap() error { return e.Err }

// EventBuffer queues events in memory and sends them with TrackBatch from one background goroutine: when
// MaxBatchSize events are waiting, every FlushInterval, and on Flush and Close. Only one flush runs at a time.
// Create it with Client.NewEventBuffer. Its methods are safe for concurrent use.
//
// Go has no exit hook: call Close before the program (or a serverless handler) returns, for example
// defer buf.Close(ctx). Events still queued when the process ends are lost.
type EventBuffer struct {
	client      *Client
	batchSize   int
	queueSize   int
	maxAttempts int
	interval    time.Duration
	onError     func(error, []TrackParams)
	sendCtx     context.Context
	cancelSend  context.CancelCauseFunc
	wake, quit  chan struct{}
	stopped     chan struct{}
	quitOnce    sync.Once
	mu          sync.Mutex
	queue       []bufferedEvent
	sending     []bufferedEvent
	closed      bool
	wanted      uint64 // the flush number a Flush waits for
	started     uint64
	finished    uint64
	flushDone   chan struct{} // closed and replaced when a flush finishes
	backoff     time.Duration // the pause after the last flush; 0 when it had no failed event
	pausedUntil time.Time     // size and timer triggers wait until then; Flush and Close do not
}

type bufferedEvent struct {
	params   TrackParams
	attempts int
}

// NewEventBuffer starts a buffer that sends events through TrackBatch in the background. It panics when an
// option is out of range, like New does for a missing key. Call Close when done.
func (c *Client) NewEventBuffer(opts EventBufferOptions) *EventBuffer {
	b := &EventBuffer{
		client: c, batchSize: opts.MaxBatchSize, queueSize: opts.MaxQueueSize, maxAttempts: opts.MaxAttempts,
		interval: opts.FlushInterval, onError: opts.OnError, wake: make(chan struct{}, 1), quit: make(chan struct{}),
		stopped: make(chan struct{}), flushDone: make(chan struct{}),
	}
	if b.batchSize == 0 {
		b.batchSize = defaultBufferBatchSize
	}
	if b.queueSize == 0 {
		b.queueSize = defaultBufferQueueSize
	}
	if b.maxAttempts == 0 {
		b.maxAttempts = defaultBufferMaxAttempts
	}
	if b.interval == 0 {
		b.interval = defaultBufferFlushInterval
	}
	if b.batchSize < 1 || b.batchSize > MaxTrackBatchChunkSize {
		panic(fmt.Sprintf("nozle: EventBufferOptions.MaxBatchSize must be from 1 to %d", MaxTrackBatchChunkSize))
	}
	if b.queueSize < 1 || b.maxAttempts < 1 {
		panic("nozle: EventBufferOptions.MaxQueueSize and MaxAttempts must be positive")
	}
	if b.onError == nil {
		b.onError = func(err error, events []TrackParams) { log.Printf("[nozle] %v (%d events)", err, len(events)) }
	}
	b.sendCtx, b.cancelSend = context.WithCancelCause(context.Background())
	go b.run()
	return b
}

// Track queues an event and returns at once, without a network call. See TrackWithID.
func (b *EventBuffer) Track(p TrackParams) error {
	_, err := b.TrackWithID(p)
	return err
}

// TrackWithID queues an event and returns its transaction ID: p.TransactionID, or a generated UUID version 7. Every
// resend of the event carries that ID, so Engine records it once. It returns a *ValidationError for an event that
// TrackBatch would refuse, ErrBufferFull when the queue is full and ErrBufferClosed after Close; the last two are
// also reported to OnError. Do not change p.Metadata after the call.
func (b *EventBuffer) TrackWithID(p TrackParams) (string, error) {
	if err := validateKey(b.client.apiKey, "event_buffer"); err != nil {
		return "", err
	}
	if err := required("event_buffer", "customer_id", p.CustomerID); err != nil {
		return "", err
	}
	if err := required("event_buffer", "event", p.Event); err != nil {
		return "", err
	}
	if p.Timestamp != "" {
		if _, err := eventTimestamp(p.Timestamp); err != nil {
			return "", err
		}
	}
	if p.TransactionID == "" {
		p.TransactionID = generateUUID()
	}
	b.mu.Lock()
	var refused error
	switch {
	case b.closed:
		refused = ErrBufferClosed
	case len(b.queue)+len(b.sending) >= b.queueSize:
		refused = ErrBufferFull
	default:
		b.queue = append(b.queue, bufferedEvent{params: p})
	}
	due := len(b.queue) >= b.batchSize
	b.mu.Unlock()
	if refused != nil {
		code := "buffer_full"
		if refused == ErrBufferClosed {
			code = "closed"
		}
		b.onError(&EventBufferError{Code: code, Err: refused}, []TrackParams{p})
		return p.TransactionID, refused
	}
	if due {
		b.signal()
	}
	return p.TransactionID, nil
}

// Len returns how many events are waiting to be sent; events in a request in flight are not counted.
func (b *EventBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queue)
}

// held counts the waiting events and those in a request in flight.
func (b *EventBuffer) held() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queue) + len(b.sending)
}

// Flush sends every event waiting now and returns when that flush is done, or ctx.Err() when ctx ends first (the
// flush then continues in the background). An event whose request failed stays queued for the next flush.
// Undelivered events are reported to OnError, not returned.
func (b *EventBuffer) Flush(ctx context.Context) error {
	b.mu.Lock()
	target := b.started // a flush in progress
	if len(b.queue) > 0 {
		target = b.started + 1 // the next flush takes everything waiting now
		b.wanted = max(b.wanted, target)
	}
	b.mu.Unlock()
	b.signal()
	for {
		b.mu.Lock()
		done, changed := b.finished >= target, b.flushDone
		b.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Close stops the timer, flushes until every event is delivered or reported (failed events are resent up to
// MaxAttempts flushes), refuses later Track calls and stops the goroutine. When ctx ends first it cancels the
// request in flight, reports the events left as failed with the context error and returns ctx.Err().
// Closing twice is safe.
func (b *EventBuffer) Close(ctx context.Context) error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	var err error
	for err == nil && b.held() > 0 {
		err = b.Flush(ctx)
	}
	b.cancelSend(err) // aborts the request in flight when ctx ended; a no-op after a full drain
	b.quitOnce.Do(func() { close(b.quit) })
	<-b.stopped
	return err
}

func (b *EventBuffer) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *EventBuffer) run() {
	defer close(b.stopped)
	defer b.dropQueued()
	ticker := &time.Ticker{}
	if b.interval > 0 {
		ticker = time.NewTicker(b.interval)
		defer ticker.Stop()
	}
	resume := time.NewTimer(time.Hour)
	resume.Stop()
	defer resume.Stop()
	for {
		timer := false
		select {
		case <-b.quit:
			return
		case <-b.wake:
		case <-ticker.C:
			timer = true
		case <-resume.C:
			timer = b.interval > 0 // the backoff ended: flush what waited, as a tick would
		}
		for b.due(timer) {
			if pause := b.flush(); pause > 0 {
				resume.Reset(pause)
			}
			timer = false
			if b.sendCtx.Err() != nil {
				return
			}
		}
	}
}

// due reports whether a flush should start: a Flush waits, or, outside a backoff, the timer fired or a full batch
// is waiting.
func (b *EventBuffer) due(timer bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.queue) == 0 {
		return false
	}
	automatic := (timer && !b.closed) || len(b.queue) >= b.batchSize
	return b.wanted > b.started || (automatic && !time.Now().Before(b.pausedUntil))
}

// flush sends everything waiting in one TrackBatch call, requeues failed events at the front and reports the rest.
// It returns the backoff that pauses automatic flushes, or 0 when no event failed.
func (b *EventBuffer) flush() time.Duration {
	b.mu.Lock()
	b.started++
	// TrackBatch refuses a repeated transaction ID in one call: a repeat waits for the next flush.
	seen := map[string]bool{}
	var later []bufferedEvent
	for _, e := range b.queue {
		if seen[e.params.TransactionID] {
			later = append(later, e)
		} else {
			seen[e.params.TransactionID] = true
			b.sending = append(b.sending, e)
		}
	}
	b.queue = later
	sending := b.sending
	b.mu.Unlock()

	params := make([]TrackParams, len(sending))
	for i, e := range sending {
		params[i] = e.params
	}
	var retry []bufferedEvent
	var gaveUp []TrackParams
	var lastErr error
	res, err := b.client.TrackBatch(b.sendCtx, params, WithBatchChunkSize(b.batchSize))
	if err != nil {
		// TrackWithID validated every event, so this is not expected; report rather than resend a refused call.
		b.onError(&EventBufferError{Code: "rejected", Err: err}, params)
	} else {
		for i, r := range res.Results {
			switch r.Status {
			case BatchEventRejected:
				b.onError(&EventBufferError{Code: "rejected", Errors: r.Errors}, params[i:i+1])
			case BatchEventFailed:
				e := sending[i]
				e.attempts++
				lastErr = r.Err
				if b.sendCtx.Err() != nil {
					lastErr = context.Cause(b.sendCtx)
				}
				if e.attempts >= b.maxAttempts || b.sendCtx.Err() != nil {
					gaveUp = append(gaveUp, e.params)
				} else {
					retry = append(retry, e)
				}
			}
		}
	}
	if len(gaveUp) > 0 {
		b.onError(&EventBufferError{Code: "failed", Err: lastErr}, gaveUp)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queue = append(retry, b.queue...)
	b.sending = nil
	b.finished++
	close(b.flushDone)
	b.flushDone = make(chan struct{})
	if len(retry) == 0 && len(gaveUp) == 0 {
		b.backoff, b.pausedUntil = 0, time.Time{}
	} else {
		b.backoff = min(max(2*b.backoff, bufferBackoffBase), bufferBackoffCap)
		b.pausedUntil = time.Now().Add(b.backoff)
	}
	return b.backoff
}

// dropQueued reports the events left when the goroutine stops after Close's context ended.
func (b *EventBuffer) dropQueued() {
	b.mu.Lock()
	left := b.queue
	b.queue = nil
	b.finished = max(b.finished, b.wanted)
	close(b.flushDone)
	b.flushDone = make(chan struct{})
	b.mu.Unlock()
	if len(left) == 0 {
		return
	}
	err := context.Cause(b.sendCtx)
	if err == nil {
		err = ErrBufferClosed
	}
	params := make([]TrackParams, len(left))
	for i, e := range left {
		params[i] = e.params
	}
	b.onError(&EventBufferError{Code: "failed", Err: err}, params)
}
