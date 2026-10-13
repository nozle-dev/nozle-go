package nozle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListCreditSystemsReadsNozleID(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"credit_systems":[{"nozle_id":"new-1","code":"a"},{"id":"other","code":"b"}],"meta":{"next_page":null}}`))
	}))
	defer s.Close()
	c := New("sk_test", WithEventsURL(s.URL))
	got, err := c.ListCreditSystems(context.Background())
	// A system without nozle_id gets an empty ID, as a missing id did before.
	if err != nil || len(got) != 2 || got[0].ID != "new-1" || got[1].ID != "" {
		t.Fatalf("got %+v, err %v", got, err)
	}
}
