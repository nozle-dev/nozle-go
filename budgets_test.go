package nozle_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

const budgetJSON = `{"feature_code":"agent_execution","limit":"50","spent":"49.5","held":"0.5","remaining":"0",
"currency":"USD","period":"billing_period","period_start":"2026-10-01T00:00:00Z","period_end":"2026-11-01T00:00:00Z",
"mode":"hard","status":"exceeded","as_of":"2026-10-31T11:59:58.123Z"}`

var wantBudget = nozle.Budget{
	FeatureCode: "agent_execution", Limit: "50", Spent: "49.5", Held: "0.5", Remaining: "0", Currency: "USD",
	Period: "billing_period", PeriodStart: "2026-10-01T00:00:00Z", PeriodEnd: "2026-11-01T00:00:00Z",
	Mode: "hard", Status: "exceeded", AsOf: "2026-10-31T11:59:58.123Z",
}

func answer(t *testing.T, status int, body string) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var got []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestCan_budgetExceededIsADenialWithTheBudget(t *testing.T) {
	srv, _ := answer(t, 200, `{"allowed":false,"reason":"budget_exceeded","used":0,"limit":null,"remaining":null,"budget":`+budgetJSON+`}`)
	res, err := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL)).Can(context.Background(), "acme", "agent_execution")
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed || res.Reason != nozle.RefusalReasonBudgetExceeded || res.Budget == nil || *res.Budget != wantBudget {
		t.Fatalf("got %+v", res)
	}
}

func TestCan_unknownReasonPassesThrough(t *testing.T) {
	srv, _ := answer(t, 200, `{"allowed":false,"reason":"some_new_reason","used":0,"limit":null,"remaining":null}`)
	res, err := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL)).Can(context.Background(), "acme", "f")
	if err != nil || res.Reason != "some_new_reason" || res.Budget != nil {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestUsage_budgetExceededRefusalCarriesTheBudget(t *testing.T) {
	srv, _ := answer(t, 200, `{"allowed":false,"reason":"budget_exceeded","budget":`+budgetJSON+`}`)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	check, err := c.CheckUsage(context.Background(), nozle.UsageParams{CustomerID: "acme", FeatureCode: "agent_execution"})
	if err != nil || check.Allowed || check.Reason != nozle.RefusalReasonBudgetExceeded || check.Budget == nil || *check.Budget != wantBudget {
		t.Fatalf("check: %+v, %v", check, err)
	}
	track, err := c.TrackUsage(context.Background(), nozle.UsageParams{CustomerID: "acme", FeatureCode: "agent_execution"}, "k1")
	if err != nil || track.Budget == nil || track.Budget.Status != "exceeded" {
		t.Fatalf("track: %+v, %v", track, err)
	}
}

func TestListBudgets_readsTheRouteWithSubscriptionID(t *testing.T) {
	srv, got := answer(t, 200, `{"customer_id":"acme/1","subscription_id":"sub_1","budgets":[`+budgetJSON+`,{"feature_code":null,"limit":"100","spent":"0","held":"0","remaining":"100","currency":"USD","period":"lifetime","period_start":"2026-01-01T00:00:00Z","period_end":null,"mode":"soft","status":"ok","as_of":null}]}`)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	list, err := c.ListBudgets(context.Background(), "acme/1", "sub_1")
	if err != nil {
		t.Fatal(err)
	}
	r := (*got)[0]
	if r.Method != "GET" || r.URL.EscapedPath() != "/api/v1/customers/acme%2F1/budgets" || r.URL.RawQuery != "subscription_id=sub_1" {
		t.Fatalf("request %s %s?%s", r.Method, r.URL.EscapedPath(), r.URL.RawQuery)
	}
	if list.SubscriptionID != "sub_1" || len(list.Budgets) != 2 || list.Budgets[0] != wantBudget {
		t.Fatalf("list %+v", list)
	}
	if total := list.Budgets[1]; total.FeatureCode != "" || total.PeriodEnd != "" || total.AsOf != "" || total.Mode != "soft" {
		t.Fatalf("total limit %+v", total)
	}
	if _, err := c.ListBudgets(context.Background(), "acme", ""); err != nil || (*got)[1].URL.RawQuery != "" {
		t.Fatalf("without subscription: %v, %q", err, (*got)[1].URL.RawQuery)
	}
}

func TestListBudgets_validatesBeforeAnyRequest(t *testing.T) {
	srv, got := answer(t, 200, `{}`)
	_, err := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL)).ListBudgets(context.Background(), " ", "")
	var v *nozle.ValidationError
	if !errors.As(err, &v) || len(*got) != 0 {
		t.Fatalf("err = %v, requests = %d", err, len(*got))
	}
}

func TestTypedErrors_byStatusAndExactCode(t *testing.T) {
	ctx := context.Background()
	srv, _ := answer(t, 402, `{"error":{"code":"budget_exceeded"},"code":"budget_exceeded","budget":`+budgetJSON+`}`)
	_, err := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL)).ListBudgets(ctx, "acme", "")
	var budgetErr *nozle.BudgetExceededError
	var apiErr *nozle.APIError
	if !errors.As(err, &budgetErr) || budgetErr.Budget == nil || *budgetErr.Budget != wantBudget {
		t.Fatalf("want *BudgetExceededError with the budget, got %#v", err)
	}
	if !nozle.AsAPIError(err, &apiErr) || apiErr.StatusCode != 402 || apiErr.Code() != nozle.RefusalReasonBudgetExceeded {
		t.Fatalf("want it to unwrap to the *APIError, got %#v", err)
	}

	srv, _ = answer(t, 402, `{"code":"budget_exceeded"}`)
	_, err = nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL)).ListBudgets(ctx, "acme", "")
	if !errors.As(err, &budgetErr) || budgetErr.Budget != nil {
		t.Fatalf("want *BudgetExceededError without a budget, got %#v", err)
	}

	srv, got := answer(t, 429, `{"code":"usage_limit_exceeded"}`)
	_, err = nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithMaxRetries(0)).ListBudgets(ctx, "acme", "")
	var limitErr *nozle.UsageLimitExceededError
	if !errors.As(err, &limitErr) || limitErr.StatusCode != 429 || len(*got) != 1 {
		t.Fatalf("want *UsageLimitExceededError, got %#v", err)
	}

	for _, c := range []struct {
		status int
		code   string
	}{{402, "insufficient_credits"}, {429, "budget_exceeded"}, {402, "usage_limit_exceeded"}} {
		srv, _ := answer(t, c.status, `{"code":"`+c.code+`"}`)
		_, err := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithMaxRetries(0)).ListBudgets(ctx, "acme", "")
		if _, ok := err.(*nozle.APIError); !ok {
			t.Fatalf("%d %s: want a plain *APIError, got %#v", c.status, c.code, err)
		}
	}
}

const budgetHoldJSON = `{"hold_id":"bh_1","kind":"budget","status":"held","customer_id":"acme","subscription_id":"sub_1",
"feature_code":"agent_execution","metric_amount":"1000","amount_held":"0.25","currency":"USD",
"expires_at":"2026-10-31T12:15:00Z","created_at":"2026-10-31T12:00:00Z","resolved_at":null`

func TestBudgetHolds_carryKindAmountHeldAndCurrency(t *testing.T) {
	ctx := context.Background()
	srv, _ := answer(t, 200, `{"hold_id":"bh_1","kind":"budget","status":"denied","customer_id":"acme","subscription_id":"sub_1",
"feature_code":"agent_execution","metric_amount":"1000","amount_held":"0","currency":"USD","expires_at":"2026-10-31T12:15:00Z",
"created_at":"2026-10-31T12:00:00Z","resolved_at":"2026-10-31T12:00:00Z","allowed":false,"reason":"budget_exceeded","budget":`+budgetJSON+`}`)
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	res, err := c.CreateCreditHold(ctx, nozle.CreditHoldParams{CustomerID: "acme", FeatureCode: "agent_execution"}, "run-1")
	if err != nil || res.Kind != nozle.HoldKindBudget || res.Allowed || res.Reason != nozle.RefusalReasonBudgetExceeded ||
		res.Currency != "USD" || res.Budget == nil || *res.Budget != wantBudget {
		t.Fatalf("create %+v, %v", res, err)
	}

	srv, _ = answer(t, 200, budgetHoldJSON+`}`)
	c = nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL))
	hold, err := c.GetCreditHold(ctx, "bh_1")
	if err != nil || hold.Kind != nozle.HoldKindBudget || hold.AmountHeld != "0.25" || hold.Currency != "USD" || hold.CreditsHeld != "" {
		t.Fatalf("get %+v, %v", hold, err)
	}
	fin, err := c.FinalizeCreditHold(ctx, "bh_1", nozle.FinalizeCreditHoldParams{Properties: map[string]any{"tokens": 900}})
	if err != nil || fin.Kind != nozle.HoldKindBudget || fin.Status != "held" || fin.AmountHeld != "0.25" || fin.HoldID != "bh_1" {
		t.Fatalf("finalize %+v, %v", fin, err)
	}
}
