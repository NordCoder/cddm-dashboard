package orchestration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

const (
	AutonomousMaterializationPending      = "pending"
	AutonomousMaterializationMaterialized = "materialized"
	AutonomousMaterializationCompleted    = "completed"
	AutonomousMaterializationBlocked      = "blocked"
	AutonomousMaterializationSuperseded   = "superseded"
	AutonomousMaterializationAmbiguous    = "ambiguous"
)

const autopilotLeaseOwner = "dashboard-autopilot"

type AutonomousMaterialization struct {
	ID                 string     `json:"materialization_id"`
	ProjectID          int64      `json:"project_id"`
	IntentID           string     `json:"intent_id"`
	LeaseID            string     `json:"lease_id"`
	ProvisionRequestID string     `json:"provision_request_id"`
	SchedulerLaneKey   string     `json:"scheduler_lane_key"`
	DeliveryLaneKey    string     `json:"delivery_lane_key,omitempty"`
	PlanID             int64      `json:"plan_id,omitempty"`
	Status             string     `json:"status"`
	ReasonCode         string     `json:"reason_code,omitempty"`
	WorkflowCommandID  string     `json:"workflow_command_id,omitempty"`
	DeliveryCommandID  string     `json:"delivery_command_id,omitempty"`
	ContextHash        string     `json:"context_hash,omitempty"`
	PromptHash         string     `json:"prompt_hash,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

type AutonomousPlanner interface {
	GenerateAutonomous(context.Context, int64, int, string, string) (planning.GenerationResult, error)
	Get(context.Context, int64, int, int64) (planning.GenerationResult, error)
}

type AutonomousDelivery interface {
	Create(context.Context, int64, int, delivery.Confirmation) (delivery.Command, error)
}

// DeliveryBindingResolver preserves the canonical Stage-3 delivery lane while
// resolving it to the exact M10/M11 scheduler binding recorded by one active
// autonomous materialization. Manual bindings always take precedence.
type DeliveryBindingResolver struct {
	db   *sql.DB
	base delivery.BindingResolver
}

func NewDeliveryBindingResolver(db *sql.DB, base delivery.BindingResolver) *DeliveryBindingResolver {
	return &DeliveryBindingResolver{db: db, base: base}
}

func (r *DeliveryBindingResolver) Resolve(ctx context.Context, projectID int64, laneKey string) (delivery.BindingSnapshot, error) {
	if r == nil || r.db == nil || r.base == nil {
		return delivery.BindingSnapshot{}, delivery.ErrUnavailable
	}
	if value, err := r.base.Resolve(ctx, projectID, laneKey); err == nil {
		return value, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT scheduler_lane_key FROM autonomous_command_materializations
		WHERE project_id=? AND delivery_lane_key=? AND status IN ('pending','materialized')
		ORDER BY created_at DESC,id DESC LIMIT 2`, projectID, strings.TrimSpace(laneKey))
	if err != nil {
		return delivery.BindingSnapshot{}, err
	}
	defer rows.Close()
	lanes := make([]string, 0, 2)
	for rows.Next() {
		var lane string
		if err := rows.Scan(&lane); err != nil {
			return delivery.BindingSnapshot{}, err
		}
		lanes = append(lanes, lane)
	}
	if err := rows.Err(); err != nil {
		return delivery.BindingSnapshot{}, err
	}
	if len(lanes) != 1 {
		return delivery.BindingSnapshot{}, delivery.ErrUnavailable
	}
	value, err := r.base.Resolve(ctx, projectID, lanes[0])
	if err != nil {
		return delivery.BindingSnapshot{}, err
	}
	value.LaneKey = strings.TrimSpace(laneKey)
	return value, nil
}

type AutopilotEngine struct {
	store        *Store
	scheduler    *Scheduler
	provisioning *ProvisioningService
	planner      AutonomousPlanner
	delivery     AutonomousDelivery
	bindings     delivery.BindingResolver
	snapshots    *supervisor.Store
	now          func() time.Time
	leaseTTL     time.Duration
}

func NewAutopilotEngine(store *Store, scheduler *Scheduler, provisioning *ProvisioningService, planner AutonomousPlanner, deliveryService AutonomousDelivery, bindings delivery.BindingResolver, snapshots *supervisor.Store) (*AutopilotEngine, error) {
	if store == nil || scheduler == nil || provisioning == nil || planner == nil || deliveryService == nil || bindings == nil || snapshots == nil {
		return nil, fmt.Errorf("autopilot engine requires orchestration, planning, delivery, binding and snapshot services")
	}
	return &AutopilotEngine{
		store: store, scheduler: scheduler, provisioning: provisioning, planner: planner,
		delivery: deliveryService, bindings: bindings, snapshots: snapshots,
		now: func() time.Time { return time.Now().UTC() }, leaseTTL: 24 * time.Hour,
	}, nil
}

func (e *AutopilotEngine) ReconcileAll(ctx context.Context) error {
	projects, err := e.snapshots.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, project := range projects {
		snapshot, err := e.snapshots.ProjectSnapshot(ctx, project.ID)
		if err != nil {
			return err
		}
		if err := e.ReconcileProject(ctx, snapshot); err != nil {
			return fmt.Errorf("reconcile Autopilot project %d: %w", project.ID, err)
		}
	}
	return nil
}

func (e *AutopilotEngine) ReconcileProject(ctx context.Context, snapshot supervisor.ProjectSnapshot) error {
	profile, err := e.store.ProjectProfile(ctx, snapshot.Project.ID)
	if err != nil {
		return err
	}
	if profile.AutonomyMode != AutonomyModeContinuous || profile.AutonomyState != AutonomyStateEnabled {
		return nil
	}
	leases, err := e.scheduler.ListLeases(ctx, snapshot.Project.ID)
	if err != nil {
		return err
	}
	for _, lease := range leases {
		if lease.Status != LeaseActive {
			continue
		}
		if err := e.reconcileLease(ctx, snapshot, lease); err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 64; attempt++ {
		decision, err := e.scheduler.ClaimNext(ctx, ClaimRequest{
			ProjectID: snapshot.Project.ID, ClaimID: "autopilot-" + randomProvisionToken()[:32],
			LeaseOwner: autopilotLeaseOwner, LeaseTTL: e.leaseTTL, Snapshot: snapshot,
		})
		if err != nil {
			return err
		}
		if !decision.Claimed || decision.Lease == nil {
			break
		}
		if err := e.reconcileLease(ctx, snapshot, *decision.Lease); err != nil {
			return err
		}
	}
	return nil
}

func (e *AutopilotEngine) reconcileLease(ctx context.Context, snapshot supervisor.ProjectSnapshot, lease Lease) error {
	if lease.LeaseOwner != autopilotLeaseOwner {
		return nil
	}
	request, err := e.provisioning.Enqueue(ctx, EnqueueProvisioningInput{
		ProjectID: lease.ProjectID, LeaseID: lease.ID, LeaseOwner: lease.LeaseOwner, LeaseToken: lease.LeaseToken,
	})
	if err != nil {
		return err
	}
	if request.Status != ProvisionProvisioned {
		return nil
	}
	intent, err := e.store.Intent(ctx, lease.IntentID)
	if err != nil {
		return err
	}
	if intent.ActionType == ActionMerge || intent.ActionType == ActionPlanNextWave {
		return nil
	}
	if intent.ActionType != ActionDispatch && intent.ActionType != ActionCorrect {
		return e.blockMaterialization(ctx, intent, lease, request, "unsupported_autonomous_action")
	}
	return e.materialize(ctx, snapshot, intent, lease, request)
}

func (e *AutopilotEngine) materialize(ctx context.Context, snapshot supervisor.ProjectSnapshot, intent Intent, lease Lease, request ProvisionRequest) error {
	value, err := e.ensureMaterialization(ctx, intent, lease, request)
	if err != nil {
		return err
	}
	if value.Status == AutonomousMaterializationMaterialized || value.Status == AutonomousMaterializationCompleted {
		return nil
	}
	if value.Status != AutonomousMaterializationPending {
		return ErrConflict
	}
	generation := planning.GenerationResult{}
	if value.PlanID > 0 {
		generation, err = e.planner.Get(ctx, intent.ProjectID, intent.IssueNumber, value.PlanID)
	} else {
		generation, err = e.planner.GenerateAutonomous(ctx, intent.ProjectID, intent.IssueNumber, intent.Role, provisionExpectedHead(intent))
		if err == nil {
			value, err = e.recordAutonomousPlan(ctx, value.ID, generation)
		}
	}
	if err != nil {
		return e.supersedeMaterialization(ctx, value, lease, "route_or_head_changed")
	}
	if generation.Plan == nil || generation.PolicyDecision.Status != planning.StatusApproved || generation.Plan.TargetRole != intent.Role {
		return e.supersedeMaterialization(ctx, value, lease, "autonomous_plan_invalid")
	}
	binding, err := e.bindings.Resolve(ctx, intent.ProjectID, intent.LaneKey)
	if err != nil {
		return err
	}
	if binding.BindingID != request.BoundBindingID || binding.BindingVersion != request.BoundBindingVersion || !binding.Ready {
		return e.supersedeMaterialization(ctx, value, lease, "provisioned_binding_changed")
	}
	confirmation := delivery.Confirmation{
		PlanID: generation.PlanID, IdempotencyKey: "autopilot:" + intent.ID,
		ExpectedPlanHash: generation.PolicyDecision.PlanHash, ExpectedContextHash: generation.Context.ContextHash,
		ExpectedHead: generation.Context.CurrentHead, ExpectedLaneKey: generation.Plan.LaneKey,
		ExpectedBindingID: binding.BindingID, ExpectedBindingVer: binding.BindingVersion,
		ExpectedPresenceToken: binding.PresenceToken,
	}
	deliveryCommand, err := e.delivery.Create(ctx, intent.ProjectID, intent.IssueNumber, confirmation)
	if err != nil {
		return err
	}
	var workflowCommandID string
	if err := e.store.db.QueryRowContext(ctx, `SELECT workflow_command_id FROM workflow_delivery_links WHERE delivery_command_id=?`, deliveryCommand.ID).Scan(&workflowCommandID); err != nil {
		return err
	}
	now := e.now().UTC()
	result, err := e.store.db.ExecContext(ctx, `UPDATE autonomous_command_materializations SET
		status='materialized',workflow_command_id=?,delivery_command_id=?,context_hash=?,prompt_hash=?,updated_at=?
		WHERE id=? AND status='pending'`, workflowCommandID, deliveryCommand.ID, generation.Context.ContextHash,
		hashAutopilotText(deliveryCommand.Prompt), stamp(now), value.ID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		current, readErr := e.autonomousMaterialization(ctx, value.ID)
		if readErr == nil && current.WorkflowCommandID == workflowCommandID && current.DeliveryCommandID == deliveryCommand.ID {
			return nil
		}
		return ErrConflict
	}
	_, err = e.store.db.ExecContext(ctx, `UPDATE delivery_commands SET authority_kind=?,authority_ref=? WHERE id=?`, delivery.AuthorityAutonomousIntent, intent.ID, deliveryCommand.ID)
	return err
}

func (e *AutopilotEngine) ensureMaterialization(ctx context.Context, intent Intent, lease Lease, request ProvisionRequest) (AutonomousMaterialization, error) {
	id := deterministicAutopilotID(intent.ProjectID, intent.ID)
	now := stamp(e.now())
	_, err := e.store.db.ExecContext(ctx, `INSERT INTO autonomous_command_materializations(
		id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,'pending',?,?) ON CONFLICT(project_id,intent_id) DO NOTHING`,
		id, intent.ProjectID, intent.ID, lease.ID, request.ID, intent.LaneKey, now, now)
	if err != nil {
		return AutonomousMaterialization{}, err
	}
	value, err := e.autonomousMaterialization(ctx, id)
	if err != nil {
		return AutonomousMaterialization{}, err
	}
	if value.LeaseID != lease.ID || value.ProvisionRequestID != request.ID || value.SchedulerLaneKey != intent.LaneKey {
		return AutonomousMaterialization{}, ErrConflict
	}
	return value, nil
}

func (e *AutopilotEngine) recordAutonomousPlan(ctx context.Context, id string, generation planning.GenerationResult) (AutonomousMaterialization, error) {
	if generation.Plan == nil || generation.PlanID <= 0 {
		return AutonomousMaterialization{}, ErrConflict
	}
	result, err := e.store.db.ExecContext(ctx, `UPDATE autonomous_command_materializations SET
		plan_id=?,delivery_lane_key=?,context_hash=?,updated_at=? WHERE id=? AND status='pending' AND plan_id=0`,
		generation.PlanID, generation.Plan.LaneKey, generation.Context.ContextHash, stamp(e.now()), id)
	if err != nil {
		return AutonomousMaterialization{}, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		current, readErr := e.autonomousMaterialization(ctx, id)
		if readErr != nil || current.PlanID == 0 {
			return AutonomousMaterialization{}, ErrConflict
		}
	}
	return e.autonomousMaterialization(ctx, id)
}

func (e *AutopilotEngine) blockMaterialization(ctx context.Context, intent Intent, lease Lease, request ProvisionRequest, reason string) error {
	value, err := e.ensureMaterialization(ctx, intent, lease, request)
	if err != nil {
		return err
	}
	_, err = e.store.db.ExecContext(ctx, `UPDATE autonomous_command_materializations SET status='blocked',reason_code=?,updated_at=? WHERE id=? AND status='pending'`, reason, stamp(e.now()), value.ID)
	return err
}

func (e *AutopilotEngine) supersedeMaterialization(ctx context.Context, value AutonomousMaterialization, lease Lease, reason string) error {
	now := e.now().UTC()
	tx, err := e.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE autonomous_command_materializations SET status='superseded',reason_code=?,updated_at=?,completed_at=? WHERE id=? AND status='pending'`, reason, stamp(now), stamp(now), value.ID); err != nil {
		return err
	}
	if err := setIntentStatusTx(ctx, tx, lease.IntentID, IntentClaimed, IntentSuperseded, now); err != nil && !errors.Is(err, ErrConflict) {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_lane_leases SET status='superseded',released_at=? WHERE id=? AND status='active'`, stamp(now), lease.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (e *AutopilotEngine) autonomousMaterialization(ctx context.Context, id string) (AutonomousMaterialization, error) {
	return scanAutonomousMaterialization(e.store.db.QueryRowContext(ctx, autonomousMaterializationSelect+` WHERE id=?`, id))
}

func (e *AutopilotEngine) Run(ctx context.Context, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.ReconcileAll(ctx); err != nil && report != nil {
				report(err)
			}
		}
	}
}

const autonomousMaterializationSelect = `SELECT id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,delivery_lane_key,plan_id,status,reason_code,workflow_command_id,delivery_command_id,context_hash,prompt_hash,created_at,updated_at,completed_at FROM autonomous_command_materializations`

func scanAutonomousMaterialization(row rowScanner) (AutonomousMaterialization, error) {
	var value AutonomousMaterialization
	var created, updated, completed string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IntentID, &value.LeaseID, &value.ProvisionRequestID,
		&value.SchedulerLaneKey, &value.DeliveryLaneKey, &value.PlanID, &value.Status, &value.ReasonCode,
		&value.WorkflowCommandID, &value.DeliveryCommandID, &value.ContextHash, &value.PromptHash,
		&created, &updated, &completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AutonomousMaterialization{}, ErrNotFound
		}
		return AutonomousMaterialization{}, err
	}
	var err error
	if value.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return AutonomousMaterialization{}, err
	}
	if value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return AutonomousMaterialization{}, err
	}
	if completed != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, completed)
		if parseErr != nil {
			return AutonomousMaterialization{}, parseErr
		}
		value.CompletedAt = &parsed
	}
	return value, nil
}

func deterministicAutopilotID(projectID int64, intentID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", projectID, intentID)))
	return "autocmd-" + hex.EncodeToString(sum[:16])
}

func hashAutopilotText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
