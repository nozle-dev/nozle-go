package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

const trialOK = `{"subscription":{"external_id":"sub/1","status":"active","trial_ends_at":"2026-12-01T00:00:00Z","trial_ended_at":null,"in_trial":true}}`

func trialServer(t *testing.T, status int, reply string, onRequest func(r *http.Request, body map[string]any)) (*nozle.Client, *int32) {
	t.Helper()
	var calls int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if onRequest != nil {
			onRequest(r, body)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(s.Close)
	return nozle.New("sk_test", nozle.WithHost(s.URL), nozle.WithMaxRetries(2), nozle.WithRetryDelay(1)), &calls
}

func TestUpdateSubscriptionSendsEachTrialForm(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		update nozle.TrialUpdate
		want   map[string]any
	}{
		{nozle.TrialUpdate{ExtendDays: 7}, map[string]any{"extend_days": float64(7)}},
		{nozle.TrialUpdate{EndsAt: "2026-12-01T00:00:00Z", Reason: "call"}, map[string]any{"ends_at": "2026-12-01T00:00:00Z", "reason": "call"}},
		{nozle.TrialUpdate{Convert: true}, map[string]any{"convert": true}},
	} {
		var got map[string]any
		var path, method string
		c, _ := trialServer(t, 200, trialOK, func(r *http.Request, body map[string]any) {
			got, path, method = body, r.URL.EscapedPath(), r.Method
		})
		out, err := c.UpdateSubscription(ctx, "sub/1", tc.update)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"subscription": map[string]any{"trial": tc.want}}
		if !reflect.DeepEqual(got, want) || method != "PUT" || path != "/core/api/v1/subscriptions/sub%2F1" {
			t.Fatalf("%s %s %v, want %v", method, path, got, want)
		}
		if !out.Subscription.InTrial || out.Subscription.TrialEndsAt == nil || *out.Subscription.TrialEndsAt != "2026-12-01T00:00:00Z" || out.Subscription.TrialEndedAt != nil {
			t.Fatalf("result = %+v", out)
		}
	}
}

func TestUpdateSubscriptionRejectsBadTrialBeforeSending(t *testing.T) {
	c, calls := trialServer(t, 200, trialOK, nil)
	ctx := context.Background()
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'x'
	}
	for name, update := range map[string]nozle.TrialUpdate{
		"none":             {},
		"reason only":      {Reason: "x"},
		"days and end":     {ExtendDays: 3, EndsAt: "2026-12-01T00:00:00Z"},
		"days and convert": {ExtendDays: 3, Convert: true},
		"negative days":    {ExtendDays: -1},
		"bad end":          {EndsAt: "tomorrow"},
		"long reason":      {Convert: true, Reason: string(long)},
	} {
		var verr *nozle.ValidationError
		if _, err := c.UpdateSubscription(ctx, "sub_1", update); !errors.As(err, &verr) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	var verr *nozle.ValidationError
	if _, err := c.UpdateSubscription(ctx, "", nozle.TrialUpdate{Convert: true}); !errors.As(err, &verr) {
		t.Errorf("empty id: %v", err)
	}
	pk := nozle.New("pk_x")
	if _, err := pk.ConvertTrial(ctx, "sub_1", ""); err == nil {
		t.Error("publishable key accepted")
	}
	if _, err := c.ExtendTrial(ctx, "sub_1", 0, "", ""); !errors.As(err, &verr) {
		t.Errorf("extend with nothing: %v", err)
	}
	if _, err := c.ExtendTrial(ctx, "sub_1", 1, "2026-12-01T00:00:00Z", ""); !errors.As(err, &verr) {
		t.Errorf("extend with both: %v", err)
	}
	if *calls != 0 {
		t.Fatalf("%d requests sent", *calls)
	}
}

func TestExtendAndConvertTrialHelpers(t *testing.T) {
	var got map[string]any
	c, _ := trialServer(t, 200, trialOK, func(_ *http.Request, body map[string]any) { got = body })
	ctx := context.Background()
	if _, err := c.ExtendTrial(ctx, "sub/1", 14, "", "sales call"); err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"subscription": map[string]any{"trial": map[string]any{"extend_days": float64(14), "reason": "sales call"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("extend body %v", got)
	}
	if _, err := c.ExtendTrial(ctx, "sub/1", 0, "2026-12-01T00:00:00Z", ""); err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"subscription": map[string]any{"trial": map[string]any{"ends_at": "2026-12-01T00:00:00Z"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("extend body %v", got)
	}
	if _, err := c.ConvertTrial(ctx, "sub/1", "customer asked"); err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"subscription": map[string]any{"trial": map[string]any{"convert": true, "reason": "customer asked"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("convert body %v", got)
	}
}

func TestTrialErrorCodes(t *testing.T) {
	ctx := context.Background()
	for _, code := range []string{
		nozle.CodeTrialInvalidParams, nozle.CodeTrialNotActive, nozle.CodeTrialExtensionTooLong,
		nozle.CodeTrialEndInPast, nozle.CodePaymentMethodRequired,
	} {
		c, _ := trialServer(t, 422, `{"code":"validation_errors","error_details":{"trial":["`+code+`"]}}`, nil)
		_, err := c.ConvertTrial(ctx, "sub_1", "")
		var api *nozle.APIError
		if !errors.As(err, &api) || api.StatusCode != 422 || !api.HasCode(code) {
			t.Fatalf("%s: %v", code, err)
		}
		other := nozle.CodeTrialNotActive
		if code == other {
			other = nozle.CodeTrialEndInPast
		}
		if api.HasCode(other) {
			t.Fatalf("%s also matched %s", code, other)
		}
	}
	c, _ := trialServer(t, 409, `{"code":"plan_change_pending"}`, nil)
	_, err := c.ConvertTrial(ctx, "sub_1", "")
	var api *nozle.APIError
	if !errors.As(err, &api) || api.StatusCode != 409 || !api.HasCode(nozle.CodePlanChangePending) {
		t.Fatalf("409: %v", err)
	}
}

func TestTrialUpdateIsNotRetried(t *testing.T) {
	c, calls := trialServer(t, 503, `{}`, nil)
	if _, err := c.ExtendTrial(context.Background(), "sub_1", 3, "", ""); err == nil {
		t.Fatal("503 accepted")
	}
	if *calls != 1 {
		t.Fatalf("%d requests, want 1", *calls)
	}
}
