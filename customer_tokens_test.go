package nozle_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

const tokenOK = `{"token":"tok","customer_id":"cus 1","expires_at":"2026-11-22T13:00:00Z"}`

func TestCustomerTokensCreateAndRevokeCallCore(t *testing.T) {
	var bodies []map[string]any
	var paths []string
	var auth string
	c, _ := trialServer(t, 200, tokenOK, func(r *http.Request, body map[string]any) {
		bodies, paths, auth = append(bodies, body), append(paths, r.Method+" "+r.URL.Path), r.Header.Get("Authorization")
	})
	ctx := context.Background()
	out, err := c.CustomerTokens.Create(ctx, "cus 1", nozle.CustomerTokenOptions{TTLSeconds: 600, Features: []string{"seats"}})
	if err != nil || *out != (nozle.CustomerToken{Token: "tok", CustomerID: "cus 1", ExpiresAt: "2026-11-22T13:00:00Z"}) {
		t.Fatalf("out = %+v, err %v", out, err)
	}
	if _, err := c.CustomerTokens.Create(ctx, "cus 1", nozle.CustomerTokenOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CustomerTokens.Revoke(ctx); err != nil {
		t.Fatal(err)
	}
	wantBodies := []map[string]any{
		{"customer_id": "cus 1", "ttl_seconds": float64(600), "features": []any{"seats"}},
		{"customer_id": "cus 1"},
		{},
	}
	wantPaths := []string{"POST /core/api/v1/customer_tokens", "POST /core/api/v1/customer_tokens", "POST /core/api/v1/customer_tokens/revoke"}
	if !reflect.DeepEqual(bodies, wantBodies) || !reflect.DeepEqual(paths, wantPaths) || auth != "Bearer sk_test" {
		t.Fatalf("%v %v %q", bodies, paths, auth)
	}
}

func TestCustomerTokensRefuseBadInputBeforeSending(t *testing.T) {
	c, calls := trialServer(t, 200, tokenOK, nil)
	ctx := context.Background()
	var verr *nozle.ValidationError
	if _, err := c.CustomerTokens.Create(ctx, " ", nozle.CustomerTokenOptions{}); !errors.As(err, &verr) {
		t.Errorf("blank customer: %v", err)
	}
	pk := nozle.New("pk_x")
	if _, err := pk.CustomerTokens.Create(ctx, "cus", nozle.CustomerTokenOptions{}); err == nil {
		t.Error("publishable key accepted by Create")
	}
	if _, err := pk.CustomerTokens.Revoke(ctx); err == nil {
		t.Error("publishable key accepted by Revoke")
	}
	if *calls != 0 {
		t.Fatalf("%d requests sent", *calls)
	}
}

func TestCustomerTokensCreateReturnsCoreRefusals(t *testing.T) {
	c, _ := trialServer(t, 404, `{"status":404,"error":"Not Found","code":"customer_not_found"}`, nil)
	_, err := c.CustomerTokens.Create(context.Background(), "missing", nozle.CustomerTokenOptions{})
	var api *nozle.APIError
	if !errors.As(err, &api) || api.StatusCode != 404 || !api.HasCode("customer_not_found") {
		t.Fatalf("err = %v", err)
	}
}
