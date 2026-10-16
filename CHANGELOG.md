# Changelog

All notable changes to the nozle-go SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- `ListInvoicesParams.PaymentOverdue` keeps only unpaid invoices past their due date.
- `ListAlerts`, `GetAlert`, `CreateAlert`, `UpdateAlert` and `DeleteAlert` manage subscription usage alerts and wallet balance alerts (`AlertTarget`).
- `ListInvoices`, `GetInvoice` and `InvoiceDownloadURL` (the PDF URL, or `""` while it is generated).
- `CurrentUsage`, `PastUsage` and `ProjectedUsage` read Core usage (`CustomerUsage`, `CustomerProjectedUsage`); an empty subscription ID uses the customer's one active subscription.
- `Client.VerifyWebhook(ctx, body, header, hmacKey)` checks `X-Nozle-Signature` (`jwt`: Nozle's public key, read once with the secret key and cached; `hmac`: the signing secret) and returns a `WebhookEvent`, or a `*WebhookVerificationError` with a `Code`. `Client.WebhookHandler(hmacKey, onEvent)` is an `http.Handler` that answers 200, 400 for a forged request, or 500 when `onEvent` fails.
- `New` reads the `NOZLE_SECRET_KEY` environment variable when the key argument is blank. It still panics when both are blank. An explicit key is used as before.
- `WithHost` option (default `https://api.nozle.app`). It fills in the Engine URL (`host/engine`) and Core URL (`host/core`) when `WithBaseURL` and `WithEventsURL` are not given. Those two keep their meaning and win over `WithHost`.
- Opt-in retries: `WithMaxRetries(n)` (default 0, no retries) and `WithRetryDelay(d)` (default 500ms, doubling up to 10 seconds, or the `Retry-After` header when sent). Network errors, timeouts, `429` and `5xx` are retried, only for calls that are safe to repeat: reads, calls with an idempotency key (such as `TrackUsage`), `Track`, `Meter`, `TrackCostEvent`, ``UpsertCustomer`, and the read-only `CheckUsage` and transition previews. `Subscribe`, `Checkout` and other writes without a key are never retried. `WithTimeout` applies to each attempt.
- `APIError.RequestID` (the `X-Request-Id` response header, which Core sends and the Engine does not yet) and `APIError.RetryAfter` (a `time.Duration` from `Retry-After`, as seconds or an HTTP date). Both are empty or zero when the service did not send them. The message is unchanged.
- A contract test (`openapi_contract_test.go`) checks that response structs declare only JSON keys the API schema has. `scripts/sync_openapi_contract.py` refreshes `testdata/openapi_schemas.json` from `nozle-openapi.yaml`.
- A request contract test (`request_contract_test.go`) checks the body of SDK calls against the API request schema: no unknown key, no `null` where it is not allowed, enum values only. `scripts/sync_openapi_requests.py` refreshes `testdata/openapi_requests.json` from `nozle-openapi.yaml`.
- `WrapHTTPClient` reports once per event code, through `OnError`, a `*SetupError` with `Code == "feature_not_found"` when no Feature has the event code: the usage is stored but not billed. Recording is not blocked.
- `WrapHTTPClient(providerClient, nozleClient, CaptureOptions{...})` returns an `*http.Client` that records the token usage of every successful LLM response it carries, for any LLM SDK that takes an HTTP client or for your own requests. Usage is recognised by the response shape: OpenAI-compatible `usage` (any OpenAI-compatible API, including the Responses API), Anthropic `usage` and Gemini `usageMetadata`, for JSON responses and streams. Chat completion streams are asked for usage (`stream_options.include_usage`; `SkipStreamUsage` turns it off). The customer comes from the `X-Nozle-Customer-Id` request header (not sent to the provider), `WithCustomer(ctx, id)` on the request context, `CustomerIDFunc`, or `CustomerID`. Recording never changes or fails the provider response; failures go to `OnError`.
- `RecordUsage(ctx, ProviderCapture, Usage)` records usage your code read itself.

### Breaking

- Removed struct fields the API has never sent, which were always zero: `PingResult.OrgID`, `KeyType`, `Env` and `Version`, and `CanResult.CostPerUseCents`, `RevenuePerUseCents`, `MarginPerUseCents` and `MinMarginPercent` (use `CanResult.Economics`). `CostEventResult.Accepted` stays, derived from `Status` and no longer tagged as a JSON field.

### Changed

- Usage events recorded by `WrapOpenAIHTTPClient`, `WrapAnthropicHTTPClient`, `CaptureOpenAIUsage`, `CaptureAnthropicUsage` and the new capture use the provider's response id as the transaction id (`llm-<format>-<id>`) when there is one and no `TransactionID` is set, so the same response is never recorded twice. `ProviderCapture.ProviderHost` is sent as `provider_host`.

### Fixed

- `TrackUsage` reuses the timestamp it filled in when the same idempotency key is sent again on the same client, so a retry no longer fails with 409 `idempotency_key_reused`. The client remembers the last 1000 keys; a retry from another process or after a restart must pass `Timestamp`.
- Every request leaves out fields that are `null` or an empty string, at any depth, so an optional field you did not set is not sent and the API applies its default. Event `properties` and `metadata` are your own data and are sent unchanged.
- `CheckoutEntitySubscription`, `ChangeEntitySubscriptionPlan` and `CheckoutEntitySubscriptions` leave out `return_url` and `billing_time` when they are empty instead of sending `""`, which is not a valid `billing_time`. `ChangeEntitySubscriptionPlan` now sends `BillingTime` when set; it used to drop it. `CheckoutEntitySubscriptions` rejects a `billingTime` other than `calendar` or `anniversary` before sending.

## [0.11.0] - 2026-10-14

### Added
- `Customer.BillingConfiguration` (`CustomerBillingConfiguration`) is sent as Core's `billing_configuration` by ``UpsertCustomer`, so a customer can be linked to an existing Stripe customer in the same call. It is decoded from responses too. Upserts without it send the same body as before.

## [0.10.0] - 2026-10-13

### Changed
- `Track`, `Meter` and provider wrappers submit Feature Events to the Go Engine endpoints `/api/v1/events` and `/api/v1/events/batch`. Custom event-ingestion deployments now use `WithBaseURL`; `WithEventsURL` continues to configure Core billing resources and subscription discovery. Payloads, authentication and transaction IDs are unchanged.

### Breaking
- **`CanResult.Limit` and `CanResult.Remaining` are `*int`.** The API sends `null` for both on an unlimited feature (docs: `can()` response), and a plain `int` decoded that as `0`, which reads as "no allowance left". They are `nil` when the feature is unlimited and a pointer to the value otherwise. Node.js and Python already report `null`. Code that reads `res.Limit` or `res.Remaining` as an `int` no longer compiles: check `res.Limit != nil` first.

## [0.9.1] - 2026-10-13

### Breaking
- **Credit systems read `nozle_id`.** `ListCreditSystems` now takes each credit system's `ID` from the `nozle_id` key and no longer falls back to `lago_id`. Against a server that still sends only the old key, `ID` is empty. Ship this release together with the Nozle Core release that sends `nozle_id`.
- **`Consume()` and `LLM()` are removed** (see Removed). Code that calls them no longer compiles.
- **`CheckoutResult.URL`, `PublishableKey`, `StripeAccount` and `PendingSubscriptionID` are removed.** Core and the Engine never return them from checkout, so they were always empty. The pending subscription ID is `SubscriptionID`, and a hosted Razorpay payment link is `PaymentURL`; use your own Stripe publishable key. Code that reads the removed fields no longer compiles.

### Added
- `CheckoutResult` now decodes `expires_at`, `customer_id`, `recurring`, `mandate_max_amount_cents` and `plan_upgrade_intent_id` (Razorpay orders, payment links and mandates, paid upgrades), and `plan_code`, `payment_source`, `transition_direction` and `transition_policy` (completed and scheduled results).
- `APIError.Code()` and `APIError.HasCode()` expose the API error code, including the codes listed in `error_details` such as `razorpay_requires_inr`, `payment_provider_required` and `invalid_checkout_signature`.
- README section covering Stripe and Razorpay checkout side by side, including `processing` results for Stripe and Razorpay, Stripe-hosted credit top-up links, and idempotency edge cases.

### Changed
- `VerifyCheckout` rejects a missing `razorpay_order_id`, `razorpay_payment_id` or `razorpay_signature` before sending a request (it already rejected a missing signature).

### Deprecated
- `CheckAndDeductParams.Feature` is ignored: it is no longer required, validated or sent, because the endpoint never used it. Existing code that sets it keeps working.

### Removed
- **Breaking:** `Consume()` and `LLM()` with `ConsumeParams`, `ConsumeResult`, `ConsumeUsage`, `LLMParams` and `LLMResult`. Their routes (`/api/v1/consume`, `/api/v1/llm`) are absent from the managed service and always returned HTTP 404. Use `CheckUsage`/`TrackUsage`, `Can`/`Track`, or the cost-event helpers instead.

## [0.9.0] - 2026-10-11

### Changed
- Added typed checkout, subscription, customer, Entity, credits, usage, Cost Event, margin, and provider capture workflows aligned with the current Node and Python SDKs.
- `Track` and `Meter` use Core Feature Event routes; Engine and Core URL overrides are independent.
- Added UUIDv7 transaction and Cost Event ID helpers and structured API errors.
- Added automatic OpenAI chat-completion and Anthropic message usage capture for completed calls and streams through wrapped provider HTTP clients. The core module remains provider-dependency-free.

### Fixed
- Preserved `amount_due_at_effective_cents` and `renewal_at` in signed subscription-change previews.

## [0.2.0] - 2025-06-20

### Added
- Atomic `Consume()` check-and-meter method

### Fixed
- Align SDK routes and checkout payload with engine `/api/v1/` prefix

## [0.1.0] - 2025-05-10

### Added
- Initial Go SDK release with full API parity (JS/Python SDKs), Apache 2.0 license
- `Can()` entitlement check
- `Meter()` / `Track()` batch usage events
- `LLM()` cost computation
- `Ping()` connectivity check
- `ListPlans()` plan listing
- `CreateSubscription()` and `CancelSubscription()` billing methods
- Cloud Build CI pipeline
