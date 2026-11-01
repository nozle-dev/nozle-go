package nozle

import (
	"context"
	"errors"
)

// Codes of a refused Events.Void, and of a Track of a voided ID (CodeTransactionVoided, a 409). Test them
// with APIError.HasCode.
const (
	CodeEventNotFound     = "event_not_found"
	CodePeriodInvoiced    = "period_invoiced"
	CodeReasonRequired    = "reason_required"
	CodeTransactionVoided = "transaction_voided"
)

// EventsService is Client.Events.
type EventsService struct{ c *Client }

// EventVoidEffects says what a void did to each downstream record: "pending" while its async step has not
// finished, "not_applicable" when there was nothing to undo.
type EventVoidEffects struct {
	Event   string `json:"event"`   // voided
	Usage   string `json:"usage"`   // reversed | recomputed | not_applicable | pending
	Credits string `json:"credits"` // refunded | not_applicable | pending
	Cost    string `json:"cost"`    // reversed | not_applicable | pending
	Margin  string `json:"margin"`  // reversed | not_applicable | pending
}

// EventVoid is the result of Events.Void. Status is "voided" for this call and "already_voided" for a replay.
type EventVoid struct {
	TransactionID string           `json:"transaction_id"`
	Status        string           `json:"status"`
	Reason        string           `json:"reason"`
	VoidedAt      string           `json:"voided_at"` // RFC 3339, server time
	Actor         string           `json:"actor"`     // api_key:<id>
	Effects       EventVoidEffects `json:"effects"`
}

// Void voids a recorded usage event by transaction ID (POST /events/{transaction_id}/void, Engine) and
// reverses its usage, credits, cost and margin. reason is required, 1 to 500 characters. It is idempotent: a
// replay returns Status "already_voided", and a failed call is retried like Track. A refusal is an *APIError;
// test it with HasCode(CodeEventNotFound), HasCode(CodePeriodInvoiced) or HasCode(CodeReasonRequired).
func (s *EventsService) Void(ctx context.Context, transactionID, reason string) (*EventVoid, error) {
	const op = "events.void"
	if err := validateKey(s.c.apiKey, op); err != nil {
		return nil, err
	}
	if err := required(op, "transaction_id", transactionID); err != nil {
		return nil, err
	}
	if n := len([]rune(reason)); n < 1 || n > 500 {
		return nil, &ValidationError{op, "reason must be 1 to 500 characters"}
	}
	var out struct {
		Void EventVoid `json:"void"`
	}
	err := s.c.request(ctx, op, "POST", false, "/api/v1/events/"+segment(transactionID)+"/void", map[string]any{"reason": reason}, &out, "")
	if err != nil {
		if details, ok := errorDetails(err); ok {
			// Engine names the refusal in "error"; surface it as "code" so HasCode works.
			if name, _ := details["error"].(string); name != "" && details["code"] == nil {
				details["code"] = name
			}
		}
		return nil, err
	}
	return &out.Void, nil
}

func errorDetails(err error) (map[string]any, bool) {
	var api *APIError
	if !errors.As(err, &api) {
		return nil, false
	}
	details, ok := api.Details.(map[string]any)
	return details, ok
}
