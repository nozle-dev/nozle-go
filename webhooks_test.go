package nozle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
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
