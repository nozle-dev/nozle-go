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

// versionServer serves Core at /core and Engine at /engine, recording the last request.
type versionCall struct {
	method, path, query string
	body                map[string]any
}

func versionServer(t *testing.T, status int, response string) (*nozle.Client, *versionCall) {
	t.Helper()
	call := &versionCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call.method, call.path, call.query, call.body = r.Method, r.URL.Path, r.URL.RawQuery, nil
		_ = json.NewDecoder(r.Body).Decode(&call.body)
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test-key" {
			t.Errorf("Authorization %q", got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL+"/engine"), nozle.WithEventsURL(srv.URL+"/core")), call
}

func TestPlansCarryTheirVersion(t *testing.T) {
	c, _ := versionServer(t, 200, `{"plans":[{"code":"pro","name":"Pro","amount_cents":5900,"amount_currency":"USD","interval":"monthly","version":2,"is_current":true}]}`)
	plans, err := c.Plans(context.Background())
	if err != nil || len(plans) != 1 || plans[0].Version != 2 || !plans[0].IsCurrent {
		t.Fatalf("plans %+v err %v", plans, err)
	}
}

func TestSubscribeSendsAPlanVersionOnlyWhenSet(t *testing.T) {
	for _, test := range []struct {
		version int
		want    any
	}{{0, nil}, {1, float64(1)}} {
		c, call := versionServer(t, 200, `{"subscription":{"external_id":"sub-cust-pro","status":"active","plan_code":"pro","plan_version":1}}`)
		result, err := c.Subscribe(context.Background(), nozle.SubscribeParams{CustomerID: "cust", PlanCode: "pro", PlanVersion: test.version})
		if err != nil {
			t.Fatal(err)
		}
		got, sent := call.body["plan_version"]
		if call.path != "/engine/api/v1/subscribe" || (test.want == nil && sent) || (test.want != nil && got != test.want) {
			t.Fatalf("version %d: %s %v", test.version, call.path, call.body)
		}
		if result.Subscription.PlanVersion != 1 {
			t.Fatalf("plan_version not read: %+v", result)
		}
	}
	c, _ := versionServer(t, 200, `{}`)
	var invalid *nozle.ValidationError
	if _, err := c.Subscribe(context.Background(), nozle.SubscribeParams{CustomerID: "cust", PlanCode: "pro", PlanVersion: -1}); !errors.As(err, &invalid) {
		t.Fatalf("negative version: %v", err)
	}
}

func TestPlanVersionsListsEveryVersionFromCore(t *testing.T) {
	c, call := versionServer(t, 200, `{"versions":[{"nozle_id":"p1","version":1,"is_current":false,"created_at":"2026-10-01T09:00:00Z","amount_cents":4900,"amount_currency":"USD","active_subscriptions_count":42},{"nozle_id":"p2","version":2,"is_current":true,"created_at":"2026-10-15T09:00:00Z","amount_cents":5900,"amount_currency":"USD","active_subscriptions_count":7}]}`)
	versions, err := c.PlanVersions(context.Background(), "pro plan")
	if err != nil {
		t.Fatal(err)
	}
	if call.method != "GET" || call.path != "/core/api/v1/plans/pro plan/versions" {
		t.Fatalf("request %s %s", call.method, call.path)
	}
	if len(versions) != 2 || versions[0].Version != 1 || versions[0].ActiveSubscriptionsCount != 42 || !versions[1].IsCurrent || versions[1].AmountCents != 5900 {
		t.Fatalf("versions %+v", versions)
	}
}

func TestPlanVersionReadsOneVersion(t *testing.T) {
	c, call := versionServer(t, 200, `{"plan":{"code":"pro","amount_cents":4900,"version":1,"is_current":false}}`)
	plan, err := c.PlanVersion(context.Background(), "pro", 1)
	if err != nil || call.path != "/core/api/v1/plans/pro" || call.query != "version=1" || plan.Version != 1 || plan.IsCurrent || plan.AmountCents != 4900 {
		t.Fatalf("plan %+v err %v request %s?%s", plan, err, call.path, call.query)
	}
	if _, err := c.PlanVersion(context.Background(), "pro", 0); err == nil {
		t.Fatal("version 0 accepted")
	}
}

func TestPlanVersionMigrations(t *testing.T) {
	ctx := context.Background()
	migration := `{"plan_version_migration":{"nozle_id":"mig_1","plan_code":"pro","from_version":1,"to_version":2,"status":"running","matched_count":3,"succeeded_count":0,"skipped_count":0,"failed_count":0,"started_at":"2026-10-22T10:00:00Z","finished_at":null,"canceled_at":null,"created_at":"2026-10-22T10:00:00Z"}}`

	c, call := versionServer(t, 200, `{"plan_version_migration_preview":{"plan_code":"pro","from_version":1,"to_version":2,"items":[{"external_subscription_id":"sub_1","external_customer_id":"cus_1","outcome":"move","skip_reason":null,"effective_on":"2026-11-01","current_amount_cents":4900,"new_amount_cents":5900},{"external_subscription_id":"sub_2","external_customer_id":"cus_2","outcome":"skip","skip_reason":"custom_pricing","effective_on":null,"current_amount_cents":3900,"new_amount_cents":5900}]}}`)
	preview, err := c.PreviewPlanVersionMigration(ctx, "pro", nozle.PlanVersionMigrationParams{FromVersion: 1})
	if err != nil || call.method != "POST" || call.path != "/core/api/v1/plans/pro/version-migrations/preview" || call.body["from_version"] != float64(1) {
		t.Fatalf("preview request %s %s %v err %v", call.method, call.path, call.body, err)
	}
	if _, sent := call.body["to_version"]; sent {
		t.Fatalf("to_version sent without being asked: %v", call.body)
	}
	if len(preview.Items) != 2 || preview.Items[0].Outcome != "move" || preview.Items[0].NewAmountCents != 5900 || preview.Items[1].SkipReason != "custom_pricing" {
		t.Fatalf("preview %+v", preview)
	}

	c, call = versionServer(t, 200, migration)
	created, err := c.CreatePlanVersionMigration(ctx, "pro", nozle.PlanVersionMigrationParams{FromVersion: 1, ToVersion: 2})
	if err != nil || call.path != "/core/api/v1/plans/pro/version-migrations" || call.body["to_version"] != float64(2) || created.NozleID != "mig_1" || created.Status != "running" || created.MatchedCount != 3 {
		t.Fatalf("create %+v request %s %v err %v", created, call.path, call.body, err)
	}

	c, call = versionServer(t, 200, `{"plan_version_migrations":[{"nozle_id":"mig_1","plan_code":"pro","from_version":1,"to_version":2,"status":"succeeded","matched_count":3,"succeeded_count":2,"skipped_count":1,"failed_count":0,"created_at":"2026-10-22T10:00:00Z"}]}`)
	list, err := c.ListPlanVersionMigrations(ctx, "pro")
	if err != nil || call.method != "GET" || len(list) != 1 || list[0].SucceededCount != 2 {
		t.Fatalf("list %+v err %v", list, err)
	}

	c, call = versionServer(t, 200, `{"plan_version_migration":{"nozle_id":"mig_1","plan_code":"pro","from_version":1,"to_version":2,"status":"succeeded","matched_count":1,"succeeded_count":1,"skipped_count":0,"failed_count":0,"created_at":"2026-10-22T10:00:00Z","items":[{"external_subscription_id":"sub_1","status":"succeeded","skip_reason":null,"error":null,"effective_at":"2026-11-01T00:00:00Z","move_status":"pending"}]}}`)
	got, err := c.GetPlanVersionMigration(ctx, "pro", "mig_1")
	if err != nil || call.path != "/core/api/v1/plans/pro/version-migrations/mig_1" || len(got.Items) != 1 || got.Items[0].MoveStatus != "pending" || got.Items[0].EffectiveAt == "" {
		t.Fatalf("get %+v err %v", got, err)
	}

	c, call = versionServer(t, 200, migration)
	if _, err := c.CancelPlanVersionMigration(ctx, "pro", "mig_1"); err != nil || call.method != "POST" || call.path != "/core/api/v1/plans/pro/version-migrations/mig_1/cancel" {
		t.Fatalf("cancel %s %s err %v", call.method, call.path, err)
	}

	c, _ = versionServer(t, 409, `{"status":409,"error":"Conflict","code":"plan_version_migration_running"}`)
	_, err = c.CreatePlanVersionMigration(ctx, "pro", nozle.PlanVersionMigrationParams{FromVersion: 1})
	var api *nozle.APIError
	if !errors.As(err, &api) || api.StatusCode != 409 || !api.HasCode("plan_version_migration_running") {
		t.Fatalf("running migration: %v", err)
	}

	for name, call := range map[string]func() error{
		"no from version": func() error {
			_, err := c.PreviewPlanVersionMigration(ctx, "pro", nozle.PlanVersionMigrationParams{})
			return err
		},
		"no code": func() error { _, err := c.ListPlanVersionMigrations(ctx, ""); return err },
		"no id":   func() error { _, err := c.CancelPlanVersionMigration(ctx, "pro", ""); return err },
		"bad to": func() error {
			_, err := c.CreatePlanVersionMigration(ctx, "pro", nozle.PlanVersionMigrationParams{FromVersion: 1, ToVersion: -2})
			return err
		},
	} {
		var invalid *nozle.ValidationError
		if err := call(); !errors.As(err, &invalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPlanVersionCallsNeedASecretKey(t *testing.T) {
	c := nozle.New("pk_test-key")
	var auth *nozle.AuthenticationError
	if _, err := c.PlanVersions(context.Background(), "pro"); !errors.As(err, &auth) {
		t.Fatalf("publishable key: %v", err)
	}
}
