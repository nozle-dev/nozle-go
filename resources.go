package nozle

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func validateSecret(c *Client, op string) error { return validateKey(c.apiKey, op) }
func pageQuery(p PageOptions) (url.Values, error) {
	if p.Limit < 0 || p.Limit > 100 {
		return nil, &ValidationError{"page", "limit must be 1-100"}
	}
	q := url.Values{}
	if p.Limit > 0 {
		q.Set("limit", fmt.Sprint(p.Limit))
	}
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}
	if p.Status != "" {
		q.Set("status", p.Status)
	}
	if p.CreditSystemCode != "" {
		q.Set("credit_system_code", p.CreditSystemCode)
	}
	return q, nil
}
func (c *Client) UpsertCustomer(ctx context.Context, p Customer) (Customer, error) {
	var out struct {
		Customer Customer `json:"customer"`
	}
	if err := validateSecret(c, "customers.upsert"); err != nil {
		return out.Customer, err
	}
	if err := required("customers.upsert", "external_id", p.ExternalID); err != nil {
		return out.Customer, err
	}
	err := c.request(ctx, "customers.upsert", "POST", true, "/api/v1/customers", map[string]any{"customer": p}, &out, "")
	return out.Customer, err
}
func (c *Client) GetEntity(ctx context.Context, customer, entity string) (Entity, error) {
	var out struct {
		Entity Entity `json:"entity"`
	}
	err := c.request(ctx, "entities.get", "GET", false, entityPath(customer, entity), nil, &out, "")
	return out.Entity, err
}
func (c *Client) ListEntities(ctx context.Context, customer string, p PageOptions) (EntityPage, error) {
	var out EntityPage
	q, err := pageQuery(p)
	if err != nil {
		return out, err
	}
	err = c.request(ctx, "entities.list", "GET", false, query(customerPath(customer)+"/entities", q), nil, &out, "")
	return out, err
}
func (c *Client) UpsertEntity(ctx context.Context, customer, entity string, p EntityInput, key string) (EntityMutation, error) {
	var out EntityMutation
	if err := validateSecret(c, "entities.upsert"); err != nil {
		return out, err
	}
	if err := idem("entities.upsert", key); err != nil {
		return out, err
	}
	if err := validateEntityID(entity); err != nil {
		return out, err
	}
	if err := validateEntityStatus(p.Status); err != nil {
		return out, err
	}
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	err := c.request(ctx, "entities.upsert", "PUT", false, entityPath(customer, entity), p, &out, key)
	return out, err
}
func validateEntityID(id string) error {
	if id == "" || len([]byte(id)) > 255 {
		return &ValidationError{"entity", "ID must be 1-255 bytes"}
	}
	return nil
}
func validateEntityStatus(s string) error {
	if s != "active" && s != "suspended" && s != "deleted" {
		return &ValidationError{"entity", "invalid status"}
	}
	return nil
}
func (c *Client) SetEntityStatus(ctx context.Context, customer, entity, status, key string) (EntityMutation, error) {
	var out EntityMutation
	if err := validateEntityStatus(status); err != nil {
		return out, err
	}
	current, err := c.GetEntity(ctx, customer, entity)
	if err != nil {
		return out, err
	}
	return c.UpsertEntity(ctx, customer, entity, EntityInput{Name: current.Name, Status: status, Metadata: current.Metadata}, key)
}
func (c *Client) ActivateEntity(ctx context.Context, customer, entity, key string) (EntityMutation, error) {
	return c.SetEntityStatus(ctx, customer, entity, "active", key)
}
func (c *Client) SuspendEntity(ctx context.Context, customer, entity, key string) (EntityMutation, error) {
	return c.SetEntityStatus(ctx, customer, entity, "suspended", key)
}
func (c *Client) BulkUpsertEntities(ctx context.Context, customer string, items []EntityInput, key string) (EntityBulkResult, error) {
	var out EntityBulkResult
	if err := idem("entities.bulk_upsert", key); err != nil {
		return out, err
	}
	if len(items) < 1 || len(items) > 500 {
		return out, &ValidationError{"entities.bulk_upsert", "requires 1-500 entities"}
	}
	seen := map[string]bool{}
	for _, p := range items {
		if err := validateEntityID(p.ExternalID); err != nil {
			return out, err
		}
		if err := validateEntityStatus(p.Status); err != nil {
			return out, err
		}
		if seen[p.ExternalID] {
			return out, &ValidationError{"entities.bulk_upsert", "duplicate entity ID"}
		}
		seen[p.ExternalID] = true
	}
	err := c.request(ctx, "entities.bulk_upsert", "POST", false, customerPath(customer)+"/entities/bulk-upsert", map[string]any{"entities": items}, &out, key)
	return out, err
}
func (c *Client) ListCreditSystems(ctx context.Context) ([]CreditSystem, error) {
	if err := validateSecret(c, "credit_systems.list"); err != nil {
		return nil, err
	}
	all := []CreditSystem{}
	page := 1
	seen := map[int]bool{}
	for page > 0 {
		if seen[page] {
			return nil, &ValidationError{"credit_systems.list", "pagination cycle"}
		}
		seen[page] = true
		var out struct {
			CreditSystems []struct {
				ID          string `json:"lago_id"`
				Code        string `json:"code"`
				Name        string `json:"name"`
				Description string `json:"description"`
				UnitName    string `json:"unit_name"`
				Status      string `json:"status"`
				CreatedAt   string `json:"created_at"`
				UpdatedAt   string `json:"updated_at"`
			} `json:"credit_systems"`
			Meta struct {
				NextPage *int `json:"next_page"`
			} `json:"meta"`
		}
		q := url.Values{"status": {"active"}, "page": {fmt.Sprint(page)}, "per_page": {"100"}}
		if err := c.request(ctx, "credit_systems.list", "GET", true, query("/api/v1/credit-systems", q), nil, &out, ""); err != nil {
			return nil, err
		}
		for _, x := range out.CreditSystems {
			all = append(all, CreditSystem{x.ID, x.Code, x.Name, x.Description, x.UnitName, x.Status, x.CreatedAt, x.UpdatedAt})
		}
		if out.Meta.NextPage == nil {
			break
		}
		page = *out.Meta.NextPage
	}
	return all, nil
}
func (c *Client) GetCreditBalance(ctx context.Context, customer, system string) (CreditBalance, error) {
	var out CreditBalance
	err := c.request(ctx, "credits.get_balance", "GET", false, customerPath(customer)+"/credit-systems/"+segment(system)+"/balance", nil, &out, "")
	return out, err
}
func (c *Client) ListCreditBalances(ctx context.Context, customer string) (CreditBalances, error) {
	var out CreditBalances
	err := c.request(ctx, "credits.list_balances", "GET", false, customerPath(customer)+"/credit-systems", nil, &out, "")
	return out, err
}
func (c *Client) ListCreditOperations(ctx context.Context, customer string, p PageOptions) (CreditOperationPage, error) {
	var out CreditOperationPage
	q, err := pageQuery(p)
	if err != nil {
		return out, err
	}
	err = c.request(ctx, "credits.list_operations", "GET", false, query(customerPath(customer)+"/credit-operations", q), nil, &out, "")
	return out, err
}
func (c *Client) GetEntityCreditBalance(ctx context.Context, customer, entity, system string) (EntityCreditBalance, error) {
	var out EntityCreditBalance
	err := c.request(ctx, "credits.get_entity_balance", "GET", false, entityPath(customer, entity)+"/credit-systems/"+segment(system)+"/balance", nil, &out, "")
	return out, err
}
func (c *Client) ListEntityCreditBalances(ctx context.Context, customer, entity string) (EntityCreditBalances, error) {
	var out EntityCreditBalances
	err := c.request(ctx, "credits.list_entity_balances", "GET", false, entityPath(customer, entity)+"/credit-systems", nil, &out, "")
	return out, err
}
func (c *Client) ListEntityCreditOperations(ctx context.Context, customer, entity string, p PageOptions) (CreditOperationPage, error) {
	var out CreditOperationPage
	q, err := pageQuery(p)
	if err != nil {
		return out, err
	}
	err = c.request(ctx, "credits.list_entity_operations", "GET", false, query(entityPath(customer, entity)+"/credit-operations", q), nil, &out, "")
	return out, err
}

var amountPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.[0-9]{1,12})?$`)

func validateAmount(amount Decimal) error {
	if !amountPattern.MatchString(string(amount)) || strings.TrimLeft(strings.ReplaceAll(string(amount), ".", ""), "0") == "" {
		return &ValidationError{"credit transfer", "amount must be a positive decimal string with at most 12 places"}
	}
	return nil
}
func (c *Client) transfer(ctx context.Context, action, customer, entity, system string, amount Decimal, key string) (CreditTransfer, error) {
	var out CreditTransfer
	if err := validateSecret(c, "credits."+action); err != nil {
		return out, err
	}
	if err := validateAmount(amount); err != nil {
		return out, err
	}
	if err := idem("credits."+action, key); err != nil {
		return out, err
	}
	suffix := "credit-allocations"
	if action == "deallocate" {
		suffix = "credit-deallocations"
	}
	err := c.request(ctx, "credits."+action, "POST", false, entityPath(customer, entity)+"/"+suffix, map[string]any{"credit_system": system, "amount": amount}, &out, key)
	return out, err
}
func (c *Client) AllocateCredits(ctx context.Context, customer, entity, system string, amount Decimal, key string) (CreditTransfer, error) {
	return c.transfer(ctx, "allocate", customer, entity, system, amount, key)
}
func (c *Client) DeallocateCredits(ctx context.Context, customer, entity, system string, amount Decimal, key string) (CreditTransfer, error) {
	return c.transfer(ctx, "deallocate", customer, entity, system, amount, key)
}
func usageBody(p UsageParams, check bool) map[string]any {
	props := p.Properties
	if props == nil {
		props = map[string]any{}
	}
	b := map[string]any{"customer_id": p.CustomerID, "feature_code": p.FeatureCode, "properties": props}
	timestamp := p.Timestamp
	if timestamp == "" {
		timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if check {
		b["occurred_at"] = timestamp
	} else {
		b["timestamp"] = timestamp
	}
	if p.EntityID != "" {
		b["entity_id"] = p.EntityID
	}
	if p.CreditSystemCode != "" {
		b["credit_system_code"] = p.CreditSystemCode
	}
	return b
}
func (c *Client) CheckUsage(ctx context.Context, p UsageParams) (UsageResult, error) {
	var out UsageResult
	if err := validateSecret(c, "usage.check"); err != nil {
		return out, err
	}
	err := c.request(ctx, "usage.check", "POST", false, "/api/v1/usage/check", usageBody(p, true), &out, "")
	return out, err
}
func (c *Client) TrackUsage(ctx context.Context, p UsageParams, key string) (UsageResult, error) {
	var out UsageResult
	if err := validateSecret(c, "usage.track"); err != nil {
		return out, err
	}
	if err := idem("usage.track", key); err != nil {
		return out, err
	}
	err := c.request(ctx, "usage.track", "POST", false, "/api/v1/usage/track", usageBody(p, false), &out, key)
	return out, err
}
func (c *Client) TrackCostEvent(ctx context.Context, p CostEventParams) (CostEventResult, error) {
	var out CostEventResult
	if err := validateSecret(c, "cost_events.track"); err != nil {
		return out, err
	}
	if p.CostMeterCode == "" || (p.ParentTransactionID == "" && p.ExternalCustomerID == "") {
		return out, &ValidationError{"cost_events.track", "cost meter and parent transaction or customer required"}
	}
	id := p.CostEventID
	if id == "" {
		id = generateUUID()
	}
	props := p.Properties
	if props == nil {
		props = map[string]any{}
	}
	b := map[string]any{"cost_event_id": id, "cost_meter_code": p.CostMeterCode, "properties": props}
	for k, v := range map[string]string{"parent_transaction_id": p.ParentTransactionID, "external_customer_id": p.ExternalCustomerID, "request_id": p.RequestID, "operation_key": p.OperationKey} {
		if v != "" {
			b[k] = v
		}
	}
	if p.Timestamp != nil {
		b["timestamp"] = *p.Timestamp
	}
	err := c.request(ctx, "cost_events.track", "POST", false, "/api/v1/cost-events", b, &out, "")
	return out, err
}
