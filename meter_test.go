package nozle_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

// meterServer records every batch body. handle answers attempt n (1-based) itself and returns true,
// or returns false for a 200.
func meterServer(t *testing.T, handle func(attempt int, w http.ResponseWriter, r *http.Request) bool) (*httptest.Server, func() [][]byte) {
	t.Helper()
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, b)
		attempt := len(bodies)
		mu.Unlock()
		if handle(attempt, w, r) {
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, func() [][]byte { mu.Lock(); defer mu.Unlock(); return append([][]byte(nil), bodies...) }
}

func sentTransactionIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var payload struct {
		Events []struct {
			TransactionID string `json:"transaction_id"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	ids := make([]string, len(payload.Events))
	for i, e := range payload.Events {
		ids[i] = e.TransactionID
	}
	return ids
}

func meterBatch() []nozle.MeterEvent {
	return []nozle.MeterEvent{
		{CustomerID: "c1", SubscriptionID: "s1", Event: "api_call", TransactionID: "caller-tx"},
		{CustomerID: "c1", SubscriptionID: "s1", Event: "api_call", Metadata: map[string]any{"n": 1}},
		{CustomerID: "c2", SubscriptionID: "s2", Event: "api_call"},
	}
}

// checkIDs asserts the Engine dedup contract for one batch: every event has a transaction ID, IDs are
// unique within the batch (Engine rejects a repeated one), the caller's own ID is kept, and every
// generated ID was written back into the slice.
func checkIDs(t *testing.T, events []nozle.MeterEvent, sent []string) {
	t.Helper()
	if len(sent) != len(events) {
		t.Fatalf("sent %d events, want %d", len(sent), len(events))
	}
	seen := map[string]bool{}
	for i, id := range sent {
		if id == "" || seen[id] {
			t.Fatalf("transaction IDs must be non-empty and unique in a batch: %v", sent)
		}
		seen[id] = true
		if events[i].TransactionID != id {
			t.Fatalf("events[%d].TransactionID = %q, sent %q", i, events[i].TransactionID, id)
		}
	}
	if sent[0] != "caller-tx" {
		t.Fatalf("caller transaction ID replaced: %q", sent[0])
	}
}

func TestMeter_inSDKRetrySendsSameBatch(t *testing.T) {
	srv, bodies := meterServer(t, func(attempt int, w http.ResponseWriter, _ *http.Request) bool {
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}
		return false
	})
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL), nozle.WithMaxRetries(2), nozle.WithRetryDelay(time.Millisecond))
	events := meterBatch()
	if err := c.Meter(context.Background(), events); err != nil {
		t.Fatalf("Meter: %v", err)
	}
	got := bodies()
	if len(got) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(got))
	}
	// Engine treats a repeated transaction ID with the same code, subscription, properties and
	// timestamp as a replay of the stored event, so the retry must be the same batch byte for byte.
	if string(got[0]) != string(got[1]) {
		t.Fatalf("retry changed the batch:\n%s\n%s", got[0], got[1])
	}
	checkIDs(t, events, sentTransactionIDs(t, got[1]))
}

func TestMeter_callerRetryReusesGeneratedIDs(t *testing.T) {
	cases := map[string]func(t *testing.T, c *nozle.Client, events []nozle.MeterEvent){
		// The server answers 503 and the client has no in-SDK retries.
		"server error": func(t *testing.T, c *nozle.Client, events []nozle.MeterEvent) {
			if err := c.Meter(context.Background(), events); err == nil {
				t.Fatal("want an error from the first call")
			}
		},
		// The server received the batch (and may have stored it) but the caller's context ended first.
		"context timeout": func(t *testing.T, c *nozle.Client, events []nozle.MeterEvent) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := c.Meter(ctx, events); err == nil {
				t.Fatal("want an error from the first call")
			}
		},
	}
	for name, firstCall := range cases {
		t.Run(name, func(t *testing.T) {
			srv, bodies := meterServer(t, func(attempt int, w http.ResponseWriter, r *http.Request) bool {
				if attempt != 1 {
					return false
				}
				if name == "context timeout" {
					select {
					case <-r.Context().Done():
					case <-time.After(5 * time.Second):
					}
					return true
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				return true
			})
			c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL), nozle.WithMaxRetries(0))
			events := meterBatch()
			firstCall(t, c, events)
			if err := c.Meter(context.Background(), events); err != nil {
				t.Fatalf("retry: %v", err)
			}
			got := bodies()
			if len(got) != 2 {
				t.Fatalf("want 2 requests, got %d", len(got))
			}
			if string(got[0]) != string(got[1]) {
				t.Fatalf("caller retry changed the batch:\n%s\n%s", got[0], got[1])
			}
			checkIDs(t, events, sentTransactionIDs(t, got[1]))
		})
	}
}

func TestMeter_freshSliceGetsNewIDs(t *testing.T) {
	srv, bodies := meterServer(t, func(int, http.ResponseWriter, *http.Request) bool { return false })
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
	for range 2 {
		if err := c.Meter(context.Background(), meterBatch()); err != nil {
			t.Fatalf("Meter: %v", err)
		}
	}
	got := bodies()
	first, second := sentTransactionIDs(t, got[0]), sentTransactionIDs(t, got[1])
	if first[1] == second[1] || first[2] == second[2] {
		t.Fatalf("separate batches must get separate generated IDs: %v %v", first, second)
	}
}

func TestMeterWithResultReturnsAcceptedEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"events":[{"transaction_id":"t1","code":"api_call","external_customer_id":"c1","external_subscription_id":"s1"}]}`))
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	res, err := c.MeterWithResult(context.Background(), []nozle.MeterEvent{{CustomerID: "c1", SubscriptionID: "s1", Event: "api_call"}})
	if err != nil || len(res.Events) != 1 || res.Events[0].TransactionID != "t1" || res.Events[0].Code != "api_call" {
		t.Fatalf("result %+v err %v", res, err)
	}
}

func TestMeterBatchRejectedSurfacesIndexErrorsAndCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"status":422,"code":"validation_errors","error_details":{"1":{"timestamp":["invalid_format"]}}}`))
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	res, err := c.MeterWithResult(context.Background(), meterBatch())
	var api *nozle.APIError
	if res != nil || !nozle.AsAPIError(err, &api) {
		t.Fatalf("res %v err %v", res, err)
	}
	if api.Code() != "validation_errors" || !strings.Contains(err.Error(), "(validation_errors)") {
		t.Fatalf("code missing from %q", err)
	}
	if be := api.BatchErrors(); len(be) != 1 || be[1] == nil {
		t.Fatalf("batch errors %v", be)
	}
}

func TestCheckoutAmountDueOmittedVersusZero(t *testing.T) {
	var omitted, zero nozle.CheckoutResult
	if err := json.Unmarshal([]byte(`{"type":"x"}`), &omitted); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"amount_due_cents":0}`), &zero); err != nil {
		t.Fatal(err)
	}
	if omitted.AmountDueCents != "" || zero.AmountDueCents != "0" {
		t.Fatalf("omitted %q zero %q", omitted.AmountDueCents, zero.AmountDueCents)
	}
}
