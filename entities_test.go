package nozle_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

func TestUpsertEntity_externalIDOnlyInPath(t *testing.T) {
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Write([]byte(`{"action":"created","entity":{"external_id":"user_42"}}`))
	}))
	defer srv.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
	in := nozle.EntityInput{ExternalID: "user_42", Name: "Asha", Status: "active"}
	if _, err := c.UpsertEntity(context.Background(), "workspace_123", "user_42", in, "key"); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/customers/workspace_123/entities/user_42" {
		t.Errorf("path: %s", path)
	}
	if _, ok := body["external_id"]; ok {
		t.Errorf("body has external_id: %v", body)
	}
	if body["name"] != "Asha" || body["status"] != "active" {
		t.Errorf("body: %v", body)
	}
	if in.ExternalID != "user_42" {
		t.Errorf("caller input changed: %+v", in)
	}
}

func TestUpsertEntity_differentExternalIDFailsLocally(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	c := nozle.New("sk_test", nozle.WithBaseURL(srv.URL), nozle.WithEventsURL(srv.URL))
	_, err := c.UpsertEntity(context.Background(), "workspace_123", "user_42", nozle.EntityInput{ExternalID: "user_43", Status: "active"}, "key")
	var ve *nozle.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}
