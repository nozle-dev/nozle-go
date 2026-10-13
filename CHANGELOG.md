# Changelog

All notable changes to the nozle-go SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.9.1] - 2026-10-13

### Removed
- `Consume()` and `LLM()` with `ConsumeParams`, `ConsumeResult`, `ConsumeUsage`, `LLMParams` and `LLMResult`. Their routes (`/api/v1/consume`, `/api/v1/llm`) are absent from the managed service and always returned HTTP 404. Use `CheckUsage`/`TrackUsage`, `Can`/`Track`, or the cost-event helpers instead.

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
