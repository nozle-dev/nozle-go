package providercheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	nozle "github.com/nozle-dev/nozle-go"
	openai "github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"
)

func TestOfficialGoClientsWithAutomaticCapture(t *testing.T) {
	events := make(chan map[string]any, 2)
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		events <- body
		_, _ = w.Write([]byte(`{"event":{}}`))
	}))
	defer billingServer.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"id":"chat-1","object":"chat.completion","created":1,"model":"test-openai","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`))
		case "/v1/messages":
			_, _ = w.Write([]byte(`{"id":"msg-1","type":"message","role":"assistant","model":"test-anthropic","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":4}}`))
		default:
			t.Errorf("unexpected provider path %s", r.URL.Path)
		}
	}))
	defer provider.Close()
	billing := nozle.New("sk_test", nozle.WithEventsURL(billingServer.URL))
	openaiHTTP, err := nozle.WrapOpenAIHTTPClient(nil, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1", SubscriptionID: "subscription-1"})
	if err != nil {
		t.Fatal(err)
	}
	openaiClient := openai.NewClient(openaiopt.WithAPIKey("test"), openaiopt.WithBaseURL(provider.URL+"/v1"), openaiopt.WithHTTPClient(openaiHTTP))
	completion, err := openaiClient.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{Model: "test-openai", Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	if completion.ID != "chat-1" {
		t.Fatalf("wrong OpenAI response: %+v", completion)
	}
	anthropicHTTP, err := nozle.WrapAnthropicHTTPClient(nil, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1", SubscriptionID: "subscription-1"})
	if err != nil {
		t.Fatal(err)
	}
	anthropicClient := anthropic.NewClient(anthropicopt.WithAPIKey("test"), anthropicopt.WithBaseURL(provider.URL), anthropicopt.WithHTTPClient(anthropicHTTP))
	message, err := anthropicClient.Messages.New(context.Background(), anthropic.MessageNewParams{Model: "test-anthropic", MaxTokens: 10, Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))}})
	if err != nil {
		t.Fatal(err)
	}
	if message.ID != "msg-1" {
		t.Fatalf("wrong Anthropic response: %+v", message)
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case event := <-events:
			properties := event["event"].(map[string]any)["properties"].(map[string]any)
			seen[properties["provider"].(string)] = true
		case <-time.After(3 * time.Second):
			t.Fatal("automatic usage event missing")
		}
	}
	if !seen["openai"] || !seen["anthropic"] {
		t.Fatalf("missing provider event: %#v", seen)
	}
}

func TestOfficialGoClientStreamsWithAutomaticCapture(t *testing.T) {
	events := make(chan map[string]any, 2)
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		events <- body
		_, _ = w.Write([]byte(`{"event":{}}`))
	}))
	defer billingServer.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		switch r.URL.Path {
		case "/v1/chat/completions":
			var request map[string]any
			_ = json.NewDecoder(r.Body).Decode(&request)
			options, _ := request["stream_options"].(map[string]any)
			if options["include_usage"] != true {
				t.Errorf("OpenAI stream usage missing: %#v", request)
			}
			_, _ = w.Write([]byte("data: {\"id\":\"chat-stream\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-openai\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"id\":\"chat-stream\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-openai\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\ndata: [DONE]\n\n"))
		case "/v1/messages":
			_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-stream\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"test-anthropic\",\"content\":[],\"usage\":{\"input_tokens\":8,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
		default:
			t.Errorf("unexpected provider path %s", r.URL.Path)
		}
	}))
	defer provider.Close()
	billing := nozle.New("sk_test", nozle.WithEventsURL(billingServer.URL))
	openaiHTTP, _ := nozle.WrapOpenAIHTTPClient(nil, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1", SubscriptionID: "subscription-1"})
	openaiClient := openai.NewClient(openaiopt.WithAPIKey("test"), openaiopt.WithBaseURL(provider.URL+"/v1"), openaiopt.WithHTTPClient(openaiHTTP))
	openaiStream := openaiClient.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{Model: "test-openai", Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")}})
	for openaiStream.Next() {
		_ = openaiStream.Current()
	}
	if err := openaiStream.Err(); err != nil {
		t.Fatal(err)
	}
	_ = openaiStream.Close()
	anthropicHTTP, _ := nozle.WrapAnthropicHTTPClient(nil, billing, nozle.ProviderWrapOptions{CustomerID: "customer-1", SubscriptionID: "subscription-1"})
	anthropicClient := anthropic.NewClient(anthropicopt.WithAPIKey("test"), anthropicopt.WithBaseURL(provider.URL), anthropicopt.WithHTTPClient(anthropicHTTP))
	anthropicStream := anthropicClient.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{Model: "test-anthropic", MaxTokens: 10, Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))}})
	for anthropicStream.Next() {
		_ = anthropicStream.Current()
	}
	if err := anthropicStream.Err(); err != nil {
		t.Fatal(err)
	}
	_ = anthropicStream.Close()
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case event := <-events:
			properties := event["event"].(map[string]any)["properties"].(map[string]any)
			seen[properties["provider"].(string)] = true
		case <-time.After(3 * time.Second):
			t.Fatal("automatic stream usage event missing")
		}
	}
	if !seen["openai"] || !seen["anthropic"] {
		t.Fatalf("missing provider stream event: %#v", seen)
	}
}
