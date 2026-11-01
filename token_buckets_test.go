package nozle_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	nozle "github.com/nozle-dev/nozle-go"
)

// bucketRecorder is a Nozle API double that keeps the usage events and the cost events it receives.
type bucketRecorder struct {
	mu     sync.Mutex
	events chan map[string]any
	costs  []map[string]any
}

func newBucketRecorder(t *testing.T) (*nozle.Client, *bucketRecorder) {
	t.Helper()
	rec := &bucketRecorder{events: make(chan map[string]any, 8)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/features/"):
			http.NotFound(w, r)
		case r.URL.Path == "/api/v1/events":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			rec.events <- payload["event"].(map[string]any)
			_, _ = w.Write([]byte(`{"event":{}}`))
		case r.URL.Path == "/api/v1/cost-events":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			rec.mu.Lock()
			rec.costs = append(rec.costs, payload)
			rec.mu.Unlock()
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		default:
			t.Errorf("unexpected Nozle request %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return nozle.New("sk_test", nozle.WithBaseURL(server.URL), nozle.WithEventsURL(server.URL)), rec
}

func (r *bucketRecorder) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case event := <-r.events:
		return event["properties"].(map[string]any)
	case <-time.After(3 * time.Second):
		t.Fatal("no usage event recorded")
		return nil
	}
}

// costBuckets returns the recorded cost events as cost_event_id -> {type, tokens}.
func (r *bucketRecorder) costBuckets(t *testing.T, want int) map[string][2]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		n := len(r.costs)
		r.mu.Unlock()
		if n >= want || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string][2]any{}
	for _, cost := range r.costs {
		props := cost["properties"].(map[string]any)
		if cost["operation_key"] != props["type"] {
			t.Errorf("operation_key %v != type %v", cost["operation_key"], props["type"])
		}
		out[cost["cost_event_id"].(string)] = [2]any{props["type"], props["tokens"]}
	}
	return out
}

func TestRecordUsageSendsBreakdownsAndBucketCostEvents(t *testing.T) {
	billing, rec := newBucketRecorder(t)
	err := billing.RecordUsage(context.Background(), nozle.ProviderCapture{CustomerID: "c", Model: "claude", RequestID: "r1", CostMeterCode: "ai"}, nozle.Usage{
		Provider: "anthropic", InputTokens: 100, CachedInputTokens: 30, CacheWriteTokens: 10, CacheWrite1hTokens: 5, AudioInputTokens: 2,
		OutputTokens: 50, ReasoningTokens: 20, ServiceTier: "batch",
	})
	if err != nil {
		t.Fatal(err)
	}
	props := rec.next(t)
	want := map[string]any{
		"input_tokens": 100.0, "output_tokens": 50.0, "cached_input_tokens": 30.0, "cache_write_tokens": 10.0,
		"cache_write_1h_tokens": 5.0, "audio_input_tokens": 2.0, "reasoning_tokens": 20.0, "service_tier": "batch", "usage_source": "reported",
	}
	for key, value := range want {
		if props[key] != value {
			t.Errorf("%s = %v, want %v", key, props[key], value)
		}
	}
	for _, absent := range []string{"stream_aborted", "image_input_tokens", "audio_output_tokens"} {
		if _, ok := props[absent]; ok {
			t.Errorf("%s must be left out when 0", absent)
		}
	}
	got := rec.costBuckets(t, 7)
	wantCosts := map[string][2]any{
		"llm-anthropic-r1:batch_input": {"batch_input", 53.0}, "llm-anthropic-r1:cached_input": {"cached_input", 30.0},
		"llm-anthropic-r1:cache_write": {"cache_write", 10.0}, "llm-anthropic-r1:cache_write_1h": {"cache_write_1h", 5.0},
		"llm-anthropic-r1:audio_input": {"audio_input", 2.0}, "llm-anthropic-r1:batch_output": {"batch_output", 30.0},
		"llm-anthropic-r1:reasoning": {"reasoning", 20.0},
	}
	if !reflect.DeepEqual(got, wantCosts) {
		t.Fatalf("cost events = %v, want %v", got, wantCosts)
	}
}

func TestWrapHTTPClientBucketsByProviderShape(t *testing.T) {
	cases := []struct {
		name, body string
		props      map[string]any
		buckets    map[string]float64
	}{
		{
			"openai audio and cache write",
			`{"id":"o1","usage":{"prompt_tokens":100,"completion_tokens":40,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":10,"audio_tokens":5},"completion_tokens_details":{"reasoning_tokens":8,"audio_tokens":4}}}`,
			map[string]any{"input_tokens": 100.0, "output_tokens": 40.0, "cached_input_tokens": 20.0, "cache_write_tokens": 10.0, "audio_input_tokens": 5.0, "reasoning_tokens": 8.0, "audio_output_tokens": 4.0},
			map[string]float64{"input": 65, "cached_input": 20, "cache_write": 10, "audio_input": 5, "output": 28, "reasoning": 8, "audio_output": 4},
		},
		{
			"openai clamped",
			`{"id":"o2","usage":{"prompt_tokens":10,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":9,"cache_write_tokens":9,"audio_tokens":9},"completion_tokens_details":{"reasoning_tokens":99}}}`,
			map[string]any{"cached_input_tokens": 9.0, "cache_write_tokens": 1.0, "reasoning_tokens": 3.0},
			map[string]float64{"cached_input": 9, "cache_write": 1, "reasoning": 3},
		},
		{
			"anthropic cache ttl split and batch",
			`{"id":"a1","type":"message","usage":{"input_tokens":50,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20},"output_tokens":7,"service_tier":"batch"}}`,
			map[string]any{"input_tokens": 100.0, "output_tokens": 7.0, "cached_input_tokens": 20.0, "cache_write_tokens": 10.0, "cache_write_1h_tokens": 20.0, "service_tier": "batch"},
			map[string]float64{"batch_input": 50, "cached_input": 20, "cache_write": 10, "cache_write_1h": 20, "batch_output": 7},
		},
		{
			"gemini modalities",
			`{"responseId":"g1","usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":30,"candidatesTokenCount":50,"thoughtsTokenCount":6,"promptTokensDetails":[{"modality":"TEXT","tokenCount":50},{"modality":"AUDIO","tokenCount":20},{"modality":"IMAGE","tokenCount":30}],"candidatesTokensDetails":[{"modality":"AUDIO","tokenCount":12},{"modality":"IMAGE","tokenCount":8}]}}`,
			map[string]any{"input_tokens": 100.0, "output_tokens": 56.0, "cached_input_tokens": 30.0, "audio_input_tokens": 20.0, "image_input_tokens": 30.0, "reasoning_tokens": 6.0, "audio_output_tokens": 12.0, "image_output_tokens": 8.0},
			map[string]float64{"input": 20, "cached_input": 30, "audio_input": 20, "image_input": 30, "output": 30, "reasoning": 6, "audio_output": 12, "image_output": 8},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			billing, rec := newBucketRecorder(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)
			client, _ := nozle.WrapHTTPClient(nil, billing, nozle.CaptureOptions{CustomerID: "c", CostMeterCode: "ai"})
			resp, err := client.Post(server.URL+"/v1/x", "application/json", strings.NewReader(`{"model":"m"}`))
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			props := rec.next(t)
			for key, value := range tc.props {
				if props[key] != value {
					t.Errorf("%s = %v, want %v", key, props[key], value)
				}
			}
			if props["usage_source"] != "reported" {
				t.Errorf("usage_source = %v", props["usage_source"])
			}
			costs := rec.costBuckets(t, len(tc.buckets))
			if len(costs) != len(tc.buckets) {
				t.Fatalf("cost events = %v, want buckets %v", costs, tc.buckets)
			}
			for id, typeAndTokens := range costs {
				bucket := typeAndTokens[0].(string)
				if !strings.HasSuffix(id, ":"+bucket) || tc.buckets[bucket] != typeAndTokens[1] {
					t.Errorf("cost event %s = %v, want %v", id, typeAndTokens, tc.buckets)
				}
			}
		})
	}
}

// streamingProvider writes the chunks one by one, then holds the stream open until the client goes away (or, when
// cut is true, closes it without a final marker).
func streamingProvider(t *testing.T, chunks []string, cut bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range chunks {
			_, _ = io.WriteString(w, chunk)
			w.(http.Flusher).Flush()
		}
		if !cut {
			<-r.Context().Done()
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func readUntil(t *testing.T, body io.Reader, marker string) {
	t.Helper()
	seen := ""
	buf := make([]byte, 512)
	for !strings.Contains(seen, marker) {
		n, err := body.Read(buf)
		seen += string(buf[:n])
		if err != nil {
			t.Fatalf("stream ended before %q: %v", marker, err)
		}
	}
}

func post(t *testing.T, client *http.Client, url, body string) *http.Response {
	t.Helper()
	resp, err := client.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestWrapHTTPClientStreamClosedEarlyRecordsAnEstimate(t *testing.T) {
	billing, rec := newBucketRecorder(t)
	server := streamingProvider(t, []string{`data: {"id":"c1","model":"m","choices":[{"delta":{"content":"héllo wor"}}]}` + "\n\n"}, false)
	client, _ := nozle.WrapHTTPClient(nil, billing, nozle.CaptureOptions{CustomerID: "c"})
	resp := post(t, client, server.URL+"/v1/chat/completions", `{"model":"m","stream":true,"messages":[{"role":"user","content":"twelve bytes"}]}`)
	readUntil(t, resp.Body, "wor")
	_ = resp.Body.Close()

	props := rec.next(t)
	if props["stream_aborted"] != true || props["usage_source"] != "estimated" {
		t.Fatalf("wrong flags: %v", props)
	}
	if props["input_tokens"] != 3.0 || props["output_tokens"] != 3.0 { // ceil(12/4), ceil(10/4)
		t.Fatalf("estimate = %v / %v", props["input_tokens"], props["output_tokens"])
	}
}

func TestWrapHTTPClientStreamCutWithoutFinalUsageIsAborted(t *testing.T) {
	billing, rec := newBucketRecorder(t)
	server := streamingProvider(t, []string{`data: {"id":"c2","choices":[{"delta":{"content":"abcdefghi"}}]}` + "\n\n"}, true)
	client, _ := nozle.WrapHTTPClient(nil, billing, nozle.CaptureOptions{CustomerID: "c"})
	resp := post(t, client, server.URL+"/v1/chat/completions", `{"model":"m","stream":true,"messages":[]}`)
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	props := rec.next(t)
	if props["stream_aborted"] != true || props["usage_source"] != "estimated" || props["output_tokens"] != 3.0 {
		t.Fatalf("wrong event: %v", props)
	}
}

func TestWrapHTTPClientCompleteStreamHasNoAbortFlag(t *testing.T) {
	billing, rec := newBucketRecorder(t)
	server := streamingProvider(t, []string{
		`data: {"id":"c3","choices":[{"delta":{"content":"hi"}}]}` + "\n\n",
		`data: {"id":"c3","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":2}}` + "\n\n",
		"data: [DONE]\n\n",
	}, true)
	client, _ := nozle.WrapHTTPClient(nil, billing, nozle.CaptureOptions{CustomerID: "c"})
	resp := post(t, client, server.URL+"/v1/chat/completions", `{"model":"m","stream":true,"messages":[]}`)
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	props := rec.next(t)
	if _, ok := props["stream_aborted"]; ok || props["usage_source"] != "reported" || props["input_tokens"] != 8.0 {
		t.Fatalf("wrong event: %v", props)
	}
}

func TestWrapAnthropicHTTPClientRecordsAbortedStreams(t *testing.T) {
	start := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"model\":\"claude\",\"usage\":{\"input_tokens\":25,\"output_tokens\":1}}}\n\n"
	delta := func(text string) string {
		return "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"" + text + "\"}}\n\n"
	}
	for _, tc := range []struct {
		name, text string
		output     float64
		source     string
	}{
		{"estimate above the reported placeholder", strings.Repeat("a", 40), 10, "estimated"},
		{"reported output kept when the estimate is smaller", "a", 1, "reported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing, rec := newBucketRecorder(t)
			server := streamingProvider(t, []string{start, delta(tc.text)}, false)
			client, err := nozle.WrapAnthropicHTTPClient(nil, billing, nozle.ProviderWrapOptions{CustomerID: "c"})
			if err != nil {
				t.Fatal(err)
			}
			resp := post(t, client, server.URL+"/v1/messages", `{"model":"claude","stream":true,"messages":[]}`)
			readUntil(t, resp.Body, tc.text)
			_ = resp.Body.Close()

			props := rec.next(t)
			if props["stream_aborted"] != true || props["usage_source"] != tc.source {
				t.Fatalf("wrong flags: %v", props)
			}
			if props["input_tokens"] != 25.0 || props["output_tokens"] != tc.output {
				t.Fatalf("tokens = %v / %v, want 25 / %v", props["input_tokens"], props["output_tokens"], tc.output)
			}
		})
	}
}

func TestWrapOpenAIHTTPClientRecordsAbortedStream(t *testing.T) {
	billing, rec := newBucketRecorder(t)
	server := streamingProvider(t, []string{`data: {"id":"c5","model":"m","choices":[{"delta":{"content":"abcdefgh"}}]}` + "\n\n"}, false)
	client, err := nozle.WrapOpenAIHTTPClient(nil, billing, nozle.ProviderWrapOptions{CustomerID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	resp := post(t, client, server.URL+"/v1/chat/completions", `{"model":"m","stream":true,"messages":[{"role":"user","content":"0123456789"}]}`)
	readUntil(t, resp.Body, "abcdefgh")
	_ = resp.Body.Close()

	props := rec.next(t)
	if props["stream_aborted"] != true || props["usage_source"] != "estimated" || props["input_tokens"] != 3.0 || props["output_tokens"] != 2.0 {
		t.Fatalf("wrong event: %v", props)
	}
}
