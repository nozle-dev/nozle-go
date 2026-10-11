# nozle-go

Go client for the [Nozle](https://nozle.dev) billing and entitlements API.

## Installation

```bash
go get github.com/nozle-dev/nozle-go
```

Requires Go 1.22 or later.

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    nozle "github.com/nozle-dev/nozle-go"
)

func main() {
    client := nozle.New("your-api-key",
        nozle.WithBaseURL("https://engine.nozle.dev"),
    )

    result, err := client.Can(context.Background(), "customer-123", "api_calls")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Allowed: %v (used %d of %d)\n", result.Allowed, result.Used, result.Limit)
}
```

## Creating a Client

```go
client := nozle.New(apiKey, opts...)
```

`New` panics if `apiKey` is empty.

### Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithBaseURL(url)` | `http://localhost:8080` | Engine API base URL |
| `WithEventsURL(url)` | `http://localhost:3000` | Events / Lago base URL |
| `WithTimeout(d)` | `15s` | HTTP request timeout |
| `WithHTTPClient(hc)` | default `http.Client` | Custom `*http.Client` |
| `WithDebug(true)` | `false` | Log outgoing requests |

## API Reference

All methods accept a `context.Context` as the first argument.

### Ping

Verify connectivity and API key validity.

```go
result, err := client.Ping(ctx)
// result.OK      bool
// result.OrgID   string
// result.KeyType string
// result.Env     string
```

### Can

Check whether a customer is entitled to use a feature.

```go
result, err := client.Can(ctx, "customer-123", "api_calls")
// result.Allowed            bool
// result.Reason             string
// result.Used               int
// result.Limit              int
// result.Remaining          int
// result.CostPerUseCents    float64
// result.RevenuePerUseCents float64
// result.MarginPerUseCents  float64
// result.MinMarginPercent   float64
```

### Consume

Atomically check entitlements and record usage in a single request.
If the entitlement check fails, no usage is recorded.

```go
result, err := client.Consume(ctx, nozle.ConsumeParams{
    CustomerID: "customer-123",
    Feature:    "api_calls",
    Units:      3,
    Metadata:   map[string]string{"model": "gpt-4"},
})
// result.Allowed        bool
// result.Reason         string
// result.Usage.Recorded bool
// result.Usage.EventID  string
```

### Track

Send a single usage event to the events API.

```go
err := client.Track(ctx, nozle.TrackParams{
    CustomerID:     "customer-123",
    Event:          "api_call",
    SubscriptionID: "sub-1",          // optional
    TransactionID:  "tx-abc",         // optional, auto-generated if empty
    Timestamp:      "2024-01-15T...", // optional
    Metadata: map[string]interface{}{
        "endpoint": "/users",
    },
})
```

### Meter

Send a batch of usage events (1--1000).

```go
err := client.Meter(ctx, []nozle.MeterEvent{
    {CustomerID: "customer-123", Event: "api_call", TransactionID: "tx-1"},
    {CustomerID: "customer-456", Event: "api_call", TransactionID: "tx-2"},
})
```

### LLM

Look up the cost of an LLM invocation.

```go
result, err := client.LLM(ctx, nozle.LLMParams{
    Model:        "gpt-4o",
    InputTokens:  1000,
    OutputTokens: 500,
    CustomerID:   "customer-123",
})
// result.CostCents       float64
// result.InputCostCents  float64
// result.OutputCostCents float64
// result.Model           string
```

### Plans

List all plans visible to the API key.

```go
plans, err := client.Plans(ctx)
for _, p := range plans {
    fmt.Printf("%s: %d %s/%s\n", p.Name, p.AmountCents, p.AmountCurrency, p.Interval)
}
```

### Checkout

Create a checkout session for a customer/plan combination.

```go
result, err := client.Checkout(ctx, nozle.CheckoutParams{
    OrganizationID: "org-123",
    PlanCode:       "starter",
    ReturnURL:      "https://example.com/success", // optional
})
// result.ClientSecret string
// result.InvoiceID    string
// result.AmountCents  int
// result.Currency     string
```

### Subscribe

Subscribe a customer to a plan directly (no checkout flow).

```go
result, err := client.Subscribe(ctx, nozle.SubscribeParams{
    CustomerID: "customer-123",
    PlanCode:   "starter",
})
// result.SubscriptionID string
// result.Status         string
```

## Error Handling

All methods return an `error` on HTTP errors (status >= 400). Use `AsAPIError` to
inspect the response details:

```go
result, err := client.Can(ctx, "customer-123", "api_calls")
if err != nil {
    var apiErr *nozle.APIError
    if nozle.AsAPIError(err, &apiErr) {
        fmt.Printf("HTTP %d: %s %s\n", apiErr.StatusCode, apiErr.Method, apiErr.Path)
        fmt.Printf("Body: %s\n", apiErr.Body)
    }
    // handle error
}
```

Common status codes:

| Code | Meaning |
|------|---------|
| 401 | Invalid or missing API key |
| 403 | Forbidden -- key lacks permission |
| 404 | Unknown endpoint or resource |
| 429 | Rate limited |
| 500 | Server error |

## License

Apache 2.0 -- see [LICENSE](LICENSE) for details.
