# Automatic OpenAI and Anthropic usage capture

The Go SDK can capture usage automatically from the official provider clients. Wrap the `*http.Client` once, then give it to the provider SDK. Your normal completion and streaming calls do not need a separate Nozle capture call.

```go
nozleClient := nozle.New(os.Getenv("NOZLE_SECRET_KEY"))
capture := nozle.ProviderWrapOptions{
    CustomerID: "workspace_123",
    SubscriptionID: "workspace_123_subscription",
    FeatureCode: "llm_tokens",
    CostMeterCode: "ai_tokens",
    OnError: func(err error) { log.Printf("Nozle usage tracking: %v", err) },
}

openaiHTTP, err := nozle.WrapOpenAIHTTPClient(&http.Client{}, nozleClient, capture)
if err != nil { return err }
openaiClient := openai.NewClient(openaiopt.WithHTTPClient(openaiHTTP))

anthropicHTTP, err := nozle.WrapAnthropicHTTPClient(&http.Client{}, nozleClient, capture)
if err != nil { return err }
anthropicClient := anthropic.NewClient(anthropicopt.WithHTTPClient(anthropicHTTP))
```

Use `openaiClient.Chat.Completions.New` or `NewStreaming`, and `anthropicClient.Messages.New` or `NewStreaming` as usual. The OpenAI wrapper requests final stream usage automatically. The provider response stays available even if Nozle telemetry fails; `OnError` receives that failure. No prompt or completion text is sent to Nozle.

The integration tests use the official Go provider clients against local fake provider and Nozle servers. Run `go test ./...` in this directory. The pinned provider versions keep this example compatible with Go 1.22; newer provider SDK versions may require a newer Go release.
