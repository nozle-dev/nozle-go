package nozle

import (
	"context"
	"time"
)

// Codes of a refused trial update: error_details["trial"] of a 422, except CodePlanChangePending
// (a 409 code). Test them with APIError.HasCode.
const (
	CodeTrialInvalidParams    = "trial_invalid_params"
	CodeTrialNotActive        = "trial_not_active"
	CodeTrialExtensionTooLong = "trial_extension_too_long"
	CodeTrialEndInPast        = "trial_end_in_past"
	CodePaymentMethodRequired = "payment_method_required"
	CodePlanChangePending     = "plan_change_pending"
)

// TrialUpdate is what UpdateSubscription does to a trial: set exactly one of ExtendDays (adds to the
// current end), EndsAt (RFC 3339, absolute new end, at least 30 minutes ahead) or Convert (end the trial
// now and start paid billing). Reason is optional, up to 500 characters.
type TrialUpdate struct {
	ExtendDays int    `json:"extend_days,omitempty"`
	EndsAt     string `json:"ends_at,omitempty"`
	Convert    bool   `json:"convert,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// TrialSubscription is the subscription as Core returns it after a trial update. The fields typed here
// are the trial's; TrialEndsAt is nil when the subscription has no trial.
type TrialSubscription struct {
	ExternalID   string  `json:"external_id"`
	Status       string  `json:"status"`
	TrialEndsAt  *string `json:"trial_ends_at"`
	TrialEndedAt *string `json:"trial_ended_at"`
	InTrial      bool    `json:"in_trial"`
}

// UpdateSubscriptionResult is the response of UpdateSubscription.
type UpdateSubscriptionResult struct {
	Subscription TrialSubscription `json:"subscription"`
}

func (t TrialUpdate) validate(op string) error {
	given := 0
	if t.ExtendDays != 0 {
		given++
	}
	if t.EndsAt != "" {
		given++
	}
	if t.Convert {
		given++
	}
	if given != 1 {
		return &ValidationError{op, "set exactly one of ExtendDays, EndsAt or Convert"}
	}
	if t.ExtendDays < 0 {
		return &ValidationError{op, "ExtendDays must be a positive number of days"}
	}
	if t.EndsAt != "" {
		if _, err := time.Parse(time.RFC3339, t.EndsAt); err != nil {
			return &ValidationError{op, "EndsAt must be an RFC 3339 timestamp"}
		}
	}
	if len([]rune(t.Reason)) > 500 {
		return &ValidationError{op, "Reason must not exceed 500 characters"}
	}
	return nil
}

// UpdateSubscription changes a subscription's trial (PUT /subscriptions/{id}, Core). It is not retried:
// a repeated extension would add twice. A refusal is an *APIError; test it with HasCode(CodeTrial...).
func (c *Client) UpdateSubscription(ctx context.Context, externalID string, trial TrialUpdate) (UpdateSubscriptionResult, error) {
	return c.updateTrial(ctx, "update_subscription", externalID, trial)
}

func (c *Client) updateTrial(ctx context.Context, op, externalID string, trial TrialUpdate) (UpdateSubscriptionResult, error) {
	var out UpdateSubscriptionResult
	if err := validateKey(c.apiKey, op); err != nil {
		return out, err
	}
	if err := required(op, "external_id", externalID); err != nil {
		return out, err
	}
	if err := trial.validate(op); err != nil {
		return out, err
	}
	body := map[string]any{"subscription": map[string]any{"trial": trial}}
	err := c.request(ctx, op, "PUT", true, "/api/v1/subscriptions/"+segment(externalID), body, &out, "")
	return out, err
}

// ExtendTrial extends a trial by days, or, when days is 0, moves its end to endsAt (RFC 3339).
// Give one of them. See UpdateSubscription.
func (c *Client) ExtendTrial(ctx context.Context, externalID string, days int, endsAt, reason string) (UpdateSubscriptionResult, error) {
	return c.updateTrial(ctx, "extend_trial", externalID, TrialUpdate{ExtendDays: days, EndsAt: endsAt, Reason: reason})
}

// ConvertTrial ends a trial now and starts paid billing per the plan's rules. See UpdateSubscription.
func (c *Client) ConvertTrial(ctx context.Context, externalID, reason string) (UpdateSubscriptionResult, error) {
	return c.updateTrial(ctx, "convert_trial", externalID, TrialUpdate{Convert: true, Reason: reason})
}
