package nozle_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

type sentEvent struct {
	TransactionID  string         `json:"transaction_id"`
	Code           string         `json:"code"`
	CustomerID     string         `json:"external_customer_id"`
	SubscriptionID string         `json:"external_subscription_id"`
	Timestamp      string         `json:"timestamp"`
	Properties     map[string]any `json:"properties"`
}

// batchServer records each request's events and lets handle answer it: it returns a status and a JSON body,
// or 0 for a 200 {"events": []}.
type batchServer struct {
	mu       sync.Mutex
	requests [][]sentEvent
	srv      *httptest.Server
}

func newBatchServer(t *testing.T, handle func(call int, events []sentEvent) (int, string)) *batchServer {
	t.Helper()
	b := &batchServer{}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/engine/api/v1/events/batch" || r.Header.Get("Authorization") != "Bearer sk_test" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("batch body is not gzipped")
		}
		unpacked, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Errorf("bad gzip: %v", err)
			return
		}
		raw, _ := io.ReadAll(unpacked)
		var body struct{ Events []sentEvent }
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("bad body: %v", err)
		}
		b.mu.Lock()
		b.requests = append(b.requests, body.Events)
		call := len(b.requests)
		b.mu.Unlock()
		status, answer := 0, ""
		if handle != nil {
			status, answer = handle(call, body.Events)
		}
		if status == 0 {
			status, answer = 200, `{"events":[]}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(answer))
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *batchServer) client(opts ...nozle.Option) *nozle.Client {
	return nozle.New("sk_test", append([]nozle.Option{nozle.WithBaseURL(b.srv.URL + "/engine"), nozle.WithRetryDelay(time.Millisecond)}, opts...)...)
}

func (b *batchServer) sizes() []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []int
	for _, r := range b.requests {
		out = append(out, len(r))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

func ids(events []sentEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.TransactionID
	}
	return out
}

func batchEvents(n int) []nozle.TrackParams {
	out := make([]nozle.TrackParams, n)
	for i := range out {
		out[i] = nozle.TrackParams{CustomerID: "c1", Event: "api_call", Metadata: map[string]any{"i": i}, TransactionID: fmt.Sprintf("tx-%d", i)}
	}
	return out
}

func rejection(positions ...string) (int, string) {
	details := map[string]any{}
	for _, p := range positions {
		details[p] = map[string]any{"code": []string{"value_is_mandatory"}}
	}
	body, _ := json.Marshal(map[string]any{"code": "validation_errors", "error_details": details})
	return 422, string(body)
}

func TestTrackBatchEmptySendsNothing(t *testing.T) {
	b := newBatchServer(t, nil)
	res, err := b.client().TrackBatch(context.Background(), nil)
	if err != nil || res.Total != 0 || len(res.Results) != 0 || len(b.requests) != 0 {
		t.Fatalf("res=%+v err=%v requests=%d", res, err, len(b.requests))
	}
}

func TestTrackBatchChunksAtTheBoundaries(t *testing.T) {
	for _, tc := range []struct {
		count int
		sizes []int
	}{{1, []int{1}}, {5000, []int{5000}}, {5001, []int{5000, 1}}, {12000, []int{5000, 5000, 2000}}} {
		b := newBatchServer(t, nil)
		res, err := b.client().TrackBatch(context.Background(), batchEvents(tc.count))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(b.sizes(), tc.sizes) {
			t.Errorf("%d events: request sizes %v, want %v", tc.count, b.sizes(), tc.sizes)
		}
		if res.Total != tc.count || res.Accepted != tc.count || res.Rejected != 0 || res.Failed != 0 {
			t.Errorf("%d events: %+v", tc.count, res)
		}
		for i, r := range res.Results {
			if r.Index != i || r.Status != nozle.BatchEventAccepted {
				t.Fatalf("result %d = %+v", i, r)
			}
		}
	}
}

func TestTrackBatchBodyMatchesTrackAndReturnsGeneratedIDs(t *testing.T) {
	b := newBatchServer(t, nil)
	res, err := b.client().TrackBatch(context.Background(), []nozle.TrackParams{
		{CustomerID: "c1", Event: "a", SubscriptionID: "s1", Timestamp: "2026-01-02T03:04:05Z", Metadata: map[string]any{"n": 1}},
		{CustomerID: "c2", Event: "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, second := b.requests[0][0], b.requests[0][1]
	if first.CustomerID != "c1" || first.Code != "a" || first.SubscriptionID != "s1" || first.Timestamp != "1767323045.000" || first.Properties["n"] != float64(1) {
		t.Errorf("first = %+v", first)
	}
	if second.Timestamp != "" || second.SubscriptionID != "" || first.TransactionID == second.TransactionID || second.TransactionID == "" {
		t.Errorf("second = %+v", second)
	}
	if res.Results[0].TransactionID != first.TransactionID || res.Results[1].TransactionID != second.TransactionID {
		t.Errorf("results do not carry the sent IDs: %+v", res.Results)
	}
}

func TestTrackBatchOptionsAreBounded(t *testing.T) {
	b := newBatchServer(t, nil)
	if _, err := b.client().TrackBatch(context.Background(), batchEvents(100), nozle.WithBatchChunkSize(40)); err != nil || len(b.requests) != 3 {
		t.Fatalf("err=%v requests=%d", err, len(b.requests))
	}
	for _, opt := range []nozle.TrackBatchOption{nozle.WithBatchChunkSize(0), nozle.WithBatchChunkSize(nozle.MaxTrackBatchChunkSize + 1), nozle.WithBatchConcurrency(0), nozle.WithBatchConcurrency(65)} {
		_, err := b.client().TrackBatch(context.Background(), batchEvents(1), opt)
		var invalid *nozle.BatchEventError
		if !errors.As(err, &invalid) || invalid.Index != -1 {
			t.Errorf("want *BatchEventError, got %v", err)
		}
	}
	if len(b.requests) != 3 {
		t.Errorf("an invalid option sent a request")
	}
}

func TestTrackBatchNeverExceedsConcurrency(t *testing.T) {
	var active, peak int32
	b := newBatchServer(t, func(int, []sentEvent) (int, string) {
		now := atomic.AddInt32(&active, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if now <= p || atomic.CompareAndSwapInt32(&peak, p, now) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return 0, ""
	})
	res, err := b.client().TrackBatch(context.Background(), batchEvents(10), nozle.WithBatchChunkSize(1), nozle.WithBatchConcurrency(3))
	if err != nil || res.Accepted != 10 || len(b.requests) != 10 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if peak != 3 {
		t.Errorf("peak in flight = %d, want 3", peak)
	}
}

func TestTrackBatchRejectsNamedEventsAndResendsTheRest(t *testing.T) {
	b := newBatchServer(t, func(call int, _ []sentEvent) (int, string) {
		if call == 1 {
			return rejection("1")
		}
		return 0, ""
	})
	res, err := b.client().TrackBatch(context.Background(), batchEvents(3))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(b.requests[0]), []string{"tx-0", "tx-1", "tx-2"}) || !reflect.DeepEqual(ids(b.requests[1]), []string{"tx-0", "tx-2"}) {
		t.Errorf("requests = %v", b.requests)
	}
	if res.Accepted != 2 || res.Rejected != 1 || res.Failed != 0 {
		t.Errorf("res = %+v", res)
	}
	got := res.Results[1]
	if got.Status != nozle.BatchEventRejected || got.TransactionID != "tx-1" || !reflect.DeepEqual(got.Errors, map[string][]string{"code": {"value_is_mandatory"}}) {
		t.Errorf("rejected = %+v", got)
	}
}

func TestTrackBatchPositionsAreThoseOfTheWholeInput(t *testing.T) {
	b := newBatchServer(t, func(_ int, events []sentEvent) (int, string) {
		for i, id := range ids(events) {
			if id == "tx-103" {
				return rejection(fmt.Sprint(i))
			}
		}
		return 0, ""
	})
	res, err := b.client().TrackBatch(context.Background(), batchEvents(150))
	if err != nil {
		t.Fatal(err)
	}
	if res.Results[103].Status != nozle.BatchEventRejected || res.Accepted != 149 || res.Rejected != 1 || res.Failed != 0 {
		t.Errorf("res = %+v, result 103 = %+v", res, res.Results[103])
	}
}

func TestTrackBatchOverdueRefusalsInA200AreRejected(t *testing.T) {
	b := newBatchServer(t, func(int, []sentEvent) (int, string) {
		return 200, `{"events":[],"error_details":{"2":{"external_customer_id":["customer_payment_overdue"]}}}`
	})
	res, err := b.client().TrackBatch(context.Background(), batchEvents(3))
	if err != nil || len(b.requests) != 1 {
		t.Fatalf("err=%v requests=%d", err, len(b.requests))
	}
	if res.Accepted != 2 || res.Rejected != 1 || !reflect.DeepEqual(res.Results[2].Errors, map[string][]string{"external_customer_id": {"customer_payment_overdue"}}) {
		t.Errorf("res = %+v", res)
	}
}

func TestTrackBatchA402RefusingEveryEventDoesNotResend(t *testing.T) {
	refusal := `{"external_customer_id":["customer_payment_overdue"]}`
	b := newBatchServer(t, func(int, []sentEvent) (int, string) {
		return 402, `{"code":"customer_payment_overdue","error_details":{"0":` + refusal + `,"1":` + refusal + `}}`
	})
	res, err := b.client().TrackBatch(context.Background(), batchEvents(2))
	if err != nil || res.Rejected != 2 || res.Failed != 0 || len(b.requests) != 1 {
		t.Fatalf("res=%+v err=%v requests=%d", res, err, len(b.requests))
	}
}

func TestTrackBatchRetryCarriesTheSameTransactionIDs(t *testing.T) {
	b := newBatchServer(t, func(call int, _ []sentEvent) (int, string) {
		if call < 3 {
			return 503, `{}`
		}
		return 0, ""
	})
	res, err := b.client(nozle.WithMaxRetries(2)).TrackBatch(context.Background(), []nozle.TrackParams{{CustomerID: "c1", Event: "a"}, {CustomerID: "c1", Event: "a"}})
	if err != nil || len(b.requests) != 3 {
		t.Fatalf("err=%v requests=%d", err, len(b.requests))
	}
	first := ids(b.requests[0])
	if !reflect.DeepEqual(ids(b.requests[1]), first) || !reflect.DeepEqual(ids(b.requests[2]), first) {
		t.Errorf("retries changed the IDs: %v", b.requests)
	}
	if res.Accepted != 2 || res.Results[0].TransactionID != first[0] || res.Results[1].TransactionID != first[1] {
		t.Errorf("res = %+v", res)
	}
}

func TestTrackBatchAFailedChunkDoesNotLoseTheOthers(t *testing.T) {
	b := newBatchServer(t, func(_ int, events []sentEvent) (int, string) {
		for _, id := range ids(events) {
			if id == "tx-100" {
				return 500, `{"error":"boom"}`
			}
		}
		return 0, ""
	})
	res, err := b.client(nozle.WithMaxRetries(0)).TrackBatch(context.Background(), batchEvents(250), nozle.WithBatchChunkSize(100))
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 250 || res.Accepted != 150 || res.Rejected != 0 || res.Failed != 100 {
		t.Fatalf("res = %+v", res)
	}
	for i, r := range res.Results {
		failed := i >= 100 && i < 200
		var api *nozle.APIError
		if failed != (r.Status == nozle.BatchEventFailed) || (failed && (!errors.As(r.Err, &api) || api.StatusCode != 500)) {
			t.Fatalf("result %d = %+v", i, r)
		}
	}
}

func TestTrackBatchTransportErrorFailsTheChunkWithItsIDs(t *testing.T) {
	b := newBatchServer(t, nil)
	b.srv.Close()
	res, err := b.client(nozle.WithMaxRetries(0)).TrackBatch(context.Background(), batchEvents(2))
	var transport *nozle.TransportError
	if err != nil || res.Failed != 2 || res.Results[0].TransactionID != "tx-0" || !errors.As(res.Results[0].Err, &transport) {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestTrackBatchA422WithoutPositionsFailsTheChunk(t *testing.T) {
	b := newBatchServer(t, func(int, []sentEvent) (int, string) {
		return 422, `{"code":"validation_errors","error_details":{"events":["no_events"]}}`
	})
	res, err := b.client().TrackBatch(context.Background(), batchEvents(2))
	if err != nil || res.Rejected != 0 || res.Failed != 2 || len(b.requests) != 1 {
		t.Fatalf("res=%+v err=%v requests=%d", res, err, len(b.requests))
	}
}

func TestTrackBatchSplitsARequestAboveTheEngineMaximum(t *testing.T) {
	// An Engine that takes at most 2 events per request.
	b := newBatchServer(t, func(_ int, events []sentEvent) (int, string) {
		if len(events) > 2 {
			return 422, `{"code":"validation_errors","error_details":{"events":["too_many_events"]}}`
		}
		return 0, ""
	})
	res, err := b.client().TrackBatch(context.Background(), batchEvents(5))
	if err != nil || res.Accepted != 5 || res.Failed != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := b.sizes(); !reflect.DeepEqual(got, []int{5, 3, 2, 2, 1}) {
		t.Fatalf("request sizes %v", got)
	}
	single := newBatchServer(t, func(int, []sentEvent) (int, string) {
		return 422, `{"code":"validation_errors","error_details":{"events":["too_many_events"]}}`
	})
	if res, err = single.client().TrackBatch(context.Background(), batchEvents(1)); err != nil || res.Failed != 1 || len(single.requests) != 1 {
		t.Fatalf("a single refused event fails: res=%+v err=%v", res, err)
	}
}

func TestTrackBatchStopsResendingAfterThreeRequests(t *testing.T) {
	b := newBatchServer(t, func(int, []sentEvent) (int, string) { return rejection("0") })
	res, err := b.client().TrackBatch(context.Background(), batchEvents(5))
	if err != nil || len(b.requests) != 3 || res.Rejected != 3 || res.Failed != 2 {
		t.Fatalf("res=%+v err=%v requests=%d", res, err, len(b.requests))
	}
}

func TestTrackBatchInvalidInputFailsBeforeAnyRequest(t *testing.T) {
	b := newBatchServer(t, nil)
	same := nozle.TrackParams{CustomerID: "c", Event: "a", TransactionID: "same"}
	for _, tc := range []struct {
		events []nozle.TrackParams
		index  int
	}{
		{append(batchEvents(1), nozle.TrackParams{Event: "a"}), 1},
		{[]nozle.TrackParams{{CustomerID: "c", Event: " "}}, 0},
		{[]nozle.TrackParams{{CustomerID: "c", Event: "a", Timestamp: "yesterday"}}, 0},
		{[]nozle.TrackParams{same, same}, 1},
	} {
		_, err := b.client().TrackBatch(context.Background(), tc.events)
		var invalid *nozle.BatchEventError
		var validation *nozle.ValidationError
		if !errors.As(err, &invalid) || invalid.Index != tc.index || !errors.As(err, &validation) || !strings.Contains(err.Error(), "track_batch") {
			t.Errorf("events %+v: err = %v", tc.events, err)
		}
	}
	if _, err := nozle.New("pk_test", nozle.WithBaseURL(b.srv.URL+"/engine")).TrackBatch(context.Background(), batchEvents(1)); err == nil {
		t.Error("a publishable key must be refused")
	}
	if len(b.requests) != 0 {
		t.Errorf("%d requests were sent", len(b.requests))
	}
}
