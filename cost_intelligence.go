package nozle

import (
	"context"
	"net/url"
	"strconv"
)

// CostIntelligenceReportParams selects the report period (RFC 3339 or YYYY-MM-DD; Engine
// defaults: the last 30 days) and the rows per list (1-500, Engine default 100).
type CostIntelligenceReportParams struct {
	From  string
	To    string
	Limit int
}

type CostIntelligenceSummary struct {
	TotalCost        Decimal `json:"total_cost"`
	DirectCost       Decimal `json:"direct_cost"`
	SharedCost       Decimal `json:"shared_cost"`
	LedgerEntryCount int64   `json:"ledger_entry_count"`
	Currency         string  `json:"currency"`
}

type CostIntelligenceBreakdown struct {
	Key           string  `json:"key"`
	Cost          Decimal `json:"cost"`
	CustomerCount int64   `json:"customer_count"`
	EntryCount    int64   `json:"entry_count"`
	Currency      string  `json:"currency"`
}

type CostIntelligenceLedgerEntry struct {
	LedgerEntryID       string  `json:"ledger_entry_id"`
	SourceExternalID    string  `json:"source_external_id"`
	ParentTransactionID string  `json:"parent_transaction_id,omitempty"`
	ExternalCustomerID  string  `json:"external_customer_id,omitempty"`
	FeatureCode         string  `json:"feature_code,omitempty"`
	EntryKind           string  `json:"entry_kind"`
	Status              string  `json:"status"`
	Quantity            Decimal `json:"quantity"`
	Amount              Decimal `json:"amount"`
	Currency            string  `json:"currency"`
	FilterValues        string  `json:"filter_values"`
	OccurredAt          string  `json:"occurred_at"`
}

type CostIntelligenceUncostedSource struct {
	SourceType         string `json:"source_type"`
	SourceExternalID   string `json:"source_external_id"`
	FeatureCode        string `json:"feature_code,omitempty"`
	ExternalCustomerID string `json:"external_customer_id,omitempty"`
	Status             string `json:"status"`
	FailureCode        string `json:"failure_code,omitempty"`
	FailureMessage     string `json:"failure_message,omitempty"`
	OccurredAt         string `json:"occurred_at"`
}

type CostIntelligenceConflict struct {
	SourceType             string `json:"source_type"`
	SourceExternalID       string `json:"source_external_id"`
	AcceptedPayloadHash    string `json:"accepted_payload_hash"`
	ConflictingPayloadHash string `json:"conflicting_payload_hash"`
	SourceTopic            string `json:"source_topic"`
	SourcePartition        int    `json:"source_partition"`
	SourceOffset           int64  `json:"source_offset"`
	DetectedAt             string `json:"detected_at"`
}

// CostIntelligenceReportResult is the ledger-backed cost report for one period.
type CostIntelligenceReportResult struct {
	From        string                           `json:"from"`
	To          string                           `json:"to"`
	Summary     CostIntelligenceSummary          `json:"summary"`
	Features    []CostIntelligenceBreakdown      `json:"features"`
	Customers   []CostIntelligenceBreakdown      `json:"customers"`
	CostEvents  []CostIntelligenceLedgerEntry    `json:"cost_events"`
	SharedCosts []CostIntelligenceLedgerEntry    `json:"shared_costs"`
	Uncosted    []CostIntelligenceUncostedSource `json:"uncosted"`
	Conflicts   []CostIntelligenceConflict       `json:"conflicts"`
}

type SharedCostCustomerAllocation struct {
	ExternalCustomerID string  `json:"external_customer_id"`
	Amount             Decimal `json:"amount"`
}

type SharedCostAllocationPreview struct {
	AllocationPeriodStart  string                         `json:"allocation_period_start"`
	AllocationPeriodEnd    string                         `json:"allocation_period_end"`
	EligibilityPeriodStart string                         `json:"eligibility_period_start"`
	EligibilityPeriodEnd   string                         `json:"eligibility_period_end"`
	RecognizedAmount       Decimal                        `json:"recognized_amount"`
	Currency               string                         `json:"currency"`
	ConsumerCount          int                            `json:"consumer_count"`
	Allocations            []SharedCostCustomerAllocation `json:"allocations"`
}

// SharedCostAllocationRun is a finalized allocation; Status is "completed" or "unallocated".
type SharedCostAllocationRun struct {
	ID                    string  `json:"id"`
	Status                string  `json:"status"`
	AllocationPeriodStart string  `json:"allocation_period_start"`
	AllocationPeriodEnd   string  `json:"allocation_period_end"`
	RecognizedAmount      Decimal `json:"recognized_amount"`
	Currency              string  `json:"currency"`
	ConsumerCount         int     `json:"consumer_count"`
}

// CostIntelligenceReport returns cost totals, breakdowns, ledger entries and review queues (secret keys).
func (c *Client) CostIntelligenceReport(ctx context.Context, p CostIntelligenceReportParams) (CostIntelligenceReportResult, error) {
	var out CostIntelligenceReportResult
	if err := validateSecret(c, "cost_intelligence.report"); err != nil {
		return out, err
	}
	q := url.Values{}
	if p.From != "" {
		q.Set("from", p.From)
	}
	if p.To != "" {
		q.Set("to", p.To)
	}
	if p.Limit != 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	err := c.request(ctx, "cost_intelligence.report", "GET", false, query("/api/v1/cost-intelligence/report", q), nil, &out, "")
	return out, err
}

// PreviewSharedCostAllocation returns the recognized amount and customer split of one pool's
// period (allocationPeriodStart, RFC 3339) without writing anything.
func (c *Client) PreviewSharedCostAllocation(ctx context.Context, poolID, allocationPeriodStart string) (SharedCostAllocationPreview, error) {
	var out SharedCostAllocationPreview
	err := c.sharedCost(ctx, "preview_shared_cost_allocation", "preview", poolID, allocationPeriodStart, &out)
	return out, err
}

// AllocateSharedCost books a closed period of one pool now (Nozle also does it within about 15
// minutes of the close). Repeating it returns the first run.
func (c *Client) AllocateSharedCost(ctx context.Context, poolID, allocationPeriodStart string) (SharedCostAllocationRun, error) {
	var out SharedCostAllocationRun
	err := c.sharedCost(ctx, "allocate_shared_cost", "allocate", poolID, allocationPeriodStart, &out)
	return out, err
}

func (c *Client) sharedCost(ctx context.Context, name, action, poolID, start string, out any) error {
	op := "cost_intelligence." + name
	if err := validateSecret(c, op); err != nil {
		return err
	}
	if err := required(op, "pool_id", poolID); err != nil {
		return err
	}
	if err := required(op, "allocation_period_start", start); err != nil {
		return err
	}
	path := "/api/v1/cost-intelligence/shared-costs/" + segment(poolID) + "/" + action
	return c.request(ctx, op, "POST", false, path, map[string]string{"allocation_period_start": start}, out, "")
}
