package nozle_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

// reported records what an EventBuffer passes to OnError.
type reported struct {
	mu    sync.Mutex
	calls []reportedCall
}

type reportedCall struct {
	err    *nozle.EventBufferError
	events []nozle.TrackParams
}

func (r *reported) onError(err error, events []nozle.TrackParams) {
	var bufErr *nozle.EventBufferError
	if !errors.As(err, &bufErr) {
		panic(fmt.Sprintf("OnError got %T", err))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, reportedCall{bufErr, events})
}

func (r *reported) all() []reportedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]reportedCall(nil), r.calls...)
}

func (b *batchServer) requestsSoFar() [][]sentEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]sentEvent(nil), b.requests...)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func event(i int) nozle.TrackParams {
	return nozle.TrackParams{CustomerID: "c1", Event: "api_call", Metadata: map[string]any{"i": i}}
}

func TestEventBufferSizeTrigger(t *testing.T) {
	b := newBatchServer(t, nil)
	buf := b.client().NewEventBuffer(nozle.EventBufferOptions{MaxBatchSize: 3, FlushInterval: -1})
	defer buf.Close(context.Background())
	for i := 0; i < 2; i++ {
		if err := buf.Track(event(i)); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(30 * time.Millisecond)
	if n := len(b.requestsSoFar()); n != 0 || buf.Len() != 2 {
		t.Fatalf("flushed below the batch size: requests=%d len=%d", n, buf.Len())
	}
	id, err := buf.TrackWithID(event(2))
	if err != nil || id == "" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	waitFor(t, "a size flush", func() bool { return len(b.requestsSoFar()) == 1 })
	sent := b.requestsSoFar()[0]
	if len(sent) != 3 || sent[2].TransactionID != id || sent[0].TransactionID == "" {
		t.Fatalf("sent %+v", sent)
	}
	waitFor(t, "an empty buffer", func() bool { return buf.Len() == 0 })
}

func TestEventBufferTimerTrigger(t *testing.T) {
	b := newBatchServer(t, nil)
	buf := b.client().NewEventBuffer(nozle.EventBufferOptions{FlushInterval: 20 * time.Millisecond})
	defer buf.Close(context.Background())
	if err := buf.Track(event(0)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a timer flush", func() bool { return len(b.requestsSoFar()) == 1 })
}

func TestEventBufferFlushCloseAndTrackAfterClose(t *testing.T) {
	b := newBatchServer(t, nil)
	var r reported
	buf := b.client().NewEventBuffer(nozle.EventBufferOptions{FlushInterval: -1, OnError: r.onError})
	ctx := context.Background()
	buf.Track(event(0))
	buf.Track(nozle.TrackParams{CustomerID: "c2", Event: "api_call", TransactionID: "mine"})
	if err := buf.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	requests := b.requestsSoFar()
	if len(requests) != 1 || len(requests[0]) != 2 || requests[0][1].TransactionID != "mine" || buf.Len() != 0 {
		t.Fatalf("requests=%+v len=%d", requests, buf.Len())
	}
	if err := buf.Flush(ctx); err != nil || len(b.requestsSoFar()) != 1 {
		t.Fatalf("an empty flush sent a request: %v", err)
	}
	buf.Track(event(2))
	if err := buf.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(b.requestsSoFar()); n != 2 {
		t.Fatalf("close did not flush: %d requests", n)
	}
	id, err := buf.TrackWithID(event(3))
	if !errors.Is(err, nozle.ErrBufferClosed) || id == "" {
		t.Fatalf("track after close: id=%q err=%v", id, err)
	}
	calls := r.all()
	if len(calls) != 1 || calls[0].err.Code != "closed" || calls[0].events[0].TransactionID != id {
		t.Fatalf("reported %+v", calls)
	}
	if err := buf.Close(ctx); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestEventBufferOverflowDropsTheNewEvent(t *testing.T) {
	b := newBatchServer(t, nil)
	var r reported
	buf := b.client().NewEventBuffer(nozle.EventBufferOptions{MaxQueueSize: 2, FlushInterval: -1, OnError: r.onError})
	defer buf.Close(context.Background())
	buf.Track(event(0))
	buf.Track(event(1))
	id, err := buf.TrackWithID(event(2))
	if !errors.Is(err, nozle.ErrBufferFull) || buf.Len() != 2 {
		t.Fatalf("err=%v len=%d", err, buf.Len())
	}
	calls := r.all()
	if len(calls) != 1 || calls[0].err.Code != "buffer_full" || !errors.Is(calls[0].err, nozle.ErrBufferFull) ||
		len(calls[0].events) != 1 || calls[0].events[0].TransactionID != id {
		t.Fatalf("reported %+v", calls)
	}
}

func TestEventBufferRejectedIsReportedAndNotRetried(t *testing.T) {
	b := newBatchServer(t, func(call int, events []sentEvent) (int, string) {
		return 200, `{"events":[],"error_details":{"1":{"code":["value_is_mandatory"]}}}`
	})
	var r reported
	buf := b.client().NewEventBuffer(nozle.EventBufferOptions{FlushInterval: -1, OnError: r.onError})
	ctx := context.Background()
	buf.Track(event(0))
	bad, _ := buf.TrackWithID(event(1))
	buf.Flush(ctx)
	buf.Flush(ctx)
	if err := buf.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(b.requestsSoFar()); n != 1 {
		t.Fatalf("rejected event was resent: %d requests", n)
	}
	calls := r.all()
	if len(calls) != 1 || calls[0].err.Code != "rejected" || calls[0].events[0].TransactionID != bad ||
		calls[0].err.Errors["code"][0] != "value_is_mandatory" {
		t.Fatalf("reported %+v", calls)
	}
}

func TestEventBufferFailedIsRequeuedThenReported(t *testing.T) {
	b := newBatchServer(t, func(call int, events []sentEvent) (int, string) { return 503, `{"code":"unavailable"}` })
	var r reported
	buf := b.client(nozle.WithMaxRetries(0)).NewEventBuffer(nozle.EventBufferOptions{FlushInterval: -1, MaxAttempts: 3, OnError: r.onError})
	ctx := context.Background()
	id, _ := buf.TrackWithID(event(0))
	buf.Track(event(1))
	for flush := 1; flush <= 2; flush++ {
		buf.Flush(ctx)
		if buf.Len() != 2 || len(r.all()) != 0 {
			t.Fatalf("flush %d: len=%d reported=%+v", flush, buf.Len(), r.all())
		}
	}
	buf.Flush(ctx)
	requests := b.requestsSoFar()
	if len(requests) != 3 {
		t.Fatalf("%d requests, want 3", len(requests))
	}
	for _, sent := range requests {
		if len(sent) != 2 || sent[0].TransactionID != id {
			t.Fatalf("resend changed the transaction id or order: %+v", sent)
		}
	}
	calls := r.all()
	var api *nozle.APIError
	if len(calls) != 1 || calls[0].err.Code != "failed" || len(calls[0].events) != 2 ||
		calls[0].events[0].TransactionID != id || !errors.As(calls[0].err, &api) || api.StatusCode != 503 {
		t.Fatalf("reported %+v", calls)
	}
	if buf.Len() != 0 {
		t.Fatalf("len=%d after giving up", buf.Len())
	}
	buf.Close(ctx)
}

func TestEventBufferCloseRetriesFailedEventsBeforeGivingUp(t *testing.T) {
	b := newBatchServer(t, func(call int, events []sentEvent) (int, string) {
		if call == 1 {
			return 500, `{}`
		}
		return 0, ""
	})
	var r reported
	buf := b.client(nozle.WithMaxRetries(0)).NewEventBuffer(nozle.EventBufferOptions{FlushInterval: -1, OnError: r.onError})
	buf.Track(event(0))
	if err := buf.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(b.requestsSoFar()); n != 2 || len(r.all()) != 0 {
		t.Fatalf("requests=%d reported=%+v", n, r.all())
	}
}

func TestEventBufferCloseHonoursContext(t *testing.T) {
	release := make(chan struct{})
	b := newBatchServer(t, func(call int, events []sentEvent) (int, string) {
		<-release
		return 0, ""
	})
	defer close(release)
	var r reported
	buf := b.client(nozle.WithMaxRetries(0)).NewEventBuffer(nozle.EventBufferOptions{FlushInterval: -1, OnError: r.onError})
	id, _ := buf.TrackWithID(event(0))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := buf.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close: %v", err)
	}
	calls := r.all()
	if len(calls) != 1 || calls[0].err.Code != "failed" || calls[0].events[0].TransactionID != id ||
		!errors.Is(calls[0].err, context.DeadlineExceeded) {
		t.Fatalf("reported %+v", calls)
	}
}

func TestEventBufferFlushHonoursContext(t *testing.T) {
	release := make(chan struct{})
	b := newBatchServer(t, func(call int, events []sentEvent) (int, string) {
		<-release
		return 0, ""
	})
	buf := b.client().NewEventBuffer(nozle.EventBufferOptions{FlushInterval: -1, MaxQueueSize: 1, OnError: func(error, []nozle.TrackParams) {}})
	buf.Track(event(0))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := buf.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("flush: %v", err)
	}
	// The event is in flight: Len counts waiting events only, and the queue bound counts both.
	if n := buf.Len(); n != 0 {
		t.Fatalf("len=%d while the event is in flight", n)
	}
	if err := buf.Track(event(1)); !errors.Is(err, nozle.ErrBufferFull) {
		t.Fatalf("in-flight event not counted against MaxQueueSize: %v", err)
	}
	close(release)
	if err := buf.Close(context.Background()); err != nil || buf.Len() != 0 || len(b.requestsSoFar()) != 1 {
		t.Fatalf("close: %v len=%d", err, buf.Len())
	}
}

func TestEventBufferConcurrentTrack(t *testing.T) {
	b := newBatchServer(t, nil)
	buf := b.client().NewEventBuffer(nozle.EventBufferOptions{MaxBatchSize: 50, FlushInterval: 5 * time.Millisecond})
	const goroutines, each = 8, 250
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := buf.Track(event(i)); err != nil {
					t.Error(err)
				}
				if i%50 == 0 {
					buf.Flush(context.Background())
					_ = buf.Len()
				}
			}
		}()
	}
	wg.Wait()
	if err := buf.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, sent := range b.requestsSoFar() {
		if len(sent) > 50 {
			t.Errorf("request of %d events above MaxBatchSize", len(sent))
		}
		for _, e := range sent {
			if seen[e.TransactionID] {
				t.Errorf("event %s sent twice", e.TransactionID)
			}
			seen[e.TransactionID] = true
		}
	}
	if len(seen) != goroutines*each {
		t.Fatalf("sent %d events, want %d", len(seen), goroutines*each)
	}
}

func TestEventBufferRepeatedTransactionIDWaitsForTheNextFlush(t *testing.T) {
	b := newBatchServer(t, nil)
	buf := b.client().NewEventBuffer(nozle.EventBufferOptions{FlushInterval: -1})
	e := event(0)
	e.TransactionID = "same"
	buf.Track(e)
	buf.Track(e)
	if err := buf.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	requests := b.requestsSoFar()
	if len(requests) != 2 || len(requests[0]) != 1 || len(requests[1]) != 1 {
		t.Fatalf("requests %+v", requests)
	}
}

func TestEventBufferValidatesAndLogsByDefault(t *testing.T) {
	b := newBatchServer(t, nil)
	buf := b.client().NewEventBuffer(nozle.EventBufferOptions{MaxQueueSize: 1, FlushInterval: -1})
	defer buf.Close(context.Background())
	var invalid *nozle.ValidationError
	if err := buf.Track(nozle.TrackParams{Event: "api_call"}); !errors.As(err, &invalid) {
		t.Fatalf("missing customer: %v", err)
	}
	if err := buf.Track(nozle.TrackParams{CustomerID: "c1", Event: "api_call", Timestamp: "yesterday"}); !errors.As(err, &invalid) {
		t.Fatalf("bad timestamp: %v", err)
	}
	var out bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&out)
	buf.Track(event(0))
	buf.Track(event(1))
	log.SetOutput(previous)
	if !strings.Contains(out.String(), "[nozle] nozle: event buffer: buffer_full") {
		t.Fatalf("logged %q", out.String())
	}
}

func TestEventBufferRejectsOutOfRangeOptions(t *testing.T) {
	for _, opts := range []nozle.EventBufferOptions{{MaxBatchSize: nozle.MaxTrackBatchChunkSize + 1}, {MaxBatchSize: -1}, {MaxQueueSize: -1}, {MaxAttempts: -1}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%+v did not panic", opts)
				}
			}()
			nozle.New("sk_test").NewEventBuffer(opts)
		}()
	}
}
