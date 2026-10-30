package nozle_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

func TestEventWritesUseEngineAndSubscriptionDiscoveryUsesCore(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer sk_test" {
			t.Error("missing authentication")
		}
		switch r.URL.Path {
		case "/core/api/v1/subscriptions":
			if r.URL.Query().Get("external_customer_id") != "customer" {
				t.Error("incorrect subscription lookup")
			}
			w.Write([]byte(`{"subscriptions":[{"external_id":"subscription"}]}`))
		case "/engine/api/v1/events", "/engine/api/v1/events/batch":
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body["event"]) == 0 && len(body["events"]) == 0 {
				t.Error("missing event envelope")
			}
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(server.URL+"/engine/"), nozle.WithEventsURL(server.URL+"/core/"))
	ctx := context.Background()
	if err := c.Track(ctx, nozle.TrackParams{CustomerID: "customer", Event: "requests", TransactionID: "single"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Meter(ctx, []nozle.MeterEvent{{CustomerID: "customer", SubscriptionID: "subscription", Event: "requests", TransactionID: "batch"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /engine/api/v1/events", "POST /engine/api/v1/events/batch"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
