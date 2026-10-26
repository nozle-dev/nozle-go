package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

func TestCanWithOptions_sendsSubscriptionAndEntitySelectors(t *testing.T) {
	var got []map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query())
		w.Write([]byte(`{"allowed":false,"reason":"subscription_required","used":0,"limit":null,"remaining":null}`))
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))

	result, err := c.CanWithOptions(context.Background(), "cust", "seats", nozle.CanOptions{SubscriptionID: "sub_a", EntityID: "ent_1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "subscription_required" {
		t.Fatalf("reason = %q", result.Reason)
	}
	if got[0]["subscription_id"][0] != "sub_a" || got[0]["entity_id"][0] != "ent_1" {
		t.Fatalf("query = %v", got[0])
	}
	if _, err := c.CanWithOptions(context.Background(), "cust", "seats", nozle.CanOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got[1]["subscription_id"]; ok {
		t.Fatalf("empty options sent a selector: %v", got[1])
	}
}

func TestTrackWithID_returnsTheGeneratedTransactionID(t *testing.T) {
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/events" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			sent = body["event"].(map[string]any)["transaction_id"].(string)
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))

	id, err := c.TrackWithID(context.Background(), nozle.TrackParams{CustomerID: "c", Event: "e", SubscriptionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || id != sent {
		t.Fatalf("id = %q, sent = %q", id, sent)
	}
	id, _ = c.TrackWithID(context.Background(), nozle.TrackParams{CustomerID: "c", Event: "e", SubscriptionID: "s", TransactionID: "mine"})
	if id != "mine" {
		t.Fatalf("id = %q", id)
	}
}

func TestTrack_subscriptionLookupFailureHasTheEngineCode(t *testing.T) {
	for code, subs := range map[string]string{
		"active_subscription_not_found": `{"subscriptions":[]}`,
		"active_subscription_ambiguous": `{"subscriptions":[{"external_id":"a"},{"external_id":"b"}]}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(subs)) }))
		c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
		err := c.Track(context.Background(), nozle.TrackParams{CustomerID: "c", Event: "e"})
		srv.Close()
		var apiErr *nozle.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 422 || !apiErr.HasCode(code) {
			t.Fatalf("%s: err = %#v", code, err)
		}
	}
}
