# Nozle Go SDK

Feature Event writes use the Go Engine at `https://api.nozle.app/engine/api/v1/events` (batch: `/events/batch`). Configure `WithBaseURL` for a custom ingestion endpoint. `WithEventsURL` configures Core billing resources and subscription discovery.

Typed server-side Go SDK for Nozle billing, subscriptions, Entities, product credits, usage, Cost Events, margin reporting, and OpenAI/Anthropic usage capture. Requires Go 1.22 or newer.

## Install

```sh
go get github.com/nozle-dev/nozle-go
```

The module path is `github.com/nozle-dev/nozle-go`.

## Authentication and setup

Keep a secret `sk_` key on your backend. A `pk_` key may list plans; every protected customer, billing, event, Entity, credit, usage, and margin operation requires `sk_`. With an empty key, `New` reads the `NOZLE_SECRET_KEY` environment variable and panics when that is empty too. For browser requests, authenticate the user in your application and derive the Nozle customer ID on your backend. Do not trust a customer ID supplied by the browser.

```go
ctx := context.Background()
client := nozle.New(os.Getenv("NOZLE_SECRET_KEY"))
plans, err := client.Plans(ctx)
if err != nil { return err }
_ = plans
```

Import `nozle "github.com/nozle-dev/nozle-go"` and the Go standard library packages used by each snippet. Snippets below assume `ctx` and `client` are defined as above. The [runnable basic example](examples/basic/README.md) reads Engine and Core using environment variables.

The default host is `https://api.nozle.app` (Engine `/engine`, Core `/core`). `WithHost` moves both; `WithBaseURL` and `WithEventsURL` set one each and win over `WithHost`. For a local stack:

```go
client := nozle.New(os.Getenv("NOZLE_SECRET_KEY"),
    nozle.WithHost("http://localhost:8888"),
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

### Currencies

A plan family can be sold in several currencies, one plan per interval and currency. With a secret key, `Plans(ctx)` lists every plan in every currency; `Plans(ctx, nozle.WithCurrency("EUR"))` lists only that currency, and `nozle.WithExternalCustomerID(id)` only the customer's locked currency (ignored when your organization allows several currencies per customer). A publishable key without `WithCurrency` gets each family's default-currency plans. A family plan has `Family` (`Code`, `Currencies`, `DefaultCurrency`) and `Prices` (every variant: `Currency`, `Interval`, `PlanCode`, `AmountCents`).

`CheckoutParams` and `SubscribeParams` take `PlanFamilyCode`, `Interval` and optional `Currency` instead of `PlanCode`; Core picks the variant (without `Currency`: the customer's locked currency, else the family's default). Needs Core 11.10.0.

```go
checkout, err := client.Checkout(ctx, nozle.CheckoutParams{
    CustomerID: "customer-123",
    PlanFamilyCode: "pro",
    Interval: "yearly",
    Currency: "EUR",
    ReturnURL: "https://merchant.example/billing/complete",
})
```

### Payment providers: Stripe and Razorpay

`Checkout` and `CheckoutInvoice` use the payment provider connected to your organization and assigned to the customer. The result's `Type` tells you what to do next. Branch on it; a checkout URL or provider callback alone does not prove fulfillment.

| `Type` | Provider | Next step |
| --- | --- | --- |
| `hosted` | Stripe, Razorpay link | Redirect the customer to `URL` (the Stripe Checkout page; `PaymentURL` is the same address), then confirm as described below (Stripe: `CheckoutID` is the Stripe session id, not a status id) |
| `razorpay` | Razorpay | Open the Razorpay modal with `KeyID`, `OrderID`, `AmountCents` and `Currency`, then verify |
| `processing` | Razorpay or Stripe | Recover with `CheckoutStatus` and `CheckoutID` instead of starting another checkout. Razorpay returns it while the order is not ready; Stripe returns it when a quoted upgrade is replayed while its webhook is still pending |
| `completed`, `scheduled` | Either | No payment is due now: credits covered it (`PaymentSource`), or the change takes effect later (`TransitionDirection`, `TransitionPolicy`, `EffectiveAt`) |

**Stripe.** Every Stripe checkout is the Stripe-hosted Checkout page; there is no embedded form. `CheckoutID` of a Stripe result is the Stripe session id (`cs_...`), which `CheckoutStatus` does not accept. An upgrade session lasts 35 minutes, an Entity batch 23 hours. Redirect the customer to `URL`. Set `CancelURL` (or use `CheckoutInvoiceWithOptions`) to choose where Stripe sends a customer who leaves the page (validated like `ReturnURL`, defaults to it). A return URL may contain the literal `{CHECKOUT_SESSION_ID}`, which Stripe replaces with the Checkout Session id. Credit top-up purchases also return a Stripe-hosted `payment_url` (valid for 24 hours).

```go
checkout, err := client.Checkout(ctx, nozle.CheckoutParams{
    CustomerID:     "customer-123",
    PlanCode:       "pro_monthly",
    ReturnURL:      "https://merchant.example/billing/complete",
    CancelURL:      "https://merchant.example/pricing",
    IdempotencyKey: "customer-123-pro_monthly-v1",
})
if err != nil { return err }
if checkout.Type == "hosted" {
    // Redirect the customer to checkout.URL.
    // Paid upgrade (checkout.PlanUpgradeIntentID): after they return, poll CheckoutStatus
    //   with that ID and the customer and subscription scope; status is awaiting_payment,
    //   processing, succeeded or expired.
    // New subscription or invoice (checkout.InvoiceID): there is no status call. The customer
    //   returns to your return URL; confirm with the invoice.payment_succeeded or subscription
    //   webhook, or by reloading billing state (Can or the subscription read).
}
```

A new-subscription checkout retried while the first attempt is unpaid reopens payment for the same invoice when the plan is the same (or returns `completed` if it was paid meanwhile). For another plan, the unpaid attempt is expired and replaced, or the request gets HTTP 409 `subscription_checkout_in_progress` (`entity_subscription_checkout_in_progress` for an Entity checkout) when it cannot be expired yet. Keep the body identical for one idempotency key: a different request with the same key gets HTTP 409 `idempotency_key_reused`, including an invoice checkout. Checkout error bodies carry `status`, `error` and `code`.

**Razorpay.** Razorpay supports only INR, and `AmountCents` is in paise. Return the `razorpay` result to the browser, open the modal there, and post the handler's three fields back to your backend. `VerifyCheckout` sends them to Nozle, which checks the signature and fetches the payment from Razorpay; the SDK does not check the signature locally. `CheckoutResult` also decodes `ExpiresAt`, and for a UPI AutoPay mandate (`RegisterMandate: &yes` in `CheckoutParams`) `CustomerID`, `Recurring` and `MandateMaxAmountCents`. Verify and status need an `sk_` key.

```go
// Backend: create.
yes := true
checkout, err := client.Checkout(ctx, nozle.CheckoutParams{
    CustomerID:      "customer-123",
    PlanCode:        "pro_monthly",
    ReturnURL:       "https://merchant.example/billing/complete",
    IdempotencyKey:  "customer-123-pro_monthly-v1",
    RegisterMandate: &yes, // optional: UPI AutoPay
})
if err != nil { return err }
if checkout.Type == "razorpay" {
    // Browser, after loading https://checkout.razorpay.com/v1/checkout.js:
    //   new Razorpay({key: KeyID, order_id: OrderID, amount: AmountCents, currency: Currency, handler}).open()
    // For a mandate also pass customer_id: CustomerID and recurring: "1".
}

// Backend: verify what the handler returned, then poll until both statuses succeed.
scope := nozle.CheckoutScope{CustomerID: "customer-123", SubscriptionID: "subscription-123"}
_, err = client.VerifyCheckout(ctx, checkout.CheckoutID, nozle.RazorpayVerification{
    RazorpayOrderID:   orderID,
    RazorpayPaymentID: paymentID,
    RazorpaySignature: signature,
}, scope)
if err != nil { return err }
status, err := client.CheckoutStatus(ctx, checkout.CheckoutID, scope)
if err != nil { return err }
// status.Status: awaiting_payment | processing | succeeded | failed | expired | needs_review
done := status.Status == "succeeded" && status.FulfillmentStatus == "succeeded"
_ = done
```

Bind each `CheckoutID` to the buyer who started it before you call verify or status. Pass both scope fields to tie a call to one customer and subscription; a status call can also run without a scope, with an `sk_` key. `CheckoutInvoice` starts collection for an existing invoice and returns the same result types.

Provider failures are `*nozle.APIError`. `Code()` is the top-level code, and `HasCode` also matches the codes listed in `error_details`, for example `razorpay_requires_inr`, `payment_provider_required` or `invalid_checkout_signature`.

```go
_, err := client.Checkout(ctx, nozle.CheckoutParams{CustomerID: "customer-123", PlanCode: "pro_monthly"})
var apiErr *nozle.APIError
if nozle.AsAPIError(err, &apiErr) && apiErr.HasCode("razorpay_requires_inr") {
    // Price the plan in INR for customers who pay through Razorpay.
}
```

`ManagePaymentMethod(ctx, customerID, returnURL, key)` is Stripe only: it returns a `ManagePaymentMethodResult` whose `URL` is the Stripe billing page where the customer adds or replaces a saved card. Razorpay customers pay each invoice by payment link, so for them it returns a 422 `*nozle.APIError` where `HasCode("razorpay_pays_by_link")` is true.

See the [Stripe](https://docs.nozle.dev/integrations/stripe) and [Razorpay](https://docs.nozle.dev/integrations/razorpay) integration guides for connection setup, webhooks, and limits.

`Subscribe` creates a backend subscription. `CancelSubscription` takes the timing (`end_of_period` or `immediate`); leave it empty to use the organization's cancellation timing. An optional `nozle.CancelSubscriptionOptions{CreditAction, RefundMode, FinalInvoiceAction}` sets how the cancellation is settled; each field left empty follows the subscription, then the organization's cancellation settings.

```go
subscription, err := client.Subscribe(ctx, nozle.SubscribeParams{CustomerID: "customer-123", PlanCode: "free"})
if err != nil { return err }
cancel, err := client.CancelSubscription(ctx, "customer-123", subscription.SubscriptionID, "end_of_period")
if err != nil { return err }
_ = cancel
```

### Plan versions

When you change the price of a plan that customers pay for, Nozle saves it as the plan's next version: new customers get the new version, and existing subscribers keep the price they bought until you move them. `Plans` returns current versions only; each `Plan` has `Version` and `IsCurrent`.

```go
// Keep selling the older price to one customer.
_, err := client.Subscribe(ctx, nozle.SubscribeParams{CustomerID: "cus_123", PlanCode: "pro", PlanVersion: 1})

versions, err := client.PlanVersions(ctx, "pro") // every version, its price and active subscriptions
older, err := client.PlanVersion(ctx, "pro", 1)

// Move version 1's subscribers to the current version, each at the end of its billing period.
preview, err := client.PreviewPlanVersionMigration(ctx, "pro", nozle.PlanVersionMigrationParams{FromVersion: 1})
migration, err := client.CreatePlanVersionMigration(ctx, "pro", nozle.PlanVersionMigrationParams{FromVersion: 1})
migration, err = client.GetPlanVersionMigration(ctx, "pro", migration.NozleID) // per-subscription Items
_, err = client.CancelPlanVersionMigration(ctx, "pro", migration.NozleID)       // unschedules moves not started
```

A migration invoices, credits and refunds nothing when it runs: each subscription is billed the new version from its next period. Subscriptions it cannot move (custom pricing, a trial the new version would change, a pending plan change, an open checkout, and so on) are skipped with a `SkipReason`. Sending the same plan code again for an existing subscription keeps its version; another `PlanVersion` for it is refused. These calls need a secret key. See [Plan versioning](https://docs.nozle.dev/guides/billing/plan-versions).

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

To link a customer that already exists in Stripe without creating a new one, pass `BillingConfiguration` with the provider customer ID. A provider assignment needs `ProviderCustomerID` or `SyncWithProvider: true`; otherwise Core drops it.

```go
_, err = client.UpsertCustomer(ctx, nozle.Customer{
    ExternalID: "customer-123",
    BillingConfiguration: &nozle.CustomerBillingConfiguration{
        PaymentProvider: "stripe", PaymentProviderCode: "stripe_main", ProviderCustomerID: "cus_ABC123",
    },
})
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

### Importing billing state and opening credit balances

When you move customers from another billing system, import each subscription that system still bills (`ImportBillingState`, Core), then its current credit balance (`ImportCreditBalance`, Engine). Both need a secret key and an idempotency key (at most 255 bytes): retry an uncertain call with the same key and params. `DryRun: true` validates without writing. Failures return `*APIError`.

```go
state, err := client.ImportBillingState(ctx, "workspace_123", nozle.BillingStateImportParams{
    Subscription: nozle.BillingStateImportSubscription{
        PlanCode: "pro_monthly", Status: "active", BillingAuthority: "external",
        ExternalReference:  "merchant_subscription_123",
        CurrentPeriodStart: "2026-10-15T00:00:00Z", CurrentPeriodEnd: "2026-11-15T00:00:00Z",
    },
}, "import-workspace_123-state")
if err != nil { return err }
_, err = client.ImportCreditBalance(ctx, "workspace_123", nozle.CreditBalanceImportParams{
    ExternalSubscriptionID: state.Subscription.ExternalSubscriptionID,
    CreditSystemCode: "ai_credits", GrantedAmount: "500", RemainingAmount: "320",
}, "import-workspace_123-credits")
if err != nil { return err }
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

The legacy wallet adapter `CheckAndDeduct` takes `CustomerID` and `Credits`; `Feature` is deprecated and ignored. It is separate from product credits and has no idempotency key. Avoid retrying it after an uncertain response. For exact-decimal product credits, use `CheckUsage` and `TrackUsage`.

## Feature Events, Cost Events, and entitlements

`Can` checks an entitlement. A customer with several subscriptions needs `CanWithOptions(ctx, customerID, feature, nozle.CanOptions{SubscriptionID: ..., EntityID: ...})` (external IDs); without a selector the result is `Allowed=false`, `Reason="subscription_required"`. `CanWithMetadata` also sends the call's properties, which Engine uses to estimate its cost and revenue (`economics`) and to apply a margin policy; they do not filter usage or change the plan limit check. Neither deducts product credits. `Track` sends one Feature Event through Engine; `Meter` sends an explicit batch of 1–100. Supply a stable transaction ID when coordinating retries or linking Cost Events. `Meter` writes the ID it generates for an event without one back into your slice, so retrying the same slice after an error or timeout sends the same IDs and Engine records each event once; build a new slice only for new events. `Track` does not return its generated ID; `TrackWithID` does. `MeterWithResult` returns the accepted events; the batch is all-or-nothing, so a rejected one is a 422 `*APIError` whose `BatchErrors()` maps each failing event index to its errors. `APIError.Error()` includes the Engine error code, also available from `Code()`. `AmountDueCents` on a checkout result is `""` when omitted and `"0"` when zero, so keep it (or pass your own `TransactionID`) when you may retry. If `Track` has no subscription ID, it looks up the customer's single active subscription; a customer with none or several returns a 422 `*APIError` with code `active_subscription_not_found` or `active_subscription_ambiguous`, so pass an explicit ID.

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

Cost Event acceptance means ingestion accepted the event; rating and settlement occur later. For a standalone cost, provide `ExternalCustomerID` instead of `ParentTransactionID`. Go accepts RFC3339 or Unix seconds in Feature Event timestamps and sends Unix seconds to Engine. Reuse a transaction or Cost Event ID for the same logical event.

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

`MarginForecast` (typed `MarginForecastResult`) projects the 30 days after the range, and `MarginSimulate` replays one metric's usage at a new unit price. `CostIntelligenceReport`, `PreviewSharedCostAllocation` and `AllocateSharedCost` read the ledger-backed cost report and book Shared Period Costs.

```go
forecast, err := client.MarginForecast(ctx, nil)
if err != nil { return err }
sim, err := client.MarginSimulate(ctx, nozle.MarginSimulateParams{MetricCode: "agent_execution", NewPriceDollars: 1}, nil)
if err != nil { return err }
report, err := client.CostIntelligenceReport(ctx, nozle.CostIntelligenceReportParams{From: "2026-10-01T00:00:00Z", Limit: 50})
if err != nil { return err }
_, _, _ = forecast, sim, report
```

## Errors and retries

HTTP failures return `*nozle.APIError` with `StatusCode`, `Operation`, `Method`, `Path`, and redacted response `Details`/`Body`. Local input failures return `*nozle.ValidationError` or `*nozle.AuthenticationError`; network failures return `*nozle.TransportError`. Context cancellation is available on every call.

```go
var apiErr *nozle.APIError
if errors.As(err, &apiErr) {
    log.Printf("Nozle %s returned HTTP %d", apiErr.Operation, apiErr.StatusCode)
}
```

By default the SDK makes one HTTP attempt per call. `WithMaxRetries(n)` (with `WithRetryDelay`) opts in to retrying network errors, timeouts, 429 and 5xx, honoring `Retry-After`, but only for a call that is safe to repeat: a read, a call with an idempotency key, or an idempotent-by-id call such as `Track` and `Meter`. Other mutations are never retried. Reuse the same idempotency key and payload if you deliberately retry after an uncertain response. A new key can create a second operation.

### Workspace suspended or card required (402)

When your Nozle workspace is suspended because its Nozle invoices are unpaid, every API call answers HTTP 402 with the code `workspace_suspended`. A workspace without a verified card answers 402 with the code `card_required` the same way. It comes back as a `*nozle.APIError` with `StatusCode` 402 and `Code()` `workspace_suspended` from `Can`, `Track`, `CheckUsage` and every other call; `Can` returns the error instead of an allowed or denied result. Never retry blindly: the SDK does not retry a 402. Add a card in the Nozle dashboard (`card_required`) or pay the Nozle invoices (`workspace_suspended`) and calls work again at once. Events sent during the suspension are refused, not queued.

```go
if errors.As(err, &apiErr) && apiErr.Code() == "workspace_suspended" {
    // Tell your team to pay the Nozle invoices; not allowed, not denied.
}
```

## Development and examples

- [Runnable basic example](examples/basic/README.md): read Engine and Core, then optionally make an advisory credit check using your configured customer and feature.
- Live API references: [subscription management](https://docs.nozle.dev/api/subscription-management), [Entity subscriptions](https://docs.nozle.dev/api/entity-subscriptions), [Events](https://docs.nozle.dev/api/events), and [Cost Events](https://docs.nozle.dev/api/cost-events).

Run `go test ./...`, `go test -race ./...`, and `go vet ./...`. Compile the example with `go build -o /tmp/nozle-basic ./examples/basic`. The standalone repository's GitHub Actions workflow runs the same checks.

## License

Apache License 2.0. See [LICENSE](LICENSE).

## Legacy methods removed

`Consume` and `LLM` were removed in v0.9.1. Their server routes (`/api/v1/consume`, `/api/v1/llm`) do not exist on the managed production service, so every call returned HTTP 404.

Use `Can` and `Track` for separate entitlement checks and event submission. Those two calls are not an atomic quota reservation. Use `CheckUsage` / `TrackUsage` for the product-credit workflow. For provider usage, use `TrackCostEvent`, `CaptureOpenAIUsage`, or `CaptureAnthropicUsage` with a configured cost meter. Those APIs record usage asynchronously; they do not provide the old synchronous token-price lookup.

Subscription credit grants are materialized asynchronously. Wait for the expected balance before allowing credit-backed activity after subscribing; an active subscription alone does not prove its grant is ready. Entity credit allocation/deallocation requires that feature to be enabled for the organization and credit system.
