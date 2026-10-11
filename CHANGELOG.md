# Changelog

All notable changes to the nozle-go SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed
- `Track` now posts to the engine's `POST {baseURL}/api/v1/track` (flat body) instead of
  `{eventsURL}/api/v1/events`. The engine resolves the customer's subscription itself, so a
  single base URL serves every call. `WithEventsURL` is deprecated and now defaults to `baseURL`.

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
