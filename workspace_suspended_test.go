package nozle_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

// A suspended workspace answers every call with 402 workspace_suspended: it is returned as an
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
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				w.WriteHeader(http.StatusPaymentRequired)
				_, _ = w.Write([]byte(`{"code":"workspace_suspended","error":"This workspace is suspended because its Nozle invoices are unpaid. Pay them to resume."}`))
			}))
			defer srv.Close()
			c := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL),
				nozle.WithMaxRetries(3), nozle.WithRetryDelay(time.Millisecond))
			var apiErr *nozle.APIError
			if err := call(c); !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.StatusCode != 402 || apiErr.Code() != "workspace_suspended" {
				t.Fatalf("status = %d, code = %q", apiErr.StatusCode, apiErr.Code())
			}
			if hits != 1 {
				t.Fatalf("requests = %d, want 1", hits)
			}
		})
	}
}
