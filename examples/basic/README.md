# Basic Go example

This example reads Engine health and plans, then lists active Core credit systems. It sends no mutations. Set a server-side secret key in your shell or secret manager:

```sh
export NOZLE_SECRET_KEY='sk_...'
go run ./examples/basic
```

To use the local stack, set both service URLs:

```sh
export NOZLE_ENGINE_URL='http://localhost:8888/engine'
export NOZLE_CORE_URL='http://localhost:8888/core'
go run ./examples/basic
```

Optionally check a customer and **configured, consumable** feature. This is advisory and does not deduct credits:

```sh
export NOZLE_CUSTOMER_ID='workspace_123'
export NOZLE_FEATURE_CODE='agent_execution'
export NOZLE_CREDIT_SYSTEM_CODE='ai_credits'
go run ./examples/basic
```

Use identifiers configured for your organization. A missing subscription, feature, rate, or balance changes the result. `allowed=false` can be the correct result for a customer with no available credits. The credit check is independent of `TrackUsage`, which records a usage operation and requires a stable idempotency key.

For a local stack when Go is unavailable on the host, run from this repository root:

```sh
docker run --rm --network host \
  -e NOZLE_SECRET_KEY -e NOZLE_ENGINE_URL -e NOZLE_CORE_URL \
  -e NOZLE_CUSTOMER_ID -e NOZLE_FEATURE_CODE -e NOZLE_CREDIT_SYSTEM_CODE \
  -v "$PWD":/src -w /src golang:1.22 go run ./examples/basic
```
