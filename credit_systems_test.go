package nozle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListCreditSystemsReadsNozleIDWithLegacyFallback(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"credit_systems":[{"nozle_id":"new-1","code":"a"},{"lago_id":"old-1","code":"b"}],"meta":{"next_page":null}}`))
	}))
	defer s.Close()
	c := New("sk_test", WithEventsURL(s.URL))
	got, err := c.ListCreditSystems(context.Background())
	if err != nil || len(got) != 2 || got[0].ID != "new-1" || got[1].ID != "old-1" {
		t.Fatalf("got %+v, err %v", got, err)
	}
}
