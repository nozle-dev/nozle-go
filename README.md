# Nozle Go SDK

Go 1.22+ server SDK for Nozle Engine and Core. Apache 2.0 licensed.

```bash
go get github.com/nozle-dev/nozle-go
```

```go
package main
import (
 "context"
 "log"
 nozle "github.com/nozle-dev/nozle-go"
)
func main() {
 c := nozle.New("sk_your_secret_key")
 result, err := c.CheckUsage(context.Background(), nozle.UsageParams{CustomerID:"workspace_123", FeatureCode:"agent_execution", CreditSystemCode:"ai_credits"})
 if err != nil { log.Fatal(err) }
 log.Println(result.Allowed, result.CreditsRequired)
}
```

Defaults: Engine `https://api.nozle.app/engine`, Core `https://api.nozle.app/core`, 10-second timeout. For local deployments, use `WithBaseURL` and `WithEventsURL`; `WithHTTPClient` injects a custom HTTP client. Every call accepts a context. The SDK makes one HTTP attempt per call and never retries mutations automatically.

## Workflows

- Billing: `Plans`, `Checkout`, `CheckoutInvoice`, `CheckoutStatus`, `VerifyCheckout`, `Subscribe`, `CancelSubscription`, `SubscriptionOptions`, `PreviewSubscriptionChange`, `PreviewSubscriptionTransition`, `ApplySubscriptionTransition`, `WithdrawPendingSubscriptionChange`.
- Metering: `Track`, `Meter`, `Can`, `CanWithMetadata`, `Consume`, `TrackCostEvent`.
- Customers and Entities: `UpsertCustomer`, `GetEntity`, `ListEntities`, `UpsertEntity`, `ActivateEntity`, `SuspendEntity`, `BulkUpsertEntities`, and Entity subscription methods.
- Credits: `ListCreditSystems`, customer and Entity balance and operation reads, `AllocateCredits`, `DeallocateCredits`, `CheckUsage`, `TrackUsage`.
- Margin: summary, customer, metric, plan, model, and trend queries.
- LLM: `CaptureOpenAIUsage` and `CaptureAnthropicUsage` record provider usage metadata and optional categorized Cost Events after a provider response. The SDK does not depend on a specific provider client library.

Use a secret `sk_` key on your server. Reuse the same idempotency key and payload after an uncertain mutation. Product-credit amounts use `Decimal` strings. Failed HTTP responses return `*APIError` with status, operation, and redacted details; validation and transport errors have separate types.

See the [Go SDK documentation](https://docs.nozle.app/sdks/go/installation) for examples. Public `go get` becomes available when this standalone repository is published.
