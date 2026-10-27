package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

type recorded struct {
	method, path, query, key string
	body                     map[string]any
}

// recorder answers every request with status and body and records what it received.
func recorder(t *testing.T, status int, body string) (*nozle.Client, *[]recorded) {
	t.Helper()
	var seen []recorded
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &b); err != nil {
				t.Errorf("invalid JSON body: %s", raw)
			}
		}
		seen = append(seen, recorded{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Idempotency-Key"), b})
		w.Header().Set("X-Request-Id", "req-1")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return nozle.New("sk_test", nozle.WithBaseURL(s.URL+"/engine"), nozle.WithEventsURL(s.URL+"/core")), &seen
}

func apiError(t *testing.T, err error, status int, code string) *nozle.APIError {
	t.Helper()
	var api *nozle.APIError
	if !errors.As(err, &api) || api.StatusCode != status || !api.HasCode(code) || api.RequestID != "req-1" {
		t.Fatalf("want %d %s APIError, got %#v", status, code, err)
	}
	return api
}

func validation(t *testing.T, err error, field string) {
	t.Helper()
	var v *nozle.ValidationError
	if !errors.As(err, &v) || !strings.Contains(err.Error(), field) {
		t.Fatalf("want a ValidationError about %s, got %v", field, err)
	}
}

var creditImport = nozle.CreditBalanceImportParams{
	ExternalSubscriptionID: "merchant_subscription_123", CreditSystemCode: "ai_credits", GrantedAmount: "500", RemainingAmount: "320",
}

var billingImport = nozle.BillingStateImportParams{Subscription: nozle.BillingStateImportSubscription{
	PlanCode: "pro_monthly", Status: "active", BillingAuthority: "external", ExternalReference: "merchant_subscription_123",
	CurrentPeriodStart: "2026-10-15T00:00:00Z", CurrentPeriodEnd: "2026-11-15T00:00:00Z",
}}

func TestImportCreditBalance(t *testing.T) {
	c, seen := recorder(t, 201, `{"credit_balance_import":{"external_customer_id":"w 1","granted_amount":"500","remaining_amount":"320","dry_run":true,"replayed":false}}`)
	p := creditImport
	p.ExternalEntityID, p.DryRun = "user_42", true
	out, err := c.ImportCreditBalance(context.Background(), "w 1", p, "imp-1")
	if err != nil || out.RemainingAmount != "320" || !out.DryRun || out.ExternalCustomerID != "w 1" {
		t.Fatalf("result: %+v %v", out, err)
	}
	r := (*seen)[0]
	want := map[string]any{
		"external_subscription_id": "merchant_subscription_123", "credit_system_code": "ai_credits",
		"granted_amount": "500", "remaining_amount": "320", "external_entity_id": "user_42", "dry_run": true,
	}
	if r.method != "POST" || r.path != "/engine/api/v1/customers/w%201/credit-balance-imports" || r.key != "imp-1" || !reflect.DeepEqual(r.body, want) {
		t.Fatalf("request: %+v", r)
	}
}

func TestImportCreditBalanceErrors(t *testing.T) {
	c, seen := recorder(t, 409, `{"code":"idempotency_key_reused","error":"different request"}`)
	_, err := c.ImportCreditBalance(context.Background(), "c", creditImport, "imp-2")
	apiError(t, err, 409, "idempotency_key_reused")
	_, err = c.ImportCreditBalance(context.Background(), "c", creditImport, "")
	validation(t, err, "idempotency key")
	_, err = c.ImportCreditBalance(context.Background(), "c", creditImport, strings.Repeat("é", 128))
	validation(t, err, "255 bytes")
	_, err = c.ImportCreditBalance(context.Background(), " ", creditImport, "k")
	validation(t, err, "customer_id")
	_, err = nozle.New("pk_test").ImportCreditBalance(context.Background(), "c", creditImport, "k")
	if err == nil {
		t.Fatal("a publishable key must be refused")
	}
	if len(*seen) != 1 {
		t.Fatalf("invalid calls must not be sent: %d requests", len(*seen))
	}
}

func TestImportBillingState(t *testing.T) {
	c, seen := recorder(t, 201, `{"billing_state_import":{"id":"imp","status":"completed","dry_run":false,"external_customer_id":"c/1","subscription":{"external_subscription_id":"merchant_subscription_123","credit_import_required":true},"replayed":false}}`)
	out, err := c.ImportBillingState(context.Background(), "c/1", billingImport, "bs-1")
	if err != nil || out.Status != "completed" || !out.Subscription.CreditImportRequired || out.Subscription.ExternalSubscriptionID != "merchant_subscription_123" {
		t.Fatalf("result: %+v %v", out, err)
	}
	r := (*seen)[0]
	want := map[string]any{"billing_state_import": map[string]any{"subscription": map[string]any{
		"plan_code": "pro_monthly", "status": "active", "billing_authority": "external", "external_reference": "merchant_subscription_123",
		"current_period_start": "2026-10-15T00:00:00Z", "current_period_end": "2026-11-15T00:00:00Z",
	}}}
	if r.method != "POST" || r.path != "/core/api/v1/customers/c%2F1/billing-state-imports" || r.key != "bs-1" || !reflect.DeepEqual(r.body, want) {
		t.Fatalf("request: %+v", r)
	}
}

func TestImportBillingStateErrors(t *testing.T) {
	c, seen := recorder(t, 422, `{"code":"validation_errors","error_details":{"current_period":["must_contain_current_time"]}}`)
	_, err := c.ImportBillingState(context.Background(), "c", billingImport, "bs-2")
	apiError(t, err, 422, "must_contain_current_time")
	_, err = c.ImportBillingState(context.Background(), "c", billingImport, " ")
	validation(t, err, "idempotency key")
	if len(*seen) != 1 {
		t.Fatalf("invalid calls must not be sent: %d requests", len(*seen))
	}
}

func TestMarginForecastAndSimulate(t *testing.T) {
	c, seen := recorder(t, 200, `{"points":[{"date":"2026-10-28","projected_revenue":100,"projected_cost":40,"projected_margin":60}],"confidence":"low","metric_code":"m","new_revenue_cents":29500,"event_count":295}`)
	q := url.Values{"from": {"2026-10-01"}}
	forecast, err := c.MarginForecast(context.Background(), q)
	if err != nil || forecast.Confidence != "low" || len(forecast.Points) != 1 || forecast.Points[0].ProjectedMargin != 60 {
		t.Fatalf("forecast: %+v %v", forecast, err)
	}
	sim, err := c.MarginSimulate(context.Background(), nozle.MarginSimulateParams{MetricCode: "m", NewPriceDollars: 1.5}, q)
	if err != nil || sim.NewRevenueCents != 29500 || sim.EventCount != 295 {
		t.Fatalf("simulate: %+v %v", sim, err)
	}
	f, s := (*seen)[0], (*seen)[1]
	if f.method != "GET" || f.path != "/engine/api/v1/margin/forecast" || f.query != "from=2026-10-01" {
		t.Fatalf("forecast request: %+v", f)
	}
	if s.method != "POST" || s.path != "/engine/api/v1/margin/simulate" || s.query != "from=2026-10-01" ||
		!reflect.DeepEqual(s.body, map[string]any{"metric_code": "m", "new_price_dollars": 1.5}) {
		t.Fatalf("simulate request: %+v", s)
	}
}

func TestMarginForecastAndSimulateErrors(t *testing.T) {
	c, seen := recorder(t, 400, `{"code":"invalid_date_range","error":"to must be after from"}`)
	_, err := c.MarginForecast(context.Background(), nil)
	apiError(t, err, 400, "invalid_date_range")
	_, err = c.MarginSimulate(context.Background(), nozle.MarginSimulateParams{MetricCode: "m"}, nil)
	apiError(t, err, 400, "invalid_date_range")
	_, err = c.MarginSimulate(context.Background(), nozle.MarginSimulateParams{MetricCode: "m", NewPriceDollars: -1}, nil)
	validation(t, err, "new_price_dollars")
	_, err = c.MarginSimulate(context.Background(), nozle.MarginSimulateParams{NewPriceDollars: 1}, nil)
	validation(t, err, "metric_code")
	if len(*seen) != 2 {
		t.Fatalf("invalid calls must not be sent: %d requests", len(*seen))
	}
}

func TestCostIntelligence(t *testing.T) {
	c, seen := recorder(t, 200, `{"from":"2026-10-01T00:00:00Z","summary":{"total_cost":"1.5","ledger_entry_count":2,"currency":"USD"},"features":[{"key":"chat","cost":"1.5"}],"id":"run-1","status":"completed","allocations":[{"external_customer_id":"c1","amount":"0.75"}]}`)
	ctx := context.Background()
	report, err := c.CostIntelligenceReport(ctx, nozle.CostIntelligenceReportParams{From: "2026-10-01T00:00:00Z", To: "2026-10-27T00:00:00Z", Limit: 50})
	if err != nil || report.Summary.TotalCost != "1.5" || report.Summary.LedgerEntryCount != 2 || report.Features[0].Key != "chat" {
		t.Fatalf("report: %+v %v", report, err)
	}
	if _, err = c.CostIntelligenceReport(ctx, nozle.CostIntelligenceReportParams{}); err != nil {
		t.Fatal(err)
	}
	preview, err := c.PreviewSharedCostAllocation(ctx, "pool/1", "2026-10-01T00:00:00Z")
	if err != nil || preview.Allocations[0].Amount != "0.75" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	run, err := c.AllocateSharedCost(ctx, "pool/1", "2026-10-01T00:00:00Z")
	if err != nil || run.ID != "run-1" || run.Status != "completed" {
		t.Fatalf("allocate: %+v %v", run, err)
	}
	r := *seen
	if r[0].method != "GET" || r[0].path != "/engine/api/v1/cost-intelligence/report" || r[0].query != "from=2026-10-01T00%3A00%3A00Z&limit=50&to=2026-10-27T00%3A00%3A00Z" || r[1].query != "" {
		t.Fatalf("report requests: %+v %+v", r[0], r[1])
	}
	body := map[string]any{"allocation_period_start": "2026-10-01T00:00:00Z"}
	if r[2].method != "POST" || r[2].path != "/engine/api/v1/cost-intelligence/shared-costs/pool%2F1/preview" || !reflect.DeepEqual(r[2].body, body) {
		t.Fatalf("preview request: %+v", r[2])
	}
	if r[3].path != "/engine/api/v1/cost-intelligence/shared-costs/pool%2F1/allocate" || !reflect.DeepEqual(r[3].body, body) {
		t.Fatalf("allocate request: %+v", r[3])
	}
}

func TestCostIntelligenceErrors(t *testing.T) {
	c, seen := recorder(t, 409, `{"code":"allocation_period_open","error":"allocation period is still open"}`)
	_, err := c.AllocateSharedCost(context.Background(), "p", "2026-10-01T00:00:00Z")
	apiError(t, err, 409, "allocation_period_open")
	_, err = c.PreviewSharedCostAllocation(context.Background(), "p", "")
	validation(t, err, "allocation_period_start")
	_, err = c.PreviewSharedCostAllocation(context.Background(), "", "2026-10-01T00:00:00Z")
	validation(t, err, "pool_id")
	_, err = nozle.New("pk_test").CostIntelligenceReport(context.Background(), nozle.CostIntelligenceReportParams{})
	if err == nil {
		t.Fatal("a publishable key must be refused")
	}
	if len(*seen) != 1 {
		t.Fatalf("invalid calls must not be sent: %d requests", len(*seen))
	}
}
