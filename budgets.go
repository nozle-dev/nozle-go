package nozle

import (
	"context"
	"encoding/json"
	"net/url"
)

// RefusalReason is the Reason of a refused CanResult, UsageResult or CreditHoldResult, and the Code() of the LLM
// proxy's 402/429 refusals. Compare with the constants; a reason a newer Engine adds arrives as its plain string.
type RefusalReason = string

const (
	RefusalReasonFeatureNotFound        RefusalReason = "feature_not_found"
	RefusalReasonNoSubscription         RefusalReason = "no_subscription"
	RefusalReasonSubscriptionRequired   RefusalReason = "subscription_required"
	RefusalReasonNoEntitlement          RefusalReason = "no_entitlement"
	RefusalReasonUsageLimitExceeded     RefusalReason = "usage_limit_exceeded"
	RefusalReasonEconomicPolicyDenied   RefusalReason = "economic_policy_denied"
	RefusalReasonCustomerPaymentOverdue RefusalReason = "customer_payment_overdue"
	RefusalReasonInsufficientCredits    RefusalReason = "insufficient_credits"
	RefusalReasonBudgetExceeded         RefusalReason = "budget_exceeded"
)

// HoldKindBudget is CreditHold.Kind of a budget hold. A credit hold has an empty Kind.
const HoldKindBudget = "budget"

// Budget is one money budget (spend limit) and its standing in the current period (Engine 2.34.0). Amounts are
// decimal strings in major units of Currency.
type Budget struct {
	// FeatureCode is the Feature the limit covers; empty for the subscription's total limit.
	FeatureCode string  `json:"feature_code"`
	Limit       Decimal `json:"limit"`
	// Spent is the gross metered usage charges booked this period; Held is what open budget holds reserve.
	Spent     Decimal `json:"spent"`
	Held      Decimal `json:"held"`
	Remaining Decimal `json:"remaining"`
	Currency  string  `json:"currency"`
	// Period is billing_period, calendar_month or lifetime.
	Period      string `json:"period"`
	PeriodStart string `json:"period_start"`
	// PeriodEnd is empty for a lifetime limit.
	PeriodEnd string `json:"period_end"`
	// Mode is hard or soft; a soft limit never refuses.
	Mode string `json:"mode"`
	// Status is ok, warning (a threshold is reached) or exceeded (spent plus held reaches the limit).
	Status string `json:"status"`
	// AsOf is the time of the latest event booked into Spent; empty before any.
	AsOf string `json:"as_of"`
}

// BudgetList answers ListBudgets.
type BudgetList struct {
	CustomerID     string   `json:"customer_id"`
	SubscriptionID string   `json:"subscription_id"`
	Budgets        []Budget `json:"budgets"`
}

// ListBudgets returns every spend limit of the customer's subscription with its spend, open holds and status in the
// current period. subscriptionID picks the subscription of a customer with several; "" uses the only one.
func (c *Client) ListBudgets(ctx context.Context, customerID, subscriptionID string) (BudgetList, error) {
	var out BudgetList
	if err := required("budgets.list", "customer id", customerID); err != nil {
		return out, err
	}
	q := url.Values{}
	if subscriptionID != "" {
		q.Set("subscription_id", subscriptionID)
	}
	err := c.request(ctx, "budgets.list", "GET", false, query(customerPath(customerID)+"/budgets", q), nil, &out, "")
	return out, err
}

// BudgetExceededError is a 402 budget_exceeded: a hard spend limit is used up. Budget is the limit's standing when
// the response carried one. It unwraps to its *APIError, so errors.As(err, &apiErr) still matches.
type BudgetExceededError struct {
	*APIError
	Budget *Budget
}

func (e *BudgetExceededError) Unwrap() error { return e.APIError }

// UsageLimitExceededError is a 429 usage_limit_exceeded: a hard unit limit is reached. It unwraps to its *APIError.
type UsageLimitExceededError struct{ *APIError }

func (e *UsageLimitExceededError) Unwrap() error { return e.APIError }

// typedAPIError picks the error for a failed response by status and exact code.
func typedAPIError(e *APIError) error {
	switch {
	case e.StatusCode == 402 && e.Code() == RefusalReasonBudgetExceeded:
		out := &BudgetExceededError{APIError: e}
		details, _ := e.Details.(map[string]any)
		if raw, ok := details["budget"].(map[string]any); ok {
			if b, err := json.Marshal(raw); err == nil {
				var budget Budget
				if json.Unmarshal(b, &budget) == nil {
					out.Budget = &budget
				}
			}
		}
		return out
	case e.StatusCode == 429 && e.Code() == RefusalReasonUsageLimitExceeded:
		return &UsageLimitExceededError{e}
	}
	return e
}
