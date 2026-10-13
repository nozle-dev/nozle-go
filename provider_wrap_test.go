package nozle_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

func TestWrapOpenAIHTTPClientCapturesCompletionAndPreservesResponse(t *testing.T) {
	events := make(chan map[string]any, 1)
	costs := make(chan map[string]any, 4)
	done := make(chan error, 1)
	nozleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/api/v1/events":
			events <- payload
			_, _ = w.Write([]byte(`{"event":{}}`))
		case "/api/v1/cost-events":
			costs <- payload
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		default:
			t.Errorf("unexpected Nozle path %s", r.URL.Path)
		}
	}))
	defer nozleServer.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected provider path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat-123","model":"test-openai","choices":[{"message":{"content":"provider reply"}}],"usage":{"prompt_tokens":12,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`))
	}))
	defer provider.Close()
	billing := nozle.New("sk_test", nozle.WithBaseURL(nozleServer.URL), nozle.WithEventsURL(nozleServer.URL))
	client, err := nozle.WrapOpenAIHTTPClient(&http.Client{}, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1", SubscriptionID: "subscription-1", CostMeterCode: "ai_tokens", OnResult: func(err error) { done <- err }})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(provider.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{"model":"test-openai","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !bytes.Contains(body, []byte("provider reply")) {
		t.Fatalf("provider response changed: %s", body)
	}
	select {
	case event := <-events:
		properties := event["event"].(map[string]any)["properties"].(map[string]any)
		if properties["provider"] != "openai" || properties["input_tokens"] != float64(12) || properties["output_tokens"] != float64(5) {
			t.Fatalf("wrong event properties: %#v", properties)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no Feature Event captured")
	}
	for i := 0; i < 4; i++ {
		select {
		case <-costs:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d Cost Events captured", i)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("capture failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("capture result missing")
	}
}

func TestWrapOpenAIHTTPClientAddsStreamUsageAndCapturesFinalChunk(t *testing.T) {
	events := make(chan map[string]any, 1)
	nozleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		events <- payload
		_, _ = w.Write([]byte(`{"event":{}}`))
	}))
	defer nozleServer.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		options, _ := request["stream_options"].(map[string]any)
		if options["include_usage"] != true {
			t.Errorf("stream usage not requested: %#v", request)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chat-stream\",\"model\":\"test-openai\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"id\":\"chat-stream\",\"model\":\"test-openai\",\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n"))
	}))
	defer provider.Close()
	billing := nozle.New("sk_test", nozle.WithBaseURL(nozleServer.URL), nozle.WithEventsURL(nozleServer.URL))
	client, err := nozle.WrapOpenAIHTTPClient(&http.Client{}, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1", SubscriptionID: "subscription-1"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(provider.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{"model":"test-openai","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	select {
	case event := <-events:
		properties := event["event"].(map[string]any)["properties"].(map[string]any)
		if properties["input_tokens"] != float64(8) || properties["output_tokens"] != float64(3) {
			t.Fatalf("wrong stream usage: %#v", properties)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no streamed Feature Event captured")
	}
}

func TestWrapAnthropicHTTPClientCapturesStream(t *testing.T) {
	events := make(chan map[string]any, 1)
	nozleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		events <- payload
		_, _ = w.Write([]byte(`{"event":{}}`))
	}))
	defer nozleServer.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-1\",\"model\":\"test-anthropic\",\"usage\":{\"input_tokens\":8,\"cache_read_input_tokens\":1,\"cache_creation_input_tokens\":2}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n"))
	}))
	defer provider.Close()
	billing := nozle.New("sk_test", nozle.WithBaseURL(nozleServer.URL), nozle.WithEventsURL(nozleServer.URL))
	client, err := nozle.WrapAnthropicHTTPClient(&http.Client{}, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1", SubscriptionID: "subscription-1"})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", provider.URL+"/v1/messages", bytes.NewBufferString(`{"model":"test-anthropic","stream":true}`))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	select {
	case event := <-events:
		properties := event["event"].(map[string]any)["properties"].(map[string]any)
		if properties["input_tokens"] != float64(11) || properties["output_tokens"] != float64(3) {
			t.Fatalf("wrong stream usage: %#v", properties)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no Anthropic Feature Event captured")
	}
}

func TestWrapAnthropicHTTPClientCapturesCompletion(t *testing.T) {
	events := make(chan map[string]any, 1)
	nozleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		events <- payload
		_, _ = w.Write([]byte(`{"event":{}}`))
	}))
	defer nozleServer.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg-123","model":"test-anthropic","content":[{"type":"text","text":"provider reply"}],"usage":{"input_tokens":8,"cache_read_input_tokens":1,"cache_creation_input_tokens":2,"output_tokens":3}}`))
	}))
	defer provider.Close()
	billing := nozle.New("sk_test", nozle.WithBaseURL(nozleServer.URL), nozle.WithEventsURL(nozleServer.URL))
	client, err := nozle.WrapAnthropicHTTPClient(nil, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1", SubscriptionID: "subscription-1"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(provider.URL+"/v1/messages", "application/json", bytes.NewBufferString(`{"model":"test-anthropic"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !bytes.Contains(body, []byte("provider reply")) {
		t.Fatalf("response changed: %s", body)
	}
	select {
	case event := <-events:
		properties := event["event"].(map[string]any)["properties"].(map[string]any)
		if properties["input_tokens"] != float64(11) || properties["output_tokens"] != float64(3) {
			t.Fatalf("wrong usage: %#v", properties)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no Anthropic event")
	}
}

func TestWrappedProviderFailureDoesNotEmitUsage(t *testing.T) {
	events := make(chan struct{}, 1)
	nozleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { events <- struct{}{} }))
	defer nozleServer.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
	}))
	defer provider.Close()
	billing := nozle.New("sk_test", nozle.WithBaseURL(nozleServer.URL), nozle.WithEventsURL(nozleServer.URL))
	client, err := nozle.WrapOpenAIHTTPClient(nil, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(provider.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status changed: %d", resp.StatusCode)
	}
	select {
	case <-events:
		t.Fatal("unexpected Nozle event")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWrappedTelemetryFailurePreservesProviderResponse(t *testing.T) {
	errorsSeen := make(chan error, 1)
	nozleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"publisher unavailable"}`))
	}))
	defer nozleServer.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"chat-1","model":"test","usage":{"prompt_tokens":2,"completion_tokens":1},"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer provider.Close()
	billing := nozle.New("sk_test", nozle.WithBaseURL(nozleServer.URL), nozle.WithEventsURL(nozleServer.URL))
	client, err := nozle.WrapOpenAIHTTPClient(nil, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1", SubscriptionID: "subscription-1", OnError: func(err error) { errorsSeen <- err }})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(provider.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !bytes.Contains(body, []byte("answer")) {
		t.Fatalf("provider response lost: %s", body)
	}
	select {
	case err := <-errorsSeen:
		var apiErr *nozle.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("wrong telemetry error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("telemetry failure not reported")
	}
}
