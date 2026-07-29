package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

type TypedAutonomousPlanner interface {
	GenerateAutonomousIntent(context.Context, int64, int, string, string, string) (planning.GenerationResult, error)
	Get(context.Context, int64, int, int64) (planning.GenerationResult, error)
}

type MergeFactsReader interface {
	ReadMergeFacts(context.Context, string, string, int, int) (supervisor.MergeFacts, error)
}

// MergeAutopilotEngine owns only typed Lead merge and next-Wave Intents. The
// M12-C1 Autopilot remains the sole owner of dispatch/correction materialization
// and the scheduler remains the sole owner of lane claims.
type MergeAutopilotEngine struct {
	store        *Store
	scheduler    *Scheduler
	provisioning *ProvisioningService
	planner      TypedAutonomousPlanner
	delivery     AutonomousDelivery
	bindings     delivery.BindingResolver
	snapshots    *supervisor.Store
	facts        MergeFactsReader
	now          func() time.Time
}

func NewMergeAutopilotEngine(
	store *Store,
	scheduler *Scheduler,
	provisioning *ProvisioningService,
	planner TypedAutonomousPlanner,
	deliveryService AutonomousDelivery,
	bindings delivery.BindingResolver,
	snapshots *supervisor.Store,
	facts MergeFactsReader,
) (*MergeAutopilotEngine, error) {
	if store == nil || scheduler == nil || provisioning == nil || planner == nil || deliveryService == nil || bindings == nil || snapshots == nil || facts == nil {
		return nil, fmt.Errorf("merge Autopilot requires orchestration, planning, delivery, binding, snapshot and GitHub fact services")
	}
	return &MergeAutopilotEngine{
		store: store, scheduler: scheduler, provisioning: provisioning, planner: planner,
		delivery: deliveryService, bindings: bindings, snapshots: snapshots, facts: facts,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (e *MergeAutopilotEngine) ReconcileAll(ctx context.Context) error {
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
			return fmt.Errorf("reconcile merge Autopilot project %d: %w", project.ID, err)
		}
	}
	return nil
}

func (e *MergeAutopilotEngine) ReconcileProject(ctx context.Context, snapshot supervisor.ProjectSnapshot) error {
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
		if lease.Status != LeaseActive || lease.LeaseOwner != autopilotLeaseOwner {
			continue
		}
		intent, err := e.store.Intent(ctx, lease.IntentID)
		if err != nil {
			return err
		}
		if intent.ActionType != ActionMerge && intent.ActionType != ActionPlanNextWave {
			continue
		}
		if err := e.reconcileLease(ctx, snapshot, intent, lease); err != nil {
			return err
		}
	}
	return nil
}

func (e *MergeAutopilotEngine) reconcileLease(ctx context.Context, snapshot supervisor.ProjectSnapshot, intent Intent, lease Lease) error {
	request, err := e.provisioning.Enqueue(ctx, EnqueueProvisioningInput{
		ProjectID: lease.ProjectID, LeaseID: lease.ID, LeaseOwner: lease.LeaseOwner, LeaseToken: lease.LeaseToken,
	})
	if err != nil {
		return err
	}
	if request.Status != ProvisionProvisioned {
		return nil
	}
	return e.materialize(ctx, snapshot, intent, lease, request)
}

func (e *MergeAutopilotEngine) materialize(ctx context.Context, snapshot supervisor.ProjectSnapshot, intent Intent, lease Lease, request ProvisionRequest) error {
	value, err := e.ensureMaterialization(ctx, intent, lease, request)
	if err != nil {
		return err
	}
	if value.Status == AutonomousMaterializationCompleted {
		return nil
	}
	if value.Status == AutonomousMaterializationMaterialized {
		return e.ensureMergeCycle(ctx, snapshot, intent, value)
	}
	if value.Status != AutonomousMaterializationPending {
		return nil
	}

	generation := planning.GenerationResult{}
	if value.PlanID > 0 {
		generation, err = e.planner.Get(ctx, intent.ProjectID, intent.IssueNumber, value.PlanID)
	} else {
		generation, err = e.planner.GenerateAutonomousIntent(
			ctx, intent.ProjectID, intent.IssueNumber, intent.ActionType, intent.Role, provisionExpectedHead(intent),
		)
		if err == nil {
			value, err = e.recordPlan(ctx, value.ID, generation)
		}
	}
	if err != nil {
		return e.supersede(ctx, value, lease, "route_or_head_changed")
	}
	if !typedPlanMatchesIntent(intent, generation) {
		return e.supersede(ctx, value, lease, "typed_autonomous_plan_invalid")
	}
	binding, err := e.bindings.Resolve(ctx, intent.ProjectID, intent.LaneKey)
	if err != nil {
		return err
	}
	if binding.BindingID != request.BoundBindingID || binding.BindingVersion != request.BoundBindingVersion || !binding.Ready {
		return e.supersede(ctx, value, lease, "provisioned_binding_changed")
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
		current, readErr := scanAutonomousMaterialization(e.store.db.QueryRowContext(ctx, autonomousMaterializationSelect+` WHERE id=?`, value.ID))
		if readErr != nil || current.WorkflowCommandID != workflowCommandID || current.DeliveryCommandID != deliveryCommand.ID {
			return ErrConflict
		}
		value = current
	} else {
		value.WorkflowCommandID = workflowCommandID
		value.DeliveryCommandID = deliveryCommand.ID
		value.Status = AutonomousMaterializationMaterialized
	}
	if _, err := e.store.db.ExecContext(ctx, `UPDATE delivery_commands SET authority_kind=?,authority_ref=? WHERE id=?`, delivery.AuthorityAutonomousIntent, intent.ID, deliveryCommand.ID); err != nil {
		return err
	}
	return e.ensureMergeCycle(ctx, snapshot, intent, value)
}

func (e *MergeAutopilotEngine) ensureMaterialization(ctx context.Context, intent Intent, lease Lease, request ProvisionRequest) (AutonomousMaterialization, error) {
	id := deterministicAutopilotID(intent.ProjectID, intent.ID)
	now := stamp(e.now())
	_, err := e.store.db.ExecContext(ctx, `INSERT INTO autonomous_command_materializations(
		id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,'pending',?,?) ON CONFLICT(project_id,intent_id) DO NOTHING`,
		id, intent.ProjectID, intent.ID, lease.ID, request.ID, intent.LaneKey, now, now)
	if err != nil {
		return AutonomousMaterialization{}, err
	}
	value, err := scanAutonomousMaterialization(e.store.db.QueryRowContext(ctx, autonomousMaterializationSelect+` WHERE id=?`, id))
	if err != nil {
		return AutonomousMaterialization{}, err
	}
	if value.LeaseID != lease.ID || value.ProvisionRequestID != request.ID || value.SchedulerLaneKey != intent.LaneKey {
		return AutonomousMaterialization{}, ErrConflict
	}
	return value, nil
}

func (e *MergeAutopilotEngine) recordPlan(ctx context.Context, id string, generation planning.GenerationResult) (AutonomousMaterialization, error) {
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
		current, readErr := scanAutonomousMaterialization(e.store.db.QueryRowContext(ctx, autonomousMaterializationSelect+` WHERE id=?`, id))
		if readErr != nil || current.PlanID == 0 {
			return AutonomousMaterialization{}, ErrConflict
		}
	}
	return scanAutonomousMaterialization(e.store.db.QueryRowContext(ctx, autonomousMaterializationSelect+` WHERE id=?`, id))
}

func (e *MergeAutopilotEngine) ensureMergeCycle(ctx context.Context, snapshot supervisor.ProjectSnapshot, intent Intent, value AutonomousMaterialization) error {
	if intent.ActionType != ActionMerge {
		return nil
	}
	baseRef, ok := exactMergeBase(snapshot, intent)
	if !ok || value.WorkflowCommandID == "" {
		return ErrConflict
	}
	id := deterministicMergeCycleID(intent.ProjectID, intent.ID)
	now := stamp(e.now())
	_, err := e.store.db.ExecContext(ctx, `INSERT INTO merge_cycle_readbacks(
		id,project_id,intent_id,workflow_command_id,repository,issue_number,pr_number,approved_head,expected_base_ref,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,'pending',?,?) ON CONFLICT(project_id,intent_id) DO NOTHING`,
		id, intent.ProjectID, intent.ID, value.WorkflowCommandID, intent.Repository, intent.IssueNumber,
		intent.PRNumber, intent.ExpectedHead, baseRef, now, now)
	if err != nil {
		return err
	}
	cycle, err := scanMergeCycle(e.store.db.QueryRowContext(ctx, mergeCycleSelect+` WHERE id=?`, id))
	if err != nil {
		return err
	}
	if cycle.ProjectID != intent.ProjectID || cycle.IntentID != intent.ID || cycle.WorkflowCommandID != value.WorkflowCommandID ||
		!strings.EqualFold(cycle.Repository, intent.Repository) || cycle.IssueNumber != intent.IssueNumber || cycle.PRNumber != intent.PRNumber ||
		cycle.ApprovedHead != intent.ExpectedHead || cycle.ExpectedBaseRef != baseRef {
		return ErrConflict
	}
	return nil
}

func (e *MergeAutopilotEngine) supersede(ctx context.Context, value AutonomousMaterialization, lease Lease, reason string) error {
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

func (e *MergeAutopilotEngine) Run(ctx context.Context, interval time.Duration, report func(error)) {
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

func typedPlanMatchesIntent(intent Intent, generation planning.GenerationResult) bool {
	if generation.Plan == nil || generation.PolicyDecision.Status != planning.StatusApproved || generation.Plan.TargetRole != intent.Role {
		return false
	}
	if generation.Plan.Action != generation.Context.Route.Action || generation.Plan.ExpectedHead != generation.Context.CurrentHead {
		return false
	}
	expectedHead := provisionExpectedHead(intent)
	if expectedHead != "" && generation.Context.CurrentHead != expectedHead {
		return false
	}
	switch intent.ActionType {
	case ActionMerge:
		return intent.Role == "lead" && intent.PRNumber > 0 && intent.ExpectedHead != "" &&
			(generation.Context.Route.Action == "dispatch" || generation.Context.Route.Action == "merge_gate")
	case ActionPlanNextWave:
		return intent.Role == "lead" && generation.Context.Route.Action == "dispatch"
	default:
		return false
	}
}

func exactMergeBase(snapshot supervisor.ProjectSnapshot, intent Intent) (string, bool) {
	for _, issue := range snapshot.Issues {
		if issue.Number != intent.IssueNumber {
			continue
		}
		for _, pull := range issue.PullRequests {
			if pull.Number == intent.PRNumber && pull.HeadSHA == intent.ExpectedHead && strings.EqualFold(pull.State, "open") && strings.TrimSpace(pull.BaseRef) != "" {
				return strings.TrimSpace(pull.BaseRef), true
			}
		}
	}
	return "", false
}
