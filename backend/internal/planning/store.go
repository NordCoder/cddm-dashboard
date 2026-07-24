package planning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrPlanNotFound = errors.New("prompt plan not found")

type InvocationRecord struct {
	Attempt       int
	Runtime       string
	Provider      string
	Model         string
	Agent         string
	Mode          string
	Latency       time.Duration
	Status        string
	ErrorCategory string
	Usage         Usage
	Output        string
	StartedAt     time.Time
	CompletedAt   time.Time
}

type GenerationRecord struct {
	ID              int64
	ProjectID       int64
	IssueNumber     int
	Mode            string
	Status          string
	Context         PromptContext
	ContextJSON     []byte
	Plan            *PromptPlan
	PlanJSON        []byte
	Decision        PolicyDecision
	DecisionHistory []PolicyAuditDecision
	Invocations     []InvocationRecord
	CreatedAt       time.Time
}

type AuditStore struct {
	db *sql.DB
}

func NewAuditStore(db *sql.DB) *AuditStore { return &AuditStore{db: db} }

func (s *AuditStore) Save(ctx context.Context, record GenerationRecord) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin planning audit transaction: %w", err)
	}
	defer tx.Rollback()

	createdAt := record.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO planning_generations (
			project_id, issue_number, context_version, context_hash, context_json,
			mode, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ProjectID, record.IssueNumber, record.Context.Version, record.Context.ContextHash,
		string(record.ContextJSON), record.Mode, record.Status, createdAt.Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("insert planning generation: %w", err)
	}
	generationID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read planning generation id: %w", err)
	}

	for _, invocation := range record.Invocations {
		output := redactText(invocation.Output)
		if len(output) > 1<<20 {
			output = truncateUTF8(output, 1<<20)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO model_invocations (
				generation_id, attempt, runtime, provider, model, agent, mode,
				latency_ms, status, error_category, input_tokens, output_tokens,
				reasoning_tokens, cache_read_tokens, cache_write_tokens, cost_micros,
				response_hash, response_text, started_at, completed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, generationID, invocation.Attempt, invocation.Runtime, invocation.Provider, invocation.Model,
			invocation.Agent, invocation.Mode, invocation.Latency.Milliseconds(), invocation.Status,
			invocation.ErrorCategory, invocation.Usage.InputTokens, invocation.Usage.OutputTokens,
			invocation.Usage.ReasoningTokens, invocation.Usage.CacheReadTokens,
			invocation.Usage.CacheWriteTokens, invocation.Usage.CostMicros, hashBytes([]byte(output)), output,
			invocation.StartedAt.UTC().Format(time.RFC3339Nano), invocation.CompletedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return 0, fmt.Errorf("insert model invocation: %w", err)
		}
	}

	if record.Plan != nil {
		planHash := hashBytes(record.PlanJSON)
		promptHash := hashBytes([]byte(record.Plan.Prompt))
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO prompt_plans (
				generation_id, schema_version, plan_hash, prompt_hash, source,
				runtime, provider, model, agent, mode, plan_json, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, generationID, record.Plan.Version, planHash, promptHash, record.Plan.Source.Kind,
			record.Plan.Source.Runtime, record.Plan.Source.Provider, record.Plan.Source.Model,
			record.Plan.Source.Agent, record.Plan.Source.Mode, string(record.PlanJSON), createdAt.Format(time.RFC3339Nano)); err != nil {
			return 0, fmt.Errorf("insert prompt plan: %w", err)
		}
	}

	decisions := append([]PolicyAuditDecision(nil), record.DecisionHistory...)
	decisions = append(decisions, PolicyAuditDecision{Attempt: -1, Final: true, Decision: record.Decision})
	for order, auditDecision := range decisions {
		violations, err := json.Marshal(auditDecision.Decision.Violations)
		if err != nil {
			return 0, fmt.Errorf("encode policy violations: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO policy_decisions (
				generation_id, decision_order, attempt, is_final, status, context_hash,
				plan_hash, violations_json, decided_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, generationID, order, auditDecision.Attempt, boolInteger(auditDecision.Final),
			auditDecision.Decision.Status, auditDecision.Decision.ContextHash,
			auditDecision.Decision.PlanHash, string(violations),
			auditDecision.Decision.DecidedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return 0, fmt.Errorf("insert policy decision: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit planning audit transaction: %w", err)
	}
	return generationID, nil
}

func (s *AuditStore) Latest(ctx context.Context, projectID int64, issueNumber int) (GenerationRecord, error) {
	return s.readOne(ctx, `WHERE g.project_id = ? AND g.issue_number = ? ORDER BY g.id DESC LIMIT 1`, projectID, issueNumber)
}

func (s *AuditStore) Get(ctx context.Context, projectID int64, issueNumber int, generationID int64) (GenerationRecord, error) {
	return s.readOne(ctx, `WHERE g.project_id = ? AND g.issue_number = ? AND g.id = ?`, projectID, issueNumber, generationID)
}

func (s *AuditStore) History(ctx context.Context, projectID int64, issueNumber, limit int) ([]GenerationRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id
		FROM planning_generations g
		WHERE g.project_id = ? AND g.issue_number = ?
		ORDER BY g.id DESC LIMIT ?
	`, projectID, issueNumber, limit)
	if err != nil {
		return nil, fmt.Errorf("list planning history: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan planning history id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list planning history rows: %w", err)
	}
	result := make([]GenerationRecord, 0, len(ids))
	for _, id := range ids {
		record, err := s.Get(ctx, projectID, issueNumber, id)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}

func (s *AuditStore) readOne(ctx context.Context, where string, args ...any) (GenerationRecord, error) {
	query := `
		SELECT g.id, g.project_id, g.issue_number, g.mode, g.status, g.context_json, g.created_at,
			p.plan_json, d.status, d.context_hash, d.plan_hash, d.violations_json, d.decided_at
		FROM planning_generations g
		LEFT JOIN prompt_plans p ON p.generation_id = g.id
		JOIN policy_decisions d ON d.generation_id = g.id AND d.is_final = 1
		` + where
	var record GenerationRecord
	var contextJSON, createdAt, decisionStatus, decisionContextHash, decisionPlanHash, violationsJSON, decidedAt string
	var planJSON sql.NullString
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&record.ID, &record.ProjectID, &record.IssueNumber, &record.Mode, &record.Status,
		&contextJSON, &createdAt, &planJSON, &decisionStatus, &decisionContextHash,
		&decisionPlanHash, &violationsJSON, &decidedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return GenerationRecord{}, ErrPlanNotFound
	} else if err != nil {
		return GenerationRecord{}, fmt.Errorf("read planning generation: %w", err)
	}
	record.ContextJSON = []byte(contextJSON)
	if err := json.Unmarshal(record.ContextJSON, &record.Context); err != nil {
		return GenerationRecord{}, fmt.Errorf("decode stored PromptContext: %w", err)
	}
	if planJSON.Valid {
		record.PlanJSON = []byte(planJSON.String)
		var plan PromptPlan
		if err := json.Unmarshal(record.PlanJSON, &plan); err != nil {
			return GenerationRecord{}, fmt.Errorf("decode stored PromptPlan: %w", err)
		}
		record.Plan = &plan
	}
	if err := json.Unmarshal([]byte(violationsJSON), &record.Decision.Violations); err != nil {
		return GenerationRecord{}, fmt.Errorf("decode stored policy violations: %w", err)
	}
	record.Decision.Status = decisionStatus
	record.Decision.ContextHash = decisionContextHash
	record.Decision.PlanHash = decisionPlanHash
	var err error
	if record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return GenerationRecord{}, fmt.Errorf("parse generation timestamp: %w", err)
	}
	if record.Decision.DecidedAt, err = time.Parse(time.RFC3339Nano, decidedAt); err != nil {
		return GenerationRecord{}, fmt.Errorf("parse policy timestamp: %w", err)
	}
	record.Invocations, err = s.readInvocations(ctx, record.ID)
	if err != nil {
		return GenerationRecord{}, err
	}
	return record, nil
}

func (s *AuditStore) readInvocations(ctx context.Context, generationID int64) ([]InvocationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT attempt, runtime, provider, model, agent, mode, latency_ms, status,
			error_category, input_tokens, output_tokens, reasoning_tokens,
			cache_read_tokens, cache_write_tokens, cost_micros, response_text,
			started_at, completed_at
		FROM model_invocations WHERE generation_id = ? ORDER BY attempt
	`, generationID)
	if err != nil {
		return nil, fmt.Errorf("read model invocations: %w", err)
	}
	defer rows.Close()
	result := make([]InvocationRecord, 0)
	for rows.Next() {
		var item InvocationRecord
		var latencyMS int64
		var startedAt, completedAt string
		if err := rows.Scan(&item.Attempt, &item.Runtime, &item.Provider, &item.Model, &item.Agent,
			&item.Mode, &latencyMS, &item.Status, &item.ErrorCategory, &item.Usage.InputTokens,
			&item.Usage.OutputTokens, &item.Usage.ReasoningTokens, &item.Usage.CacheReadTokens,
			&item.Usage.CacheWriteTokens, &item.Usage.CostMicros, &item.Output, &startedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("scan model invocation: %w", err)
		}
		item.Latency = time.Duration(latencyMS) * time.Millisecond
		if item.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt); err != nil {
			return nil, fmt.Errorf("parse invocation start: %w", err)
		}
		if item.CompletedAt, err = time.Parse(time.RFC3339Nano, completedAt); err != nil {
			return nil, fmt.Errorf("parse invocation completion: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
