package nozle_test

import (
	"context"
	"testing"

	nozle "github.com/nozle-dev/nozle-go"
)

// The bodies below are the answers of the production Engine for a feature priced 0.10 with cost rules of 0.04 and 0.08
// (2026-11-01), under deny and warn margin policies with a 50% minimum.
const (
	canAllowedBody = `{"allowed":true,"feature_type":"metered","used":0,"limit":null,"remaining":null,"economics":{"status":"estimated","currency":"USD",
"estimated_incremental_cost":"0.04","estimated_incremental_revenue":"0.1","estimated_incremental_margin":"0.06","estimated_margin_percent":"60",
"calculated_at":"2026-11-01T01:59:31.917115409Z"},"policy":{"version_id":"v1","mode":"deny","on_unavailable":"deny","minimum_margin_percent":"50",
"maximum_snapshot_age_seconds":300,"decision":"allow","reason":"within_policy"}}`
	canDeniedBody = `{"allowed":false,"reason":"economic_policy_denied","feature_type":"metered","used":0,"limit":null,"remaining":null,"economics":{"status":"estimated",
"currency":"USD","estimated_incremental_cost":"0.08","estimated_incremental_revenue":"0.1","estimated_incremental_margin":"0.02","estimated_margin_percent":"20"},
"policy":{"version_id":"v1","mode":"deny","on_unavailable":"deny","minimum_margin_percent":"50","maximum_snapshot_age_seconds":300,"decision":"deny",
"reason":"minimum_margin_percentage_not_met"}}`
	canWarnedBody = `{"allowed":true,"feature_type":"metered","used":0,"limit":null,"remaining":null,"economics":{"status":"estimated","currency":"USD",
"estimated_incremental_cost":"0.08","estimated_incremental_revenue":"0.1","estimated_incremental_margin":"0.02","estimated_margin_percent":"20"},
"policy":{"version_id":"v2","mode":"warn","on_unavailable":"allow","minimum_margin_amount":"0.05","maximum_estimated_cost":"0.05",
"maximum_snapshot_age_seconds":300,"decision":"warn","reason":"minimum_margin_percentage_not_met"}}`
)

func canWith(t *testing.T, body string) nozle.CanResult {
	t.Helper()
	srv, _ := answer(t, 200, body)
	res, err := nozle.New("sk_test-key", nozle.WithBaseURL(srv.URL)).Can(context.Background(), "mg-c-1", "mg_costly")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestCan_policyIsTypedAndAllowedWithinPolicy(t *testing.T) {
	res := canWith(t, canAllowedBody)
	if !res.Allowed || res.Policy == nil || res.MarginWarning() {
		t.Fatalf("got %+v", res)
	}
	want := nozle.CanPolicy{VersionID: "v1", Mode: "deny", OnUnavailable: "deny", MinimumMarginPercent: "50", MaximumSnapshotAgeSeconds: 300,
		Decision: nozle.PolicyDecisionAllow, Reason: "within_policy"}
	if *res.Policy != want {
		t.Fatalf("policy %+v, want %+v", *res.Policy, want)
	}
	if res.Economics == nil || res.Economics.EstimatedMarginPercent != "60" || res.Economics.EstimatedIncrementalCost != "0.04" {
		t.Fatalf("economics %+v", res.Economics)
	}
}

func TestCan_policyCarriesOnNotConfiguredAndScope(t *testing.T) {
	body := `{"allowed":true,"feature_type":"metered","used":0,"limit":null,"remaining":null,"policy":{"version_id":"v3","mode":"deny",
"on_unavailable":"warn","on_not_configured":"deny","scope":"plan","maximum_snapshot_age_seconds":300,"decision":"allow","reason":"within_policy"}}`
	res := canWith(t, body)
	if res.Policy == nil || res.Policy.OnNotConfigured != nozle.PolicyDecisionDeny || res.Policy.Scope != nozle.PolicyScopePlan {
		t.Fatalf("policy %+v", res.Policy)
	}
	// An older Engine sends neither field.
	old := canWith(t, canAllowedBody)
	if old.Policy.OnNotConfigured != "" || old.Policy.Scope != "" {
		t.Fatalf("policy %+v", old.Policy)
	}
}

func TestCan_marginDenialCarriesThePolicyReason(t *testing.T) {
	res := canWith(t, canDeniedBody)
	if res.Allowed || res.Reason != nozle.RefusalReasonEconomicPolicyDenied || res.Policy == nil ||
		res.Policy.Decision != nozle.PolicyDecisionDeny || res.Policy.Reason != "minimum_margin_percentage_not_met" || res.MarginWarning() {
		t.Fatalf("got %+v", res)
	}
}

func TestCan_warnModeIsAllowedButMarginWarningIsTrue(t *testing.T) {
	res := canWith(t, canWarnedBody)
	if !res.Allowed || !res.MarginWarning() || res.Policy.Mode != "warn" ||
		res.Policy.MinimumMarginAmount != "0.05" || res.Policy.MaximumEstimatedCost != "0.05" || res.Policy.MinimumMarginPercent != "" {
		t.Fatalf("got %+v", res)
	}
}

func TestCan_noPolicyLeavesPolicyNil(t *testing.T) {
	res := canWith(t, `{"allowed":true,"used":0,"limit":null,"remaining":null}`)
	if res.Policy != nil || res.MarginWarning() {
		t.Fatalf("got %+v", res)
	}
}
