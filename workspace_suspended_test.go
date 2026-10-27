package nozle_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

// 402 workspace_suspended (unpaid Nozle invoices) and card_required (no verified card) answer every call: it is returned as an
// *APIError with Code(), never retried and never turned into an allowed or denied result.
func TestWorkspaceSuspended_402SurfacesWithCodeAndIsNotRetried(t *testing.T) {
	calls := map[string]func(*nozle.Client) error{
		"can": func(c *nozle.Client) error {
			_, err := c.Can(context.Background(), "c", "f")
			return err
		},
		"track": func(c *nozle.Client) error {
			return c.Track(context.Background(), nozle.TrackParams{CustomerID: "c", Event: "e", SubscriptionID: "s"})
		},
		"usage check": func(c *nozle.Client) error {
			_, err := c.CheckUsage(context.Background(), nozle.UsageParams{CustomerID: "c", FeatureCode: "f"})
			return err
		},
		"plans": func(c *nozle.Client) error {
			_, err := c.Plans(context.Background())
			return err
		},
	}
	bodies := map[string]string{
		"workspace_suspended": `{"code":"workspace_suspended","error":"This workspace is suspended because its Nozle invoices are unpaid. Pay them to resume."}`,
		"card_required":       `{"code":"card_required","error":"Add a card to your Nozle workspace to use the API."}`,
	}
	for code, body := range bodies {
		for name, call := range calls {
			code, body, name, call := code, body, name, call
			t.Run(code+"/"+name, func(t *testing.T) {
				hits := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits++
					w.WriteHeader(http.StatusPaymentRequired)
					_, _ = w.Write([]byte(body))
				}))
				defer srv.Close()
				c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL),
					nozle.WithMaxRetries(3), nozle.WithRetryDelay(time.Millisecond))
				var apiErr *nozle.APIError
				if err := call(c); !errors.As(err, &apiErr) {
					t.Fatalf("err = %v, want *APIError", err)
				}
				if apiErr.StatusCode != 402 || apiErr.Code() != code {
					t.Fatalf("status = %d, code = %q", apiErr.StatusCode, apiErr.Code())
				}
				if hits != 1 {
					t.Fatalf("requests = %d, want 1", hits)
				}
			})
		}
	}
}

// Stripe checkout is hosted only: no client secret anywhere in the SDK.
func TestNoEmbeddedCheckoutSecret(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "README.md")
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if lower := strings.ToLower(string(data)); strings.Contains(lower, "client_secret") || strings.Contains(lower, "clientsecret") {
			t.Errorf("%s mentions a client secret", file)
		}
	}
}
