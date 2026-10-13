# Changelog

All notable changes to the nozle-go SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Removed
- **Breaking:** `CheckoutResult.URL`, `PublishableKey`, `StripeAccount` and `PendingSubscriptionID`. Core and the Engine never return them from checkout (the pending subscription ID is `SubscriptionID`; use your own Stripe publishable key). Code that reads these fields no longer compiles; they were always empty.

## [0.9.1] - 2026-10-13

### Breaking
- **Credit systems read `nozle_id`.** `ListCreditSystems` now takes each credit system's `ID` from the `nozle_id` key and no longer falls back to `lago_id`. Against a server that still sends only the old key, `ID` is empty. Ship this release together with the Nozle Core release that sends `nozle_id`.
- **`Consume()` and `LLM()` are removed** (see Removed). Code that calls them no longer compiles.

### Added
- `CheckoutResult` now decodes `expires_at`, `customer_id`, `recurring`, `mandate_max_amount_cents` and `plan_upgrade_intent_id` (Razorpay orders, payment links and mandates, paid upgrades), and `plan_code`, `payment_source`, `transition_direction` and `transition_policy` (completed and scheduled results).
- `APIError.Code()` and `APIError.HasCode()` expose the API error code, including the codes listed in `error_details` such as `razorpay_requires_inr`, `payment_provider_required` and `invalid_checkout_signature`.
- README section covering Stripe and Razorpay checkout side by side, including `processing` results for Stripe and Razorpay, Stripe-hosted credit top-up links, and idempotency edge cases.

### Changed
- `VerifyCheckout` rejects a missing `razorpay_order_id`, `razorpay_payment_id` or `razorpay_signature` before sending a request (it already rejected a missing signature).

### Deprecated
- `CheckAndDeductParams.Feature` is ignored: it is no longer required, validated or sent, because the endpoint never used it. Existing code that sets it keeps working.
- `CheckoutResult.PendingSubscriptionID` is never returned; use `SubscriptionID`. `URL`, `PublishableKey` and `StripeAccount` are not returned either; use your own Stripe publishable key.

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
