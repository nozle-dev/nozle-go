package nozle_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

const voidOK = `{"void":{"transaction_id":"tx/123","status":"voided","reason":"wrong quantity","voided_at":"2026-11-01T12:00:00Z","actor":"api_key:k1","effects":{"event":"voided","usage":"reversed","credits":"not_applicable","cost":"pending","margin":"pending"}}}`

func TestEventsVoidPostsReasonAndDecodesResult(t *testing.T) {
	var got map[string]any
	var path, method string
	c, _ := trialServer(t, 200, voidOK, func(r *http.Request, body map[string]any) {
		got, path, method = body, r.URL.EscapedPath(), r.Method
	})
	out, err := c.Events.Void(context.Background(), "tx/123", "wrong quantity")
	if err != nil {
		t.Fatal(err)
	}
	if method != "POST" || path != "/engine/api/v1/events/tx%2F123/void" || !reflect.DeepEqual(got, map[string]any{"reason": "wrong quantity"}) {
		t.Fatalf("%s %s %v", method, path, got)
	}
	want := nozle.EventVoid{TransactionID: "tx/123", Status: "voided", Reason: "wrong quantity", VoidedAt: "2026-11-01T12:00:00Z", Actor: "api_key:k1",
		Effects: nozle.EventVoidEffects{Event: "voided", Usage: "reversed", Credits: "not_applicable", Cost: "pending", Margin: "pending"}}
	if *out != want {
		t.Fatalf("result = %+v", *out)
	}
}

func TestEventsVoidValidatesBeforeSending(t *testing.T) {
	c, calls := trialServer(t, 200, voidOK, nil)
	ctx := context.Background()
	var verr *nozle.ValidationError
	for name, args := range map[string][2]string{"empty id": {"", "x"}, "empty reason": {"tx", ""}, "long reason": {"tx", strings.Repeat("x", 501)}} {
		if _, err := c.Events.Void(ctx, args[0], args[1]); !errors.As(err, &verr) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := nozle.New("pk_x").Events.Void(ctx, "tx", "x"); err == nil {
		t.Error("publishable key accepted")
	}
	if *calls != 0 {
		t.Fatalf("%d requests sent", *calls)
	}
}

func TestEventsVoidErrorCodes(t *testing.T) {
	for status, code := range map[int]string{404: nozle.CodeEventNotFound, 409: nozle.CodePeriodInvoiced, 422: nozle.CodeReasonRequired} {
		c, _ := trialServer(t, status, `{"error":"`+code+`","message":"no"}`, nil)
		_, err := c.Events.Void(context.Background(), "tx", "x")
		var api *nozle.APIError
		if !errors.As(err, &api) || api.StatusCode != status || !api.HasCode(code) || api.HasCode(nozle.CodeTransactionVoided) {
			t.Errorf("%d: err = %v", status, err)
		}
	}
}

func TestEventsVoidRetriesServerErrors(t *testing.T) {
	n := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n++; n == 1 {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte(voidOK))
	}))
	defer s.Close()
	c := nozle.New("sk_test", nozle.WithHost(s.URL), nozle.WithMaxRetries(2), nozle.WithRetryDelay(1))
	if _, err := c.Events.Void(context.Background(), "tx", "x"); err != nil || n != 2 {
		t.Fatalf("err = %v after %d requests", err, n)
	}
}
