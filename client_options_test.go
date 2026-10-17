package nozle

import "testing"

func TestBaseURLDoesNotChangeCoreURL(t *testing.T) {
	c := New("sk_test", WithBaseURL("http://localhost:8888/engine"))
	if got := c.endpoint(true); got != DefaultCoreURL {
		t.Fatalf("Core URL = %q, want %q", got, DefaultCoreURL)
	}
}

func TestURLOverridesAreIndependentInEitherOrder(t *testing.T) {
	for _, opts := range [][]Option{
		{WithBaseURL("http://localhost:8888/engine"), WithEventsURL("http://localhost:8888/core")},
		{WithEventsURL("http://localhost:8888/core"), WithBaseURL("http://localhost:8888/engine")},
	} {
		c := New("sk_test", opts...)
		if c.endpoint(false) != "http://localhost:8888/engine" || c.endpoint(true) != "http://localhost:8888/core" {
			t.Fatalf("URLs = %q, %q", c.endpoint(false), c.endpoint(true))
		}
	}
}

func TestHostDerivesBothServiceURLsAndExplicitURLsWin(t *testing.T) {
	c := New("sk_test", WithHost("http://localhost:8888//"))
	if c.endpoint(false) != "http://localhost:8888/engine" || c.endpoint(true) != "http://localhost:8888/core" {
		t.Fatalf("URLs = %q, %q", c.endpoint(false), c.endpoint(true))
	}
	c = New("sk_test", WithBaseURL("https://engine.example/"), WithHost("http://localhost:8888"))
	if c.endpoint(false) != "https://engine.example" || c.endpoint(true) != "http://localhost:8888/core" {
		t.Fatalf("URLs = %q, %q", c.endpoint(false), c.endpoint(true))
	}
}

func TestAPIKeyFallsBackToEnvironment(t *testing.T) {
	t.Setenv("NOZLE_SECRET_KEY", "sk_env")
	if c := New(""); c.apiKey != "sk_env" {
		t.Fatalf("apiKey = %q", c.apiKey)
	}
	if c := New("sk_explicit"); c.apiKey != "sk_explicit" {
		t.Fatalf("apiKey = %q", c.apiKey)
	}
	t.Setenv("NOZLE_SECRET_KEY", "")
	defer func() {
		if recover() == nil {
			t.Fatal("New with no key and no NOZLE_SECRET_KEY must panic")
		}
	}()
	New("  ")
}
