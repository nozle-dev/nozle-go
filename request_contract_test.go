package nozle_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

// Every request body the SDK sends must fit the API's request schema.
// testdata/openapi_requests.json is written by scripts/sync_openapi_requests.py from
// nozle-openapi.yaml. Each request is matched to its operation by method and path, and its body
// is checked: no key the schema does not have, no null where it is not allowed, enum values only.

type schema struct {
	Type                 any                `json:"type"`
	Enum                 []any              `json:"enum"`
	Properties           map[string]*schema `json:"properties"`
	Items                *schema            `json:"items"`
	AllOf                []*schema          `json:"allOf"`
	OneOf                []*schema          `json:"oneOf"`
	AnyOf                []*schema          `json:"anyOf"`
	AdditionalProperties any                `json:"additionalProperties"`
}

func (s *schema) types() []string {
	switch t := s.Type.(type) {
	case string:
		return []string{t}
	case []any:
		out := []string{}
		for _, item := range t {
			out = append(out, fmt.Sprint(item))
		}
		return out
	}
	return nil
}

func problems(value any, s *schema, where string) []string {
	if s == nil {
		return nil
	}
	for _, branches := range [][]*schema{s.OneOf, s.AnyOf} {
		if len(branches) == 0 {
			continue
		}
		var best []string
		for i, branch := range branches {
			found := problems(value, branch, where)
			if len(found) == 0 {
				return nil
			}
			if i == 0 || len(found) < len(best) {
				best = found
			}
		}
		return best
	}
	merged := *s
	props := map[string]*schema{}
	for k, v := range s.Properties {
		props[k] = v
	}
	for _, part := range s.AllOf {
		for k, v := range part.Properties {
			props[k] = v
		}
		if merged.Type == nil {
			merged.Type = part.Type
		}
	}
	kinds := merged.types()
	if value == nil {
		for _, kind := range kinds {
			if kind == "null" {
				return nil
			}
		}
		if len(kinds) == 0 {
			return nil
		}
		return []string{where + ": null is not allowed"}
	}
	if len(merged.Enum) > 0 {
		ok := false
		for _, allowed := range merged.Enum {
			if fmt.Sprint(allowed) == fmt.Sprint(value) {
				ok = true
			}
		}
		if !ok {
			return []string{fmt.Sprintf("%s: %v is not one of %v", where, value, merged.Enum)}
		}
	}
	var found []string
	if object, ok := value.(map[string]any); ok && len(props) > 0 {
		for key, item := range object {
			if child, known := props[key]; known {
				found = append(found, problems(item, child, where+"."+key)...)
			} else if merged.AdditionalProperties == nil || merged.AdditionalProperties == false {
				found = append(found, fmt.Sprintf("%s: unknown key %q", where, key))
			}
		}
	}
	if list, ok := value.([]any); ok && merged.Items != nil {
		for i, item := range list {
			found = append(found, problems(item, merged.Items, fmt.Sprintf("%s[%d]", where, i))...)
		}
	}
	return found
}

func TestRequestBodiesFitTheAPISchemas(t *testing.T) {
	raw, err := os.ReadFile("testdata/openapi_requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var operations map[string]*schema
	if err := json.Unmarshal(raw, &operations); err != nil {
		t.Fatal(err)
	}
	type route struct {
		method, key string
		pattern     *regexp.Regexp
	}
	var routes []route
	for key := range operations {
		method, path, _ := strings.Cut(key, " ")
		routes = append(routes, route{method, key, regexp.MustCompile("^" + regexp.MustCompile(`\{[^/]+\}`).ReplaceAllString(path, "[^/]+") + "$")})
	}
	find := func(method, path string) string {
		if _, ok := operations[method+" "+path]; ok {
			return method + " " + path
		}
		best := ""
		for _, r := range routes {
			if r.method == method && r.pattern.MatchString(path) && (best == "" || strings.Count(r.key, "{") < strings.Count(best, "{")) {
				best = r.key
			}
		}
		return best
	}

	type sent struct {
		method, path string
		body         []byte
	}
	var requests []sent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodGet {
			requests = append(requests, sent{r.Method, r.URL.Path, body})
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL+"/engine"), nozle.WithEventsURL(srv.URL+"/core"))
	ctx := context.Background()
	no := false
	calls := map[string]func(){
		"Track": func() {
			_ = c.Track(ctx, nozle.TrackParams{CustomerID: "cust", Event: "api_calls", SubscriptionID: "sub_1"})
		},
		"CheckUsage": func() { _, _ = c.CheckUsage(ctx, nozle.UsageParams{CustomerID: "cust", FeatureCode: "emails"}) },
		"TrackUsage": func() { _, _ = c.TrackUsage(ctx, nozle.UsageParams{CustomerID: "cust", FeatureCode: "emails"}, "k1") },
		"TrackCostEvent": func() {
			_, _ = c.TrackCostEvent(ctx, nozle.CostEventParams{CostMeterCode: "llm_tokens", ExternalCustomerID: "cust"})
		},
		"UpsertCustomer": func() { _, _ = c.UpsertCustomer(ctx, nozle.Customer{ExternalID: "cust"}) },
		"Subscribe":      func() { _, _ = c.Subscribe(ctx, nozle.SubscribeParams{CustomerID: "cust", PlanCode: "pro"}) },
		"SubscribeToAPlanVersion": func() {
			_, _ = c.Subscribe(ctx, nozle.SubscribeParams{CustomerID: "cust", PlanCode: "pro", PlanVersion: 1})
		},
		"PreviewPlanVersionMigration": func() {
			_, _ = c.PreviewPlanVersionMigration(ctx, "pro", nozle.PlanVersionMigrationParams{FromVersion: 1})
		},
		"CreatePlanVersionMigration": func() {
			_, _ = c.CreatePlanVersionMigration(ctx, "pro", nozle.PlanVersionMigrationParams{FromVersion: 1, ToVersion: 3})
		},
		"CancelPlanVersionMigration": func() { _, _ = c.CancelPlanVersionMigration(ctx, "pro", "mig_1") },
		"Checkout":                   func() { _, _ = c.Checkout(ctx, nozle.CheckoutParams{CustomerID: "cust", PlanCode: "pro"}) },
		"CheckoutEntitySubscription": func() {
			_, _ = c.CheckoutEntitySubscription(ctx, "cust", "user-42", nozle.EntitySubscriptionChange{PlanCode: "pro", RegisterMandate: &no})
		},
		"ChangeEntitySubscriptionPlan": func() {
			_, _ = c.ChangeEntitySubscriptionPlan(ctx, "cust", "user-42", nozle.EntitySubscriptionChange{PlanCode: "team"})
		},
		"CheckoutEntitySubscriptions": func() {
			_, _ = c.CheckoutEntitySubscriptions(ctx, "cust", []nozle.EntitySubscriptionCheckoutItem{{ExternalEntityID: "user-42", PlanCode: "pro"}}, "", "", "b1")
		},
		"CancelEntitySubscription": func() {
			_, _ = c.CancelEntitySubscription(ctx, "cust", "user-42", nozle.EntitySubscriptionCancel{IdempotencyKey: "c1"})
		},
		"UpsertEntity":    func() { _, _ = c.UpsertEntity(ctx, "cust", "user-42", nozle.EntityInput{Status: "active"}, "e1") },
		"AllocateCredits": func() { _, _ = c.AllocateCredits(ctx, "cust", "user-42", "ai_credits", nozle.Decimal("10"), "a1") },
	}
	for name, call := range calls {
		requests = nil
		call()
		if len(requests) == 0 {
			t.Errorf("%s sent no request with a body", name)
			continue
		}
		for _, r := range requests {
			key := find(r.method, r.path)
			if key == "" {
				t.Errorf("%s: %s %s is not an operation in the spec", name, r.method, r.path)
				continue
			}
			if len(r.body) == 0 {
				continue
			}
			var body any
			if err := json.Unmarshal(r.body, &body); err != nil {
				t.Errorf("%s: invalid JSON body: %v", name, err)
				continue
			}
			for _, problem := range problems(body, operations[key], key) {
				t.Errorf("%s: %s", name, problem)
			}
		}
	}
}

func TestTheRequestCheckRejectsNullUnknownKeysAndValuesOutsideAnEnum(t *testing.T) {
	raw, err := os.ReadFile("testdata/openapi_requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var operations map[string]*schema
	if err := json.Unmarshal(raw, &operations); err != nil {
		t.Fatal(err)
	}
	key := "POST /core/api/v1/customers/{customer_id}/entity-subscriptions/checkout"
	var body any
	_ = json.Unmarshal([]byte(`{"entity_subscription_checkout":{"items":[{"external_entity_id":"u","plan_code":"p"}],"billing_time":"","return_url":null,"extra":1}}`), &body)
	found := strings.Join(problems(body, operations[key], key), "\n")
	for _, want := range []string{"billing_time", "null is not allowed", `unknown key "extra"`} {
		if !strings.Contains(found, want) {
			t.Errorf("want a problem mentioning %s, got:\n%s", want, found)
		}
	}
}
