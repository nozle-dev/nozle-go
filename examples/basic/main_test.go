package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunRequiresSecretBeforeNetwork(t *testing.T) {
	var output bytes.Buffer
	err := run(context.Background(), func(string) string { return "" }, &output)
	if err == nil || !strings.Contains(err.Error(), "NOZLE_SECRET_KEY") {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestRunReadsBothServicesWithoutTrackingUsage(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ping":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/v1/plans":
			_, _ = w.Write([]byte(`{"plans":[{"code":"free"}]}`))
		case "/api/v1/usage/check":
			_, _ = w.Write([]byte(`{"allowed":false,"credits_required":"2","available":"0"}`))
		default:
			t.Errorf("unexpected Engine request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer engine.Close()
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/credit-systems" {
			t.Errorf("unexpected Core request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"credit_systems":[{"code":"ai_credits"}],"meta":{}}`))
	}))
	defer core.Close()
	env := map[string]string{
		"NOZLE_SECRET_KEY":         "sk_example",
		"NOZLE_ENGINE_URL":         engine.URL,
		"NOZLE_CORE_URL":           core.URL,
		"NOZLE_CUSTOMER_ID":        "customer_123",
		"NOZLE_FEATURE_CODE":       "agent_execution",
		"NOZLE_CREDIT_SYSTEM_CODE": "ai_credits",
	}
	var output bytes.Buffer
	if err := run(context.Background(), func(key string) string { return env[key] }, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "allowed=false") || !strings.Contains(output.String(), "plans=1") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}
