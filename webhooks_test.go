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
	"sync/atomic"
	"testing"
	"time"
)

// Signed with throwaway RSA keys; the private keys are not kept.
type webhookVectors struct {
	Body                 string `json:"body"`
	PublicKeySPKI        string `json:"public_key_spki_b64"`
	PublicKeyPKCS1       string `json:"public_key_pkcs1_b64"`
	RotatedPublicKeySPKI string `json:"rotated_public_key_spki_b64"`
	JWTValid             string `json:"jwt_valid"`
	JWTOtherBody         string `json:"jwt_other_body"`
	JWTOtherKey          string `json:"jwt_other_key"`
	JWTHS256Header       string `json:"jwt_hs256_header"`
	HMACKey              string `json:"hmac_key"`
	HMACSignature        string `json:"hmac_signature"`
	Standard             struct {
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

func webhookHeader(signature, algorithm string) http.Header {
	h := http.Header{}
	h.Set("X-Nozle-Signature", signature)
	h.Set("X-Nozle-Signature-Algorithm", algorithm)
	h.Set("X-Nozle-Unique-Key", "wh_1")
	return h
}

// keyServer serves Core's public key; keys are returned in order, the last one repeating.
func keyServer(t *testing.T, keys ...string) (*Client, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/core/api/v1/webhooks/json_public_key" || r.Header.Get("Authorization") != "Bearer sk_test" {
			t.Errorf("unexpected request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		n := int(atomic.AddInt32(&calls, 1)) - 1
		if n >= len(keys) {
			n = len(keys) - 1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"webhook": map[string]string{"public_key": keys[n]}})
	}))
	t.Cleanup(server.Close)
	return New("sk_test", WithHost(server.URL)), &calls
}

func verificationCode(err error) string {
	var v *WebhookVerificationError
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}

func TestVerifyWebhookHMAC(t *testing.T) {
	v := loadVectors(t)
	c := New("sk_test")
	event, err := c.VerifyWebhook(context.Background(), []byte(v.Body), webhookHeader(v.HMACSignature, "hmac"), v.HMACKey)
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "wh_1" || event.Type != "subscription.started" || event.ObjectType != "subscription" || event.OrganizationID != "org_1" {
		t.Fatalf("event = %+v", event)
	}
	var sub struct {
		PlanCode string `json:"plan_code"`
	}
	if json.Unmarshal(event.Object, &sub) != nil || sub.PlanCode != "pro" {
		t.Fatalf("object = %s", event.Object)
	}
	cases := []struct {
		body, key, code string
	}{
		{strings.Replace(v.Body, "pro", "max", 1), v.HMACKey, "invalid_signature"},
		{v.Body, "other", "invalid_signature"},
		{v.Body, "", "missing_hmac_key"},
	}
	for _, tc := range cases {
		_, err := c.VerifyWebhook(context.Background(), []byte(tc.body), webhookHeader(v.HMACSignature, "hmac"), tc.key)
		if verificationCode(err) != tc.code {
			t.Errorf("want %s, got %v", tc.code, err)
		}
	}
	if _, err := c.VerifyWebhook(context.Background(), []byte(v.Body), http.Header{}, ""); verificationCode(err) != "missing_signature" {
		t.Errorf("missing headers: %v", err)
	}
	if _, err := c.VerifyWebhook(context.Background(), []byte(v.Body), webhookHeader("x", "md5"), ""); verificationCode(err) != "unsupported_algorithm" {
		t.Errorf("md5: %v", err)
	}
}

func TestVerifyWebhookJWT(t *testing.T) {
	v := loadVectors(t)
	c, calls := keyServer(t, v.PublicKeySPKI)
	for i := 0; i < 2; i++ {
		if _, err := c.VerifyWebhook(context.Background(), []byte(v.Body), webhookHeader(v.JWTValid, "jwt"), ""); err != nil {
			t.Fatal(err)
		}
	}
	if *calls != 1 {
		t.Fatalf("public key read %d times, want 1", *calls)
	}
	for _, token := range []string{v.JWTOtherBody, v.JWTOtherKey, v.JWTHS256Header} {
		if _, err := c.VerifyWebhook(context.Background(), []byte(v.Body), webhookHeader(token, "jwt"), ""); verificationCode(err) != "invalid_signature" {
			t.Errorf("want invalid_signature, got %v", err)
		}
	}
}

func TestVerifyWebhookJWTRotatedKey(t *testing.T) {
	v := loadVectors(t)
	c, calls := keyServer(t, v.RotatedPublicKeySPKI, v.PublicKeySPKI)
	if _, err := c.VerifyWebhook(context.Background(), []byte(v.Body), webhookHeader(v.JWTValid, "jwt"), ""); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatalf("public key read %d times, want 2", *calls)
	}
}

func TestParseRSAPublicKeyFormats(t *testing.T) {
	v := loadVectors(t)
	spki, err1 := parseRSAPublicKey(v.PublicKeySPKI)
	pkcs1, err2 := parseRSAPublicKey(v.PublicKeyPKCS1)
	if err1 != nil || err2 != nil || !spki.Equal(pkcs1) {
		t.Fatalf("spki %v pkcs1 %v", err1, err2)
	}
}

func TestWebhookHandler(t *testing.T) {
	v := loadVectors(t)
	c := New("sk_test")
	var got []WebhookEvent
	handler := c.WebhookHandler(v.HMACKey, func(_ context.Context, e WebhookEvent) error {
		got = append(got, e)
		if e.ID == "fail" {
			return errors.New("db down")
		}
		return nil
	})
	post := func(signature, id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/nozle/webhooks", strings.NewReader(v.Body))
		r.Header = webhookHeader(signature, "hmac")
		r.Header.Set("X-Nozle-Unique-Key", id)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := post(v.HMACSignature, "wh_1"); w.Code != 200 || len(got) != 1 {
		t.Fatalf("genuine: %d %d", w.Code, len(got))
	}
	if w := post("bad", "wh_2"); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_signature") || len(got) != 1 {
		t.Fatalf("forged: %d %s", w.Code, w.Body.String())
	}
	if w := post(v.HMACSignature, "fail"); w.Code != 500 {
		t.Fatalf("handler error: %d", w.Code)
	}
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
		h.Set("X-Nozle-Signature-Algorithm", "standard")
		h.Set("X-Nozle-Unique-Key", sv.ID)
		return h
	}
	body := []byte(sv.Body)
	c := New("sk_test") // no host: any network call would fail the test
	verify := func(body []byte, h http.Header, opts ...WebhookOption) error {
		_, err := c.VerifyWebhook(context.Background(), body, h, "", opts...)
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
		{"301 s old", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), at(301 * time.Second)}, "invalid_signature"},
		{"299 s old", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), at(299 * time.Second)}, "invalid_payload"},
		{"301 s in the future", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), at(-301 * time.Second)}, "invalid_signature"},
		{"second of two signatures", body, header("v1,AAAA " + sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), at(0)}, "invalid_payload"},
		{"rotated secrets", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(secondSecret, sv.Secret), at(0)}, "invalid_payload"},
		{"v2 entry ignored", body, header("v2," + strings.TrimPrefix(sv.Signature, "v1,")), []WebhookOption{WithWebhookSecrets(sv.Secret), at(0)}, "invalid_signature"},
		{"missing signature header", body, header(""), []WebhookOption{WithWebhookSecrets(sv.Secret), at(0)}, "missing_signature"},
		{"no secret configured", body, header(sv.Signature), []WebhookOption{at(0)}, "missing_webhook_secret"},
		{"custom tolerance", body, header(sv.Signature), []WebhookOption{WithWebhookSecrets(sv.Secret), WithWebhookTolerance(10 * time.Second), at(11 * time.Second)}, "invalid_signature"},
	}
	for _, tc := range cases {
		if got := verificationCode(verify(tc.body, tc.h, tc.opts...)); got != tc.want {
			t.Errorf("%s: code %q, want %q", tc.name, got, tc.want)
		}
	}
	bad := header(sv.Signature)
	bad.Set("webhook-timestamp", "soon")
	if got := verificationCode(verify(body, bad, WithWebhookSecrets(sv.Secret), at(0))); got != "invalid_signature" {
		t.Errorf("non-numeric timestamp: %q", got)
	}
	// webhook-signature alone selects the scheme, with no algorithm header.
	h := header(sv.Signature)
	h.Del("X-Nozle-Signature-Algorithm")
	if err := verify(body, h, WithWebhookSecrets(sv.Secret), at(0)); !ok(err) {
		t.Fatalf("no algorithm header: %v", err)
	}
}

func TestWebhookHandlerStandard(t *testing.T) {
	sv := loadVectors(t).Standard
	var got []WebhookEvent
	handler := New("sk_test").WebhookHandler("", func(_ context.Context, e WebhookEvent) error {
		got = append(got, e)
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
	if w := post("v1,AAAA"); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_signature") {
		t.Fatalf("forged: %d %s", w.Code, w.Body.String())
	}
	if w := post(good); w.Code != 200 || len(got) != 1 || got[0].ID != sv.ID || got[0].Type != "subscription.started" {
		t.Fatalf("genuine: %d %s", w.Code, w.Body.String())
	}
}
