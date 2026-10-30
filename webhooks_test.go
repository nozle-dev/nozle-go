package nozle

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Body is a real event; Standard is the published Standard Webhooks sample.
type webhookVectors struct {
	Body     string `json:"body"`
	Standard struct {
		Secret    string `json:"secret"`
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
		Body      string `json:"body"`
		Signature string `json:"signature"`
	} `json:"standard"`
}

func loadVectors(t *testing.T) webhookVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/webhook_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v webhookVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func verificationCode(err error) string {
	var v *WebhookVerificationError
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}

func TestVerifyWebhookStandard(t *testing.T) {
	sv := loadVectors(t).Standard
	secondSecret := "whsec_" + "c2Vjb25kLXNlY3JldC0xMjM0NTY3ODkwMTI="
	at := func(offset time.Duration) WebhookOption {
		return WithWebhookClock(func() time.Time { return time.Unix(1614265330, 0).Add(offset) })
	}
	header := func(sig string) http.Header {
		h := http.Header{}
		h.Set("webhook-id", sv.ID)
		h.Set("webhook-timestamp", sv.Timestamp)
		if sig != "" {
			h.Set("webhook-signature", sig)
		}
		return h
	}
	body := []byte(sv.Body)
	c := New("sk_test") // no host: any network call would fail the test
	verify := func(body []byte, h http.Header, opts ...WebhookOption) error {
		_, err := c.VerifyWebhook(context.Background(), body, h, opts...)
		return err
	}
	// The vector body is not an event, so a good signature fails later, at invalid_payload.
	ok := func(err error) bool { return verificationCode(err) == "invalid_payload" }
	if err := verify(body, header(sv.Signature), WithWebhookSecrets(sv.Secret), at(0)); !ok(err) {
		t.Fatalf("vector: %v", err)
	}
	cases := []struct {
		name string
		body []byte
		h    http.Header
		opts []WebhookOption
		want string
	}{
		{"wrong secret", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(secondSecret), at(0)}, "invalid_signature"},
		{"tampered body", []byte(`{"test": 2432232315}`), header(sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), at(0)}, "invalid_signature"},
		{"301 s old", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), at(301 * time.Second)}, "invalid_timestamp"},
		{"299 s old", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), at(299 * time.Second)}, "invalid_payload"},
		{"301 s in the future", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), at(-301 * time.Second)}, "invalid_timestamp"},
		{"second of two signatures", body, header("v1,AAAA " + sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), at(0)}, "invalid_payload"},
		{"rotated secrets", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(secondSecret, sv.Secret), at(0)}, "invalid_payload"},
		{"v2 entry ignored", body, header("v2," + strings.TrimPrefix(sv.Signature, "v1,")), []WebhookOption{WithWebhookSecrets(sv.Secret), at(0)}, "invalid_signature"},
		{"missing signature header", body, header(""), []WebhookOption{WithWebhookSecrets(sv.Secret), at(0)}, "missing_signature"},
		{"no headers", body, http.Header{}, []WebhookOption{WithWebhookSecrets(sv.Secret), at(0)}, "missing_signature"},
		{"no secret configured", body, header(sv.Signature), []WebhookOption{at(0)}, "missing_secret"},
		{"custom tolerance", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), WithWebhookTolerance(10 * time.Second), at(11 * time.Second)}, "invalid_timestamp"},
	}
	for _, tc := range cases {
		if got := verificationCode(verify(tc.body, tc.h, tc.opts...)); got != tc.want {
			t.Errorf("%s: code %q, want %q", tc.name, got, tc.want)
		}
	}
	bad := header(sv.Signature)
	bad.Set("webhook-timestamp", "soon")
	if got := verificationCode(verify(body, bad, WithWebhookSecrets(sv.Secret), at(0))); got != "invalid_timestamp" {
		t.Errorf("non-numeric timestamp: %q", got)
	}
}

func TestWebhookHandlerStandard(t *testing.T) {
	sv := loadVectors(t).Standard
	var got []WebhookEvent
	fail := false
	handler := New("sk_test").WebhookHandler(func(_ context.Context, e WebhookEvent) error {
		got = append(got, e)
		if fail {
			return errors.New("db down")
		}
		return nil
	}, WithWebhookSecrets(sv.Secret), WithWebhookClock(func() time.Time { return time.Unix(1614265330, 0) }))
	event := loadVectors(t).Body
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(sv.Secret, "whsec_"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(sv.ID + "." + sv.Timestamp + "." + event))
	good := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	post := func(sig string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/nozle/webhooks", strings.NewReader(event))
		r.Header.Set("webhook-id", sv.ID)
		r.Header.Set("webhook-timestamp", sv.Timestamp)
		r.Header.Set("webhook-signature", sig)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := post("v1,AAAA"); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_signature") || len(got) != 0 {
		t.Fatalf("forged: %d %s", w.Code, w.Body.String())
	}
	if w := post(good); w.Code != 200 || len(got) != 1 || got[0].ID != sv.ID || got[0].Type != "subscription.started" || got[0].OrganizationID != "org_1" {
		t.Fatalf("genuine: %d %s", w.Code, w.Body.String())
	}
	fail = true
	if w := post(good); w.Code != 500 {
		t.Fatalf("handler error: %d", w.Code)
	}
}
