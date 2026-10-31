package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

type holdRequest struct {
	method, path, rawQuery, key string
	body                        map[string]any
}

// holdServer records each request and answers with the next status and body.
func holdServer(t *testing.T, answers ...string) (*httptest.Server, *[]holdRequest) {
	t.Helper()
	var got []holdRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk_test-key" {
			t.Errorf("missing authentication on %s %s", r.Method, r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("invalid JSON body: %v", err)
			}
		}
		got = append(got, holdRequest{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Idempotency-Key"), body})
		answer := answers[0]
		if len(answers) > 1 {
			answers = answers[1:]
		}
		status := http.StatusOK
		if answer[0] != '{' {
			status = map[string]int{"404": 404, "409": 409, "503": 503}[answer[:3]]
			answer = answer[3:]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func bodyKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

const holdJSON = `{"hold_id":"hold_1","status":"held","customer_id":"cust","entity_id":"user-42","subscription_id":"sub_1",
"feature_code":"agent_execution","credit_system":"ai_credits","pool_policy":"entity_then_customer","metric_amount":"3",
"credits_required":"1.500000000001","credits_held":"1.500000000001","expires_at":"2026-10-31T10:15:00Z",
"created_at":"2026-10-31T10:00:00Z","resolved_at":null`

func TestCreateCreditHold_sendsTheEngineBodyAndDecodesTheHold(t *testing.T) {
	srv, got := holdServer(t, holdJSON+`,"allowed":true,"available":"8.5"}`)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	hold, err := c.CreateCreditHold(context.Background(), nozle.CreditHoldParams{
		CustomerID: "cust", EntityID: "user-42", FeatureCode: "agent_execution",
		CreditSystemCode: "ai_credits", Properties: map[string]any{"tokens": 3}, Timestamp: "2026-10-31T10:00:00Z", TTLSeconds: 60,
	}, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	r := (*got)[0]
	if r.method != "POST" || r.path != "/api/v1/usage/holds" || r.key != "run-1" {
		t.Fatalf("request = %s %s key %q", r.method, r.path, r.key)
	}
	want := map[string]any{
		"customer_id": "cust", "entity_id": "user-42", "feature_code": "agent_execution",
		"credit_system_code": "ai_credits", "properties": map[string]any{"tokens": float64(3)},
		"timestamp": "2026-10-31T10:00:00Z", "ttl_seconds": float64(60),
	}
	if !reflect.DeepEqual(r.body, want) {
		t.Fatalf("body = %v", r.body)
	}
	if !hold.Allowed || hold.HoldID != "hold_1" || hold.Status != nozle.CreditHoldStatusHeld || hold.CreditsHeld != "1.500000000001" ||
		hold.Available != "8.5" || hold.PoolPolicy != "entity_then_customer" || hold.ResolvedAt != "" || hold.ExpiresAt != "2026-10-31T10:15:00Z" {
		t.Fatalf("hold = %+v", hold)
	}
}

func TestCreateCreditHold_leavesOutUnsetFieldsAndReusesItsTimestampForAKey(t *testing.T) {
	srv, got := holdServer(t, `{"hold_id":"hold_2","status":"denied","allowed":false,"reason":"insufficient_credits","credits_held":"0","available":"0"}`)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	p := nozle.CreditHoldParams{CustomerID: "cust", FeatureCode: "agent_execution"}
	for _, key := range []string{"k1", "k1", "k2"} {
		hold, err := c.CreateCreditHold(context.Background(), p, key)
		if err != nil {
			t.Fatal(err)
		}
		if hold.Allowed || hold.Reason != "insufficient_credits" || hold.Status != nozle.CreditHoldStatusDenied {
			t.Fatalf("a denial must be a result, got %+v", hold)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if k := bodyKeys((*got)[0].body); !reflect.DeepEqual(k, []string{"customer_id", "feature_code", "properties", "timestamp"}) {
		t.Fatalf("keys = %v", k)
	}
	stamps := []any{(*got)[0].body["timestamp"], (*got)[1].body["timestamp"], (*got)[2].body["timestamp"]}
	if stamps[0] != stamps[1] || stamps[2] == stamps[0] {
		t.Fatalf("timestamps = %v", stamps)
	}
}

func TestCreateCreditHold_refusesBadArgumentsBeforeAnyRequest(t *testing.T) {
	srv, got := holdServer(t, `{}`)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	p := nozle.CreditHoldParams{CustomerID: "cust", FeatureCode: "f"}
	var verr *nozle.ValidationError
	if _, err := c.CreateCreditHold(context.Background(), p, ""); !errors.As(err, &verr) {
		t.Fatalf("missing key: %v", err)
	}
	for _, ttl := range []int{-1, 86401} {
		p.TTLSeconds = ttl
		if _, err := c.CreateCreditHold(context.Background(), p, "k"); !errors.As(err, &verr) {
			t.Fatalf("ttl %d: %v", ttl, err)
		}
	}
	var auth *nozle.AuthenticationError
	if _, err := nozle.New("pk_browser", nozle.WithBaseURL(srv.URL)).CreateCreditHold(context.Background(), p, "k"); !errors.As(err, &auth) {
		t.Fatalf("publishable key: %v", err)
	}
	if _, err := c.FinalizeCreditHold(context.Background(), " ", nozle.FinalizeCreditHoldParams{}); !errors.As(err, &verr) {
		t.Fatalf("finalize without id: %v", err)
	}
	if _, err := c.ReleaseCreditHold(context.Background(), ""); !errors.As(err, &verr) {
		t.Fatalf("release without id: %v", err)
	}
	if _, err := c.GetCreditHold(context.Background(), ""); !errors.As(err, &verr) {
		t.Fatalf("get without id: %v", err)
	}
	if _, err := c.ListCreditHolds(context.Background(), "cust", "open"); !errors.As(err, &verr) {
		t.Fatalf("list with an unknown status: %v", err)
	}
	if len(*got) != 0 {
		t.Fatalf("sent %d requests", len(*got))
	}
}

func TestFinalizeCreditHold_sendsTheActualUsageAndIsRetried(t *testing.T) {
	srv, got := holdServer(t, `503{"code":"unavailable"}`, `{"allowed":true,"operation_id":"op_1","metric_amount":"2","credits_required":"1",
"credits_consumed":"1","remaining":"9","hold_id":"hold/1","hold_status":"finalized","credits_held":"1.5","credits_released":"0.5","hold_expired":false}`)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithMaxRetries(1), nozle.WithRetryDelay(time.Millisecond))
	result, err := c.FinalizeCreditHold(context.Background(), "hold/1", nozle.FinalizeCreditHoldParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 2 {
		t.Fatalf("requests = %d, want a retry", len(*got))
	}
	r := (*got)[1]
	if r.method != "POST" || r.path != "/api/v1/usage/holds/hold%2F1/finalize" || r.key != "" {
		t.Fatalf("request = %s %s key %q", r.method, r.path, r.key)
	}
	if k := bodyKeys(r.body); !reflect.DeepEqual(k, []string{"properties", "timestamp"}) || !reflect.DeepEqual(r.body["properties"], map[string]any{}) {
		t.Fatalf("body = %v", r.body)
	}
	if (*got)[0].body["timestamp"] != r.body["timestamp"] {
		t.Fatal("a retry must resend the same timestamp")
	}
	if !result.Allowed || result.OperationID != "op_1" || result.CreditsConsumed != "1" || result.HoldID != "hold/1" ||
		result.HoldStatus != nozle.CreditHoldStatusFinalized || result.CreditsReleased != "0.5" || result.HoldExpired {
		t.Fatalf("result = %+v", result)
	}
	if _, err := c.FinalizeCreditHold(context.Background(), "hold/1", nozle.FinalizeCreditHoldParams{}); err != nil {
		t.Fatal(err)
	}
	if (*got)[2].body["timestamp"] != r.body["timestamp"] {
		t.Fatal("finalizing the same hold again must reuse its timestamp so Engine replays the result")
	}
}

func TestReleaseGetAndListCreditHolds(t *testing.T) {
	srv, got := holdServer(t,
		`409{"code":"hold_already_resolved","message":"credit hold is already resolved"}`,
		`404{"code":"hold_not_found","message":"credit hold was not found"}`,
		holdJSON+`}`,
		`{"customer_id":"cust/1","holds":[`+holdJSON+`}]}`,
		`{"customer_id":"cust/1","holds":[]}`,
	)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	ctx := context.Background()
	var apiErr *nozle.APIError
	if _, err := c.ReleaseCreditHold(ctx, "hold_1"); !errors.As(err, &apiErr) || apiErr.StatusCode != 409 || apiErr.Code() != nozle.CodeHoldAlreadyResolved {
		t.Fatalf("release: %v", err)
	}
	if _, err := c.GetCreditHold(ctx, "missing"); !errors.As(err, &apiErr) || apiErr.StatusCode != 404 || apiErr.Code() != nozle.CodeHoldNotFound {
		t.Fatalf("get: %v", err)
	}
	hold, err := c.GetCreditHold(ctx, "hold_1")
	if err != nil || hold.HoldID != "hold_1" || hold.MetricAmount != "3" {
		t.Fatalf("get = %+v, %v", hold, err)
	}
	list, err := c.ListCreditHolds(ctx, "cust/1", nozle.CreditHoldStatusHeld)
	if err != nil || list.CustomerID != "cust/1" || len(list.Holds) != 1 || list.Holds[0].CreditsHeld != "1.500000000001" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := c.ListCreditHolds(ctx, "cust/1", ""); err != nil {
		t.Fatal(err)
	}
	want := []holdRequest{
		{"POST", "/api/v1/usage/holds/hold_1/release", "", "", nil},
		{"GET", "/api/v1/usage/holds/missing", "", "", nil},
		{"GET", "/api/v1/usage/holds/hold_1", "", "", nil},
		{"GET", "/api/v1/customers/cust%2F1/credit-holds", "status=held", "", nil},
		{"GET", "/api/v1/customers/cust%2F1/credit-holds", "", "", nil},
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("requests = %+v", *got)
	}
}

func TestCreditBalances_decodeHeldCredits(t *testing.T) {
	srv, _ := holdServer(t,
		`{"customer_id":"cust","credit_system":"ai_credits","available":"8","held":"2","sources":[{"id":"s1","remaining":"10","held":"2"}]}`,
		`{"customer_id":"cust","entity_id":"user-42","credit_system":"ai_credits","effective_available":"5","entity_held":"1","shared_held":"0.5","held":"1.5"}`,
	)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	balance, err := c.GetCreditBalance(context.Background(), "cust", "ai_credits")
	if err != nil || balance.Held != "2" || balance.Sources[0].Held != "2" {
		t.Fatalf("balance = %+v, %v", balance, err)
	}
	entity, err := c.GetEntityCreditBalance(context.Background(), "cust", "user-42", "ai_credits")
	if err != nil || entity.EntityHeld != "1" || entity.SharedHeld != "0.5" || entity.Held != "1.5" {
		t.Fatalf("entity balance = %+v, %v", entity, err)
	}
}
