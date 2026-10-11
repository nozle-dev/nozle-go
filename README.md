# Nozle Go SDK

Typed server-side Go SDK for Nozle billing, subscriptions, Entities, product credits, usage, Cost Events, margin reporting, and OpenAI/Anthropic usage capture. Requires Go 1.22 or newer.

## Install

```sh
go get github.com/nozle-dev/nozle-go
```

The module path is `github.com/nozle-dev/nozle-go`.

## Authentication and setup

Keep a secret `sk_` key on your backend. A `pk_` key may list plans; every protected customer, billing, event, Entity, credit, usage, and margin operation requires `sk_`. `New` rejects an empty key. For browser requests, authenticate the user in your application and derive the Nozle customer ID on your backend. Do not trust a customer ID supplied by the browser.

```go
ctx := context.Background()
client := nozle.New(os.Getenv("NOZLE_SECRET_KEY"))
plans, err := client.Plans(ctx)
if err != nil { return err }
_ = plans
```

Import `nozle "github.com/nozle-dev/nozle-go"` and the Go standard library packages used by each snippet. Snippets below assume `ctx` and `client` are defined as above. The [runnable basic example](examples/basic/README.md) reads Engine and Core using environment variables.

The default URLs are Engine `https://api.nozle.app/engine` and Core `https://api.nozle.app/core`. Overrides are independent. For a local stack:

```go
client := nozle.New(os.Getenv("NOZLE_SECRET_KEY"),
    nozle.WithBaseURL("http://localhost:8888/engine"),
    nozle.WithEventsURL("http://localhost:8888/core"),
    nozle.WithTimeout(15*time.Second),
)
```

`WithHTTPClient` accepts an `*http.Client` for custom transports. Every SDK method accepts `context.Context`; cancellation and deadlines propagate to HTTP requests. The default request timeout is 10 seconds. Reuse a client across calls.

## Billing and checkout

`Plans` lists available plans. `Checkout` creates a payment-aware checkout for a customer; use an HTTPS return URL whose origin is permitted by your Nozle deployment. Keep a stable idempotency key for one checkout attempt.

```go
checkout, err := client.Checkout(ctx, nozle.CheckoutParams{
    CustomerID: "customer-123",
    PlanCode: "pro_monthly",
    ReturnURL: "https://merchant.example/billing/complete",
    IdempotencyKey: "customer-123-pro-v1",
})
if err != nil { return err }
_ = checkout
```

The result can be `stripe`, `razorpay`, `hosted`, `processing`, `completed`, or `scheduled`, depending on the payment and plan transition. Handle the returned type and fields; a checkout URL or provider callback alone does not prove fulfillment. Use `CheckoutStatus` for the authoritative status. `CheckoutInvoice` starts collection for an existing invoice; `VerifyCheckout` verifies a Razorpay callback. Scope status and verification to both customer and subscription IDs when recovering a customer checkout:

```go
scope := nozle.CheckoutScope{CustomerID: "customer-123", SubscriptionID: "subscription-123"}
status, err := client.CheckoutStatus(ctx, checkout.CheckoutID, scope)
if err != nil { return err }
_ = status
```

`Subscribe` creates a backend subscription. `CancelSubscription` defaults to `end_of_period` when its policy argument is empty; pass `immediate` only when termination now is intended.

```go
subscription, err := client.Subscribe(ctx, nozle.SubscribeParams{CustomerID: "customer-123", PlanCode: "free"})
if err != nil { return err }
cancel, err := client.CancelSubscription(ctx, "customer-123", subscription.SubscriptionID, "end_of_period")
if err != nil { return err }
_ = cancel
```

For subscription self-service, `SubscriptionOptions` returns eligible plans and pending state. `PreviewSubscriptionChange` returns a signed, opaque quote for a plan change. Pass that quote into `Checkout` with the selected subscription. `PreviewSubscriptionTransition` and `ApplySubscriptionTransition` handle cancel, uncancel, and scheduled downgrade flows; `WithdrawPendingSubscriptionChange` targets the exact pending subscription. An end-of-period cancel can carry the preview's exact `EffectiveAt` in `ExpectedEffectiveAt` to reject a changed date.

```go
preview, err := client.PreviewSubscriptionTransition(ctx, nozle.SubscriptionTransitionParams{
    CustomerID: "customer-123", SubscriptionID: "subscription-123",
    Operation: "cancel", Timing: "end_of_period",
})
if err != nil { return err }
result, err := client.ApplySubscriptionTransition(ctx, nozle.SubscriptionTransitionParams{
    CustomerID: "customer-123", SubscriptionID: "subscription-123",
    Operation: "cancel", Timing: "end_of_period",
    ExpectedEffectiveAt: preview.SubscriptionTransition.EffectiveAt,
}, "cancel-subscription-123-v1")
if err != nil { return err }
_ = result
```

The live [subscription management API reference](https://docs.nozle.dev/api/subscription-management) describes checkout and transition outcomes. A plan-change preview may be rejected when a target is ineligible or the organization has no transition policy.

## Customers and Entities

Customer upserts go to Core; Core derives the organization from the secret key. Entity reads and mutations use Engine. Mutation keys identify one logical change and should be reused with the same payload after an uncertain response.

```go
customer, err := client.UpsertCustomer(ctx, nozle.Customer{
    ExternalID: "customer-123", Name: "Acme", Email: "billing@example.com",
})
if err != nil { return err }
entity, err := client.UpsertEntity(ctx, customer.ExternalID, "user-42", nozle.EntityInput{
    Name: "Asha", Status: "active", Metadata: map[string]any{"team": "support"},
}, "entity-user-42-v1")
if err != nil { return err }
_ = entity
page, err := client.ListEntities(ctx, customer.ExternalID, nozle.PageOptions{Status: "active", Limit: 50})
if err != nil { return err }
_ = page
```

`GetEntity`, `ActivateEntity`, `SuspendEntity`, and `BulkUpsertEntities` are also available. Entity IDs are limited to 255 UTF-8 bytes. List limits are 1–100; bulk upserts accept 1–500 unique Entity IDs.

Each Entity can have its own subscription. `EnsureEntitySubscription`, `GetEntitySubscription`, and `ListEntitySubscriptions` manage identity and reads. `CheckoutEntitySubscription` and `ChangeEntitySubscriptionPlan` create a payment or plan transition for one Entity. `CheckoutEntitySubscriptions` groups up to 100 Entity plans in one checkout; `CancelEntitySubscription` and `RemoveEntitySubscription` manage its lifecycle.

```go
group, err := client.CheckoutEntitySubscriptions(ctx, "customer-123", []nozle.EntitySubscriptionCheckoutItem{
    {ExternalEntityID: "user-42", PlanCode: "pro_monthly"},
    {ExternalEntityID: "user-43", PlanCode: "max_monthly"},
}, "https://merchant.example/billing/complete", "anniversary", "entity-basket-v1")
if err != nil { return err }
_ = group.Items
```

## Product credits and usage

`ListCreditSystems` follows Core's active pages. Customer and Entity balance and operation methods return exact `Decimal` strings, including `GetCreditBalance`, `ListCreditBalances`, `ListCreditOperations`, `GetEntityCreditBalance`, `ListEntityCreditBalances`, and `ListEntityCreditOperations`.

```go
balance, err := client.GetCreditBalance(ctx, "customer-123", "ai_credits")
if err != nil { return err }
_ = balance.Available // nozle.Decimal, never converted to float64

transfer, err := client.AllocateCredits(ctx, "customer-123", "user-42", "ai_credits",
    nozle.Decimal("100.000000000001"), "allocate-user-42-v1")
if err != nil { return err }
_ = transfer
```

`DeallocateCredits` returns eligible credits to the customer pool. Transfer amounts must be positive decimal strings with at most 12 fractional places; allocation and deallocation require idempotency keys.

`CheckUsage` is advisory. `TrackUsage` atomically evaluates and records usage with a required idempotency key. Use a feature with a configured credit rate and an active customer subscription. A successful HTTP response can still contain `Allowed=false` when there are not enough credits; inspect the result before doing the protected work.

```go
usage := nozle.UsageParams{
    CustomerID: "customer-123", FeatureCode: "agent_execution",
    CreditSystemCode: "ai_credits", Properties: map[string]any{"model": "example-model"},
}
check, err := client.CheckUsage(ctx, usage)
if err != nil { return err }
if !check.Allowed { return fmt.Errorf("insufficient credits") }

tracked, err := client.TrackUsage(ctx, usage, "execution-123")
if err != nil { return err }
if !tracked.Allowed { return fmt.Errorf("usage denied: %s", tracked.Reason) }
```

The legacy wallet adapter `CheckAndDeduct` is separate from product credits and has no idempotency key. Avoid retrying it after an uncertain response. For exact-decimal product credits, use `CheckUsage` and `TrackUsage`.

## Feature Events, Cost Events, and entitlements

`Can` checks an entitlement; `CanWithMetadata` includes a metadata filter. Neither deducts product credits. `Track` sends one Core Feature Event; `Meter` sends an explicit batch of 1–100. Supply a stable transaction ID when coordinating retries or linking Cost Events. If `Track` has no subscription ID, it looks up the customer's single active subscription; an ambiguous customer requires an explicit ID.

```go
transactionID := nozle.CreateTransactionID()
err := client.Track(ctx, nozle.TrackParams{
    CustomerID: "customer-123", SubscriptionID: "subscription-123",
    Event: "copilot_action", TransactionID: transactionID,
    Metadata: map[string]any{"mode": "planning"},
})
if err != nil { return err }

accepted, err := client.TrackCostEvent(ctx, nozle.CostEventParams{
    CostEventID: nozle.CreateCostEventID(), CostMeterCode: "ai_tokens",
    ParentTransactionID: transactionID, RequestID: "provider-request-123",
    OperationKey: "planning", Properties: map[string]any{"tokens": 900, "type": "output"},
})
if err != nil { return err }
_ = accepted.Accepted
```

Cost Event acceptance means ingestion accepted the event; rating and settlement occur later. For a standalone cost, provide `ExternalCustomerID` instead of `ParentTransactionID`. Go accepts RFC3339 or Unix seconds in Feature Event timestamps and sends Unix seconds to Core. Reuse a transaction or Cost Event ID for the same logical event.

## OpenAI and Anthropic usage capture

For automatic capture, wrap the `*http.Client` used by the OpenAI or Anthropic Go SDK once. Continue calling the provider SDK's normal completion and streaming methods; Nozle reads completed usage and emits a Feature Event plus optional linked Cost Events. The core Nozle module has no provider dependency.

```go
providerHTTP, err := nozle.WrapOpenAIHTTPClient(&http.Client{}, client, nozle.ProviderWrapOptions{
    CustomerID: "customer-123", SubscriptionID: "subscription-123",
    FeatureCode: "llm_tokens", CostMeterCode: "ai_tokens",
    OnError: func(err error) { log.Printf("Nozle usage tracking: %v", err) },
})
if err != nil { return err }
openaiClient := openai.NewClient(openaiopt.WithHTTPClient(providerHTTP))
_ = openaiClient
```

Use `WrapAnthropicHTTPClient` for Anthropic. The OpenAI wrapper requests final usage for streams automatically. Provider responses remain available if telemetry fails; `OnError` receives the failure. See the [official-provider integration example](examples/provider-auto/README.md), whose tests exercise both provider SDKs with completed responses and streams.

For applications using other provider clients or requiring durable queued delivery, pass usage metadata explicitly after the provider call:

```go
err := client.CaptureOpenAIUsage(ctx, nozle.ProviderCapture{
    CustomerID: "customer-123", SubscriptionID: "subscription-123",
    FeatureCode: "llm_tokens", CostMeterCode: "ai_tokens",
    Model: "example-model", RequestID: "provider-request-123",
}, nozle.OpenAIUsage{PromptTokens: 150, CachedInputTokens: 20, CompletionTokens: 40})
if err != nil { return err }
```

`CaptureAnthropicUsage` takes `AnthropicUsage` with input, cached input, cache write, and output token counts. OpenAI categories are input, cached input, output, and reasoning. Negative counts are clamped to zero. Manual capture methods return telemetry errors to the caller; if one Cost Event category fails, the others are still attempted. Use a stable transaction ID and your queue when delivery must be durable. The SDK does not retry telemetry automatically.

## Margin reporting

`MarginSummary`, `MarginByCustomer`, `MarginByMetric`, `MarginByPlan`, `MarginByModel`, and `MarginTrend` query Engine. They return `json.RawMessage` because report shapes vary by query and configuration.

```go
summary, err := client.MarginSummary(ctx, nil)
if err != nil { return err }
trend, err := client.MarginTrend(ctx, "week", nil)
if err != nil { return err }
_, _ = summary, trend
```

Pass `url.Values` for optional filters. Trend granularity is `hour`, `day`, `week`, or `month`.

## Errors and retries

HTTP failures return `*nozle.APIError` with `StatusCode`, `Operation`, `Method`, `Path`, and redacted response `Details`/`Body`. Local input failures return `*nozle.ValidationError` or `*nozle.AuthenticationError`; network failures return `*nozle.TransportError`. Context cancellation is available on every call.

```go
var apiErr *nozle.APIError
if errors.As(err, &apiErr) {
    log.Printf("Nozle %s returned HTTP %d", apiErr.Operation, apiErr.StatusCode)
}
```

The SDK makes one HTTP attempt per call and never retries mutations automatically. Reuse the same idempotency key and payload if you deliberately retry after an uncertain response. A new key can create a second operation.

## Development and examples

- [Runnable basic example](examples/basic/README.md): read Engine and Core, then optionally make an advisory credit check using your configured customer and feature.
- Live API references: [subscription management](https://docs.nozle.dev/api/subscription-management), [Entity subscriptions](https://docs.nozle.dev/api/entity-subscriptions), [Events](https://docs.nozle.dev/api/events), and [Cost Events](https://docs.nozle.dev/api/cost-events).

Run `go test ./...`, `go test -race ./...`, and `go vet ./...`. Compile the example with `go build -o /tmp/nozle-basic ./examples/basic`. The standalone repository's GitHub Actions workflow runs the same checks.

## License

Apache License 2.0. See [LICENSE](LICENSE).
