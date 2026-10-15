package nozle_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

// usageRecorder is a Nozle API double that records usage events and answers the subscription lookup.
func usageRecorder(t *testing.T, fail bool) (*nozle.Client, chan map[string]any) {
	t.Helper()
	events := make(chan map[string]any, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/features/"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subscriptions":
			_, _ = w.Write([]byte(`{"subscriptions":[{"external_id":"sub-1","status":"active"}]}`))
		case r.URL.Path == "/api/v1/events":
			if fail {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			events <- payload["event"].(map[string]any)
			_, _ = w.Write([]byte(`{"event":{}}`))
		default:
			t.Errorf("unexpected Nozle request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return nozle.New("sk_test", nozle.WithBaseURL(server.URL), nozle.WithEventsURL(server.URL)), events
}

func provider(t *testing.T, contentType, body string, seen *http.Request) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = *r.Clone(context.Background())
			raw, _ := io.ReadAll(r.Body)
			seen.Body = io.NopCloser(bytes.NewReader(raw))
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func nextEvent(t *testing.T, events chan map[string]any) map[string]any {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("no usage event recorded")
		return nil
	}
}

func TestWrapHTTPClientRecordsUsageByResponseShape(t *testing.T) {
	cases := []struct {
		name, body, format, id string
		input, output          float64
	}{
		{"openai compatible", `{"id":"c1","model":"m","usage":{"prompt_tokens":10,"completion_tokens":5}}`, "openai", "c1", 10, 5},
		{"responses api", `{"id":"r1","object":"response","usage":{"input_tokens":10,"output_tokens":4,"input_tokens_details":{"cached_tokens":3}}}`, "openai", "r1", 10, 4},
		{"anthropic", `{"id":"msg_1","type":"message","model":"claude","usage":{"input_tokens":10,"cache_read_input_tokens":5,"output_tokens":2}}`, "anthropic", "msg_1", 15, 2},
		{"gemini", `{"responseId":"g1","modelVersion":"gemini-x","usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":6,"thoughtsTokenCount":4}}`, "gemini", "g1", 12, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			billing, events := usageRecorder(t, false)
			server := provider(t, "application/json", tc.body, nil)
			client, err := nozle.WrapHTTPClient(nil, billing, nozle.CaptureOptions{CustomerID: "cust_1"})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := client.Post(server.URL+"/v1/anything", "application/json", strings.NewReader(`{"model":"m"}`))
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(body) != tc.body {
				t.Fatalf("response changed: %s", body)
			}
			event := nextEvent(t, events)
			props := event["properties"].(map[string]any)
			if props["provider"] != tc.format || props["input_tokens"] != tc.input || props["output_tokens"] != tc.output {
				t.Fatalf("wrong properties: %#v", props)
			}
			if event["transaction_id"] != "llm-"+tc.format+"-"+tc.id {
				t.Fatalf("transaction id = %v", event["transaction_id"])
			}
			if props["provider_host"] != strings.TrimPrefix(server.URL, "http://") {
				t.Fatalf("provider_host = %v", props["provider_host"])
			}
		})
	}
}

func TestWrapHTTPClientStreamsAndRequestCustomer(t *testing.T) {
	billing, events := usageRecorder(t, false)
	var seen http.Request
	stream := "data: {\"id\":\"c2\",\"choices\":[{\"delta\":{\"content\":\"Hi\"}}],\"usage\":null}\n\n" +
		"data: {\"id\":\"c2\",\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
	server := provider(t, "text/event-stream", stream, &seen)
	client, _ := nozle.WrapHTTPClient(nil, billing, nozle.CaptureOptions{CustomerIDFunc: func(*http.Request) string { return "fallback" }})

	req, _ := http.NewRequestWithContext(nozle.WithCustomer(context.Background(), "cust_ctx"), http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[],"stream":true}`))
	req.Header.Set("X-Nozle-Feature", "chat")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Contains(body, []byte(`"content":"Hi"`)) {
		t.Fatalf("stream changed: %s", body)
	}
	sent, _ := io.ReadAll(seen.Body)
	if !bytes.Contains(sent, []byte(`"stream_options":{"include_usage":true}`)) {
		t.Fatalf("stream usage not requested: %s", sent)
	}
	if seen.Header.Get("X-Nozle-Feature") != "" {
		t.Fatal("Nozle header forwarded to the provider")
	}
	event := nextEvent(t, events)
	props := event["properties"].(map[string]any)
	if event["external_customer_id"] != "cust_ctx" || props["feature"] != "chat" || props["input_tokens"] != float64(8) {
		t.Fatalf("wrong event: %#v", event)
	}
}

func TestWrapHTTPClientAnthropicStreamHeaderCustomer(t *testing.T) {
	billing, events := usageRecorder(t, false)
	stream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_2\",\"model\":\"claude\",\"usage\":{\"input_tokens\":20,\"output_tokens\":1}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{},\"usage\":{\"output_tokens\":15}}\n\n"
	var seen http.Request
	server := provider(t, "text/event-stream", stream, &seen)
	client, _ := nozle.WrapHTTPClient(&http.Client{}, billing, nozle.CaptureOptions{})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", strings.NewReader(`{"stream":true}`))
	req.Header.Set("X-Nozle-Customer-Id", "cust_header")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if seen.Header.Get("X-Nozle-Customer-Id") != "" {
		t.Fatal("customer header forwarded")
	}
	sent, _ := io.ReadAll(seen.Body)
	if bytes.Contains(sent, []byte("stream_options")) {
		t.Fatalf("non-chat request changed: %s", sent)
	}
	event := nextEvent(t, events)
	props := event["properties"].(map[string]any)
	if event["external_customer_id"] != "cust_header" || props["provider"] != "anthropic" || props["input_tokens"] != float64(20) || props["output_tokens"] != float64(15) {
		t.Fatalf("wrong event: %#v", event)
	}
}

func TestWrapHTTPClientWithoutCustomerOrOnFailureLeavesTheCallAlone(t *testing.T) {
	billing, events := usageRecorder(t, true)
	var seen http.Request
	server := provider(t, "application/json", `{"id":"c3","usage":{"prompt_tokens":1,"completion_tokens":1}}`, &seen)

	anonymous, _ := nozle.WrapHTTPClient(nil, billing, nozle.CaptureOptions{})
	resp, err := anonymous.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"messages":[],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if sent, _ := io.ReadAll(seen.Body); bytes.Contains(sent, []byte("stream_options")) {
		t.Fatal("request changed without a customer")
	}

	failures := make(chan error, 1)
	named, _ := nozle.WrapHTTPClient(nil, billing, nozle.CaptureOptions{CustomerID: "cust_1", OnError: func(err error) { failures <- err }})
	resp, err = named.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("provider call affected: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	select {
	case err := <-failures:
		var apiErr *nozle.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("unexpected failure: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("failure not reported")
	}
	select {
	case event := <-events:
		t.Fatalf("event recorded despite failure: %#v", event)
	default:
	}
}

func TestRecordUsageSplitsCategories(t *testing.T) {
	billing, events := usageRecorder(t, false)
	err := billing.RecordUsage(context.Background(), nozle.ProviderCapture{CustomerID: "cust_1", Model: "m", RequestID: "r9"}, nozle.Usage{Provider: "openai", InputTokens: 100, CachedInputTokens: 30, OutputTokens: 50, ReasoningTokens: 20})
	if err != nil {
		t.Fatal(err)
	}
	event := nextEvent(t, events)
	if event["transaction_id"] != "llm-openai-r9" || event["properties"].(map[string]any)["input_tokens"] != float64(100) {
		t.Fatalf("wrong event: %#v", event)
	}
	if err := billing.RecordUsage(context.Background(), nozle.ProviderCapture{}, nozle.Usage{}); err == nil {
		t.Fatal("missing customer accepted")
	}
}

func TestWrapHTTPClientReportsAMissingFeatureOnce(t *testing.T) {
	billing, events := usageRecorder(t, false)
	server := provider(t, "application/json", `{"id":"c9","usage":{"prompt_tokens":1,"completion_tokens":1}}`, nil)
	failures := make(chan error, 4)
	client, _ := nozle.WrapHTTPClient(nil, billing, nozle.CaptureOptions{CustomerID: "cust_1", OnError: func(err error) { failures <- err }})
	for i := 0; i < 2; i++ {
		resp, err := client.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		nextEvent(t, events)
	}
	select {
	case err := <-failures:
		var setup *nozle.SetupError
		if !errors.As(err, &setup) || setup.Code != "feature_not_found" {
			t.Fatalf("unexpected report: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing Feature not reported")
	}
	time.Sleep(100 * time.Millisecond)
	if len(failures) != 0 {
		t.Fatalf("reported more than once: %v", <-failures)
	}
}
