package nozle

import (
	"context"
	"net/url"
	"strconv"
)

// PlanVersion is one version of a plan code. A plan is version 1 until its pricing is changed while customers pay
// for it; that change saves version 2, and so on. Subscriptions stay on their version until a version migration
// moves them.
type PlanVersion struct {
	NozleID                  string `json:"nozle_id"`
	Version                  int    `json:"version"`
	IsCurrent                bool   `json:"is_current"`
	CreatedAt                string `json:"created_at"`
	AmountCents              int    `json:"amount_cents"`
	AmountCurrency           string `json:"amount_currency"`
	ActiveSubscriptionsCount int    `json:"active_subscriptions_count"`
}

// PlanVersionMigrationParams names the version whose subscriptions move and, optionally, the version they move to
// (zero means the current version).
type PlanVersionMigrationParams struct {
	FromVersion int
	ToVersion   int
}

// PlanVersionMigration moves the active subscriptions of one version of a plan code to another, each at the end of
// its billing period. Items is filled by GetPlanVersionMigration only.
type PlanVersionMigration struct {
	NozleID        string                     `json:"nozle_id"`
	PlanCode       string                     `json:"plan_code"`
	FromVersion    int                        `json:"from_version"`
	ToVersion      int                        `json:"to_version"`
	Status         string                     `json:"status"`
	MatchedCount   int                        `json:"matched_count"`
	SucceededCount int                        `json:"succeeded_count"`
	SkippedCount   int                        `json:"skipped_count"`
	FailedCount    int                        `json:"failed_count"`
	StartedAt      string                     `json:"started_at"`
	FinishedAt     string                     `json:"finished_at"`
	CanceledAt     string                     `json:"canceled_at"`
	CreatedAt      string                     `json:"created_at"`
	Items          []PlanVersionMigrationItem `json:"items"`
}

// PlanVersionMigrationItem is what a migration did to one subscription. MoveStatus says where a scheduled move
// stands: "pending", "moved" or "canceled" (empty for an item that is no move).
type PlanVersionMigrationItem struct {
	ExternalSubscriptionID string `json:"external_subscription_id"`
	Status                 string `json:"status"`
	SkipReason             string `json:"skip_reason"`
	Error                  string `json:"error"`
	EffectiveAt            string `json:"effective_at"`
	MoveStatus             string `json:"move_status"`
}

// PlanVersionMigrationPreview says what a migration would do, without changing anything.
type PlanVersionMigrationPreview struct {
	PlanCode    string                            `json:"plan_code"`
	FromVersion int                               `json:"from_version"`
	ToVersion   int                               `json:"to_version"`
	Items       []PlanVersionMigrationPreviewItem `json:"items"`
}

// PlanVersionMigrationPreviewItem is one subscription of a preview: Outcome "move" (on EffectiveOn, from
// CurrentAmountCents to NewAmountCents) or "skip" with SkipReason.
type PlanVersionMigrationPreviewItem struct {
	ExternalSubscriptionID string `json:"external_subscription_id"`
	ExternalCustomerID     string `json:"external_customer_id"`
	Outcome                string `json:"outcome"`
	SkipReason             string `json:"skip_reason"`
	EffectiveOn            string `json:"effective_on"`
	CurrentAmountCents     int    `json:"current_amount_cents"`
	NewAmountCents         int    `json:"new_amount_cents"`
}

// PlanVersions lists every version of a plan code, oldest first, with its price and active subscriptions.
// Needs a secret key.
func (c *Client) PlanVersions(ctx context.Context, code string) ([]PlanVersion, error) {
	if err := required("plan_versions", "plan code", code); err != nil {
		return nil, err
	}
	var out struct {
		Versions []PlanVersion `json:"versions"`
	}
	err := c.request(ctx, "plan_versions", "GET", true, "/api/v1/plans/"+segment(code)+"/versions", nil, &out, "")
	return out.Versions, err
}

// PlanVersion returns one version of a plan code, for example the older version a subscriber is billed on.
// Needs a secret key.
func (c *Client) PlanVersion(ctx context.Context, code string, version int) (Plan, error) {
	var out struct {
		Plan Plan `json:"plan"`
	}
	if err := required("plan_version", "plan code", code); err != nil {
		return out.Plan, err
	}
	if version < 1 {
		return out.Plan, &ValidationError{"plan_version", "version must be 1 or more"}
	}
	path := query("/api/v1/plans/"+segment(code), url.Values{"version": {strconv.Itoa(version)}})
	err := c.request(ctx, "plan_version", "GET", true, path, nil, &out, "")
	return out.Plan, err
}

// PreviewPlanVersionMigration says what migrating the subscriptions of p.FromVersion would do to each of them.
// Nothing is written. Needs a secret key.
func (c *Client) PreviewPlanVersionMigration(ctx context.Context, code string, p PlanVersionMigrationParams) (PlanVersionMigrationPreview, error) {
	var out struct {
		Preview PlanVersionMigrationPreview `json:"plan_version_migration_preview"`
	}
	body, err := migrationBody("preview_plan_version_migration", code, p)
	if err != nil {
		return out.Preview, err
	}
	err = c.request(ctx, "preview_plan_version_migration", "POST", true, migrationsPath(code)+"/preview", body, &out, "")
	return out.Preview, err
}

// CreatePlanVersionMigration starts moving the active subscriptions of p.FromVersion to p.ToVersion (the current
// version when zero). Each moves at the end of its billing period; nothing is invoiced or refunded now. The
// migration runs in the background: read it again with GetPlanVersionMigration. Needs a secret key. Another
// migration of the code still running returns a 409 *APIError with HasCode("plan_version_migration_running").
func (c *Client) CreatePlanVersionMigration(ctx context.Context, code string, p PlanVersionMigrationParams) (PlanVersionMigration, error) {
	body, err := migrationBody("create_plan_version_migration", code, p)
	if err != nil {
		return PlanVersionMigration{}, err
	}
	return c.migration(ctx, "create_plan_version_migration", "POST", migrationsPath(code), body)
}

// ListPlanVersionMigrations lists the migrations of a plan code, newest first, without their items.
func (c *Client) ListPlanVersionMigrations(ctx context.Context, code string) ([]PlanVersionMigration, error) {
	if err := required("list_plan_version_migrations", "plan code", code); err != nil {
		return nil, err
	}
	var out struct {
		Migrations []PlanVersionMigration `json:"plan_version_migrations"`
	}
	err := c.request(ctx, "list_plan_version_migrations", "GET", true, migrationsPath(code), nil, &out, "")
	return out.Migrations, err
}

// GetPlanVersionMigration returns a migration with what it did to each subscription.
func (c *Client) GetPlanVersionMigration(ctx context.Context, code, id string) (PlanVersionMigration, error) {
	if err := migrationID("get_plan_version_migration", code, id); err != nil {
		return PlanVersionMigration{}, err
	}
	return c.migration(ctx, "get_plan_version_migration", "GET", migrationsPath(code)+"/"+segment(id), nil)
}

// CancelPlanVersionMigration stops a migration and unschedules every move that has not started; those
// subscriptions stay on their version.
func (c *Client) CancelPlanVersionMigration(ctx context.Context, code, id string) (PlanVersionMigration, error) {
	if err := migrationID("cancel_plan_version_migration", code, id); err != nil {
		return PlanVersionMigration{}, err
	}
	return c.migration(ctx, "cancel_plan_version_migration", "POST", migrationsPath(code)+"/"+segment(id)+"/cancel", nil)
}

func (c *Client) migration(ctx context.Context, op, method, path string, body any) (PlanVersionMigration, error) {
	var out struct {
		Migration PlanVersionMigration `json:"plan_version_migration"`
	}
	err := c.request(ctx, op, method, true, path, body, &out, "")
	return out.Migration, err
}

func migrationsPath(code string) string {
	return "/api/v1/plans/" + segment(code) + "/version-migrations"
}

func migrationBody(op, code string, p PlanVersionMigrationParams) (map[string]any, error) {
	if err := required(op, "plan code", code); err != nil {
		return nil, err
	}
	if p.FromVersion < 1 {
		return nil, &ValidationError{op, "from version must be 1 or more"}
	}
	if p.ToVersion < 0 {
		return nil, &ValidationError{op, "to version must be 1 or more, or zero for the current version"}
	}
	body := map[string]any{"from_version": p.FromVersion}
	if p.ToVersion > 0 {
		body["to_version"] = p.ToVersion
	}
	return body, nil
}

func migrationID(op, code, id string) error {
	if err := required(op, "plan code", code); err != nil {
		return err
	}
	return required(op, "migration id", id)
}
