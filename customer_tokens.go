package nozle

import "context"

// CustomerTokensService is Client.CustomerTokens.
type CustomerTokensService struct{ c *Client }

// CustomerToken is what CustomerTokens.Create returns. Hand it to the browser.
type CustomerToken struct {
	Token      string `json:"token"`       // send as X-Nozle-Customer-Token with your publishable key; a bearer credential, never log it
	CustomerID string `json:"customer_id"` // the customer's external id
	ExpiresAt  string `json:"expires_at"`  // RFC 3339
}

// CustomerTokenOptions are the optional parts of CustomerTokens.Create.
type CustomerTokenOptions struct {
	// TTLSeconds is the lifetime, 60 to 3600; zero means the default of one hour. Core answers 422 outside the range.
	TTLSeconds int
	// Features are feature codes: can and usage reads are limited to them. Subscription and credit reads are not.
	Features []string
}

// CustomerTokenRevocation is what CustomerTokens.Revoke returns.
type CustomerTokenRevocation struct {
	RevokedAt string `json:"revoked_at"` // RFC 3339
}

// Create mints a short-lived, read-only token that lets the signed-in customer's browser read that customer's
// entitlements, usage, credit balances and subscription with your publishable key (POST /customer_tokens, Core
// 11.59.0). Take customerID, the customer's external id, from your own session, never from the browser. Secret key
// with customer:read. A refusal is an *APIError: 403 publishable_key_not_allowed, 404 for an unknown customer, 422
// for a TTL outside 60 to 3600 or an unknown feature. A repeat only mints another token, so it is retried like a read.
func (s *CustomerTokensService) Create(ctx context.Context, customerID string, opts CustomerTokenOptions) (*CustomerToken, error) {
	const op = "customer_tokens.create"
	if err := validateKey(s.c.apiKey, op); err != nil {
		return nil, err
	}
	if err := required(op, "customer_id", customerID); err != nil {
		return nil, err
	}
	body := map[string]any{"customer_id": customerID}
	if opts.TTLSeconds != 0 {
		body["ttl_seconds"] = opts.TTLSeconds
	}
	if opts.Features != nil {
		body["features"] = opts.Features
	}
	var out CustomerToken
	if err := s.c.request(ctx, op, "POST", true, "/api/v1/customer_tokens", body, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// Revoke refuses every customer token minted until now, for example after one leaked. Tokens minted afterwards work
// (POST /customer_tokens/revoke, Core 11.59.0). Secret key with customer:write.
func (s *CustomerTokensService) Revoke(ctx context.Context) (*CustomerTokenRevocation, error) {
	const op = "customer_tokens.revoke"
	if err := validateKey(s.c.apiKey, op); err != nil {
		return nil, err
	}
	var out CustomerTokenRevocation
	if err := s.c.request(ctx, op, "POST", true, "/api/v1/customer_tokens/revoke", map[string]any{}, &out, ""); err != nil {
		return nil, err
	}
	return &out, nil
}
