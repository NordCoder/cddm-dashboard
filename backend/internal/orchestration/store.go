package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("orchestration record not found")
	ErrConflict = errors.New("orchestration record conflict")
)

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,200}$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	fullSHA           = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) CreateIntent(ctx context.Context, input IntentInput) (Intent, error) {
	values, err := s.CreateBatch(ctx, nil, []IntentInput{input})
	if err != nil {
		return Intent{}, err
	}
	return values[0], nil
}

// CreateBatch atomically creates an optional Wave and all supplied Intents.
// Existing byte-equivalent identities are returned idempotently; any mismatch
// aborts the complete batch.
func (s *Store) CreateBatch(ctx context.Context, wave *WaveInput, inputs []IntentInput) ([]Intent, error) {
	if len(inputs) == 0 && wave == nil {
		return nil, fmt.Errorf("orchestration batch is empty")
	}
	for index := range inputs {
		inputs[index] = normalizeIntentInput(inputs[index])
		if err := validateIntentInput(inputs[index]); err != nil {
			return nil, fmt.Errorf("intent %d: %w", index, err)
		}
	}
	if wave != nil {
		normalized := normalizeWaveInput(*wave)
		if err := validateWaveInput(normalized); err != nil {
			return nil, err
		}
		wave = &normalized
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin orchestration batch: %w", err)
	}
	defer tx.Rollback()

	if wave != nil {
		if _, err := s.createWaveTx(ctx, tx, *wave); err != nil {
			return nil, err
		}
	}
	created := make([]Intent, 0, len(inputs))
	for _, input := range inputs {
		value, err := s.createIntentTx(ctx, tx, input)
		if err != nil {
			return nil, err
		}
		created = append(created, value)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit orchestration batch: %w", err)
	}
	return created, nil
}

func (s *Store) CreateWave(ctx context.Context, input WaveInput) (Wave, error) {
	input = normalizeWaveInput(input)
	if err := validateWaveInput(input); err != nil {
		return Wave{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Wave{}, fmt.Errorf("begin Wave create: %w", err)
	}
	defer tx.Rollback()
	value, err := s.createWaveTx(ctx, tx, input)
	if err != nil {
		return Wave{}, err
	}
	if err := tx.Commit(); err != nil {
		return Wave{}, fmt.Errorf("commit Wave create: %w", err)
	}
	return value, nil
}

func (s *Store) Intent(ctx context.Context, id string) (Intent, error) {
	return scanIntent(s.db.QueryRowContext(ctx, intentSelect+` WHERE id=?`, strings.TrimSpace(id)))
}

func (s *Store) ListIntents(ctx context.Context, projectID int64, status string) ([]Intent, error) {
	query := intentSelect + ` WHERE project_id=?`
	args := []any{projectID}
	if strings.TrimSpace(status) != "" {
		query += ` AND status=?`
		args = append(args, strings.TrimSpace(status))
	}
	query += ` ORDER BY priority,created_at,id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list Workflow Intents: %w", err)
	}
	defer rows.Close()
	values := make([]Intent, 0)
	for rows.Next() {
		value, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) Wave(ctx context.Context, projectID int64, waveID string) (Wave, error) {
	value, err := scanWave(s.db.QueryRowContext(ctx, waveSelect+` WHERE project_id=? AND wave_id=?`, projectID, strings.TrimSpace(waveID)))
	if err != nil {
		return Wave{}, err
	}
	issues, err := s.waveIssues(ctx, s.db, projectID, value.WaveID)
	if err != nil {
		return Wave{}, err
	}
	value.Issues = issues
	return value, nil
}

func (s *Store) ListWaves(ctx context.Context, projectID int64) ([]Wave, error) {
	rows, err := s.db.QueryContext(ctx, waveSelect+` WHERE project_id=? ORDER BY created_at,wave_id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list Waves: %w", err)
	}
	defer rows.Close()
	values := make([]Wave, 0)
	for rows.Next() {
		value, err := scanWave(rows)
		if err != nil {
			return nil, err
		}
		issues, err := s.waveIssues(ctx, s.db, projectID, value.WaveID)
		if err != nil {
			return nil, err
		}
		value.Issues = issues
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) SetIntentStatus(ctx context.Context, id, status string) (Intent, error) {
	status = strings.TrimSpace(status)
	if !validIntentStatus(status) {
		return Intent{}, fmt.Errorf("invalid Workflow Intent status %q", status)
	}
	now := stamp(s.now())
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_intents SET status=?,updated_at=? WHERE id=?`, status, now, strings.TrimSpace(id))
	if err != nil {
		return Intent{}, fmt.Errorf("update Workflow Intent: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return Intent{}, ErrNotFound
	}
	return s.Intent(ctx, id)
}

func (s *Store) SetWaveStatus(ctx context.Context, projectID int64, waveID, status string) (Wave, error) {
	status = strings.TrimSpace(status)
	if !validWaveStatus(status) {
		return Wave{}, fmt.Errorf("invalid Wave status %q", status)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_waves SET status=?,updated_at=? WHERE project_id=? AND wave_id=?`, status, stamp(s.now()), projectID, strings.TrimSpace(waveID))
	if err != nil {
		return Wave{}, fmt.Errorf("update Wave: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return Wave{}, ErrNotFound
	}
	return s.Wave(ctx, projectID, waveID)
}

func (s *Store) createIntentTx(ctx context.Context, tx *sql.Tx, input IntentInput) (Intent, error) {
	if existing, err := intentBySource(ctx, tx, input.ProjectID, input.SourceCommandID, input.ActionID); err == nil {
		if sameIntentInput(existing, input) {
			return existing, nil
		}
		return Intent{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return Intent{}, err
	}
	if _, err := scanIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE id=?`, input.ID)); err == nil {
		return Intent{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return Intent{}, err
	}

	now := stamp(s.now())
	_, err := tx.ExecContext(ctx, `INSERT INTO workflow_intents (
		id,project_id,source_result_comment_id,source_command_id,action_id,action_type,repository,
		issue_number,role,pr_number,expected_head,expected_previous_head,reason_code,decision_category,
		wave_id,priority,lane_key,status,created_at,updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		input.ID, input.ProjectID, input.SourceResultCommentID, input.SourceCommandID, input.ActionID,
		input.ActionType, input.Repository, input.IssueNumber, input.Role, input.PRNumber, input.ExpectedHead,
		input.ExpectedPreviousHead, input.ReasonCode, input.DecisionCategory, input.WaveID, input.Priority,
		input.LaneKey, input.Status, now, now)
	if err != nil {
		return Intent{}, fmt.Errorf("create Workflow Intent: %w", err)
	}
	return scanIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE id=?`, input.ID))
}

func (s *Store) createWaveTx(ctx context.Context, tx *sql.Tx, input WaveInput) (Wave, error) {
	if existing, err := scanWave(tx.QueryRowContext(ctx, waveSelect+` WHERE project_id=? AND wave_id=?`, input.ProjectID, input.WaveID)); err == nil {
		issues, issueErr := s.waveIssues(ctx, tx, input.ProjectID, input.WaveID)
		if issueErr != nil {
			return Wave{}, issueErr
		}
		existing.Issues = issues
		if sameWaveInput(existing, input) {
			return existing, nil
		}
		return Wave{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return Wave{}, err
	}

	now := stamp(s.now())
	if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_waves(project_id,wave_id,control_issue_number,source_command_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, input.ProjectID, input.WaveID, input.ControlIssueNumber, input.SourceCommandID, input.Status, now, now); err != nil {
		return Wave{}, fmt.Errorf("create Wave: %w", err)
	}
	for position, issue := range input.Issues {
		if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_wave_issues(project_id,wave_id,issue_number,position) VALUES(?,?,?,?)`, input.ProjectID, input.WaveID, issue, position); err != nil {
			return Wave{}, fmt.Errorf("create Wave membership: %w", err)
		}
	}
	value, err := scanWave(tx.QueryRowContext(ctx, waveSelect+` WHERE project_id=? AND wave_id=?`, input.ProjectID, input.WaveID))
	if err != nil {
		return Wave{}, err
	}
	value.Issues = append([]int(nil), input.Issues...)
	return value, nil
}

func intentBySource(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, projectID int64, commandID, actionID string) (Intent, error) {
	return scanIntent(queryer.QueryRowContext(ctx, intentSelect+` WHERE project_id=? AND source_command_id=? AND action_id=?`, projectID, commandID, actionID))
}

func (s *Store) waveIssues(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, projectID int64, waveID string) ([]int, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT issue_number FROM workflow_wave_issues WHERE project_id=? AND wave_id=? ORDER BY position`, projectID, waveID)
	if err != nil {
		return nil, fmt.Errorf("read Wave membership: %w", err)
	}
	defer rows.Close()
	issues := make([]int, 0)
	for rows.Next() {
		var issue int
		if err := rows.Scan(&issue); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

const intentSelect = `SELECT id,project_id,source_result_comment_id,source_command_id,action_id,action_type,repository,issue_number,role,pr_number,expected_head,expected_previous_head,reason_code,decision_category,wave_id,priority,lane_key,status,created_at,updated_at FROM workflow_intents`

const waveSelect = `SELECT project_id,wave_id,control_issue_number,source_command_id,status,created_at,updated_at FROM workflow_waves`

type rowScanner interface {
	Scan(...any) error
}

func scanIntent(row rowScanner) (Intent, error) {
	var value Intent
	var created, updated string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.SourceResultCommentID, &value.SourceCommandID, &value.ActionID, &value.ActionType, &value.Repository, &value.IssueNumber, &value.Role, &value.PRNumber, &value.ExpectedHead, &value.ExpectedPreviousHead, &value.ReasonCode, &value.DecisionCategory, &value.WaveID, &value.Priority, &value.LaneKey, &value.Status, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Intent{}, ErrNotFound
		}
		return Intent{}, fmt.Errorf("scan Workflow Intent: %w", err)
	}
	var err error
	if value.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return Intent{}, err
	}
	if value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return Intent{}, err
	}
	return value, nil
}

func scanWave(row rowScanner) (Wave, error) {
	var value Wave
	var created, updated string
	if err := row.Scan(&value.ProjectID, &value.WaveID, &value.ControlIssueNumber, &value.SourceCommandID, &value.Status, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Wave{}, ErrNotFound
		}
		return Wave{}, fmt.Errorf("scan Wave: %w", err)
	}
	var err error
	if value.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return Wave{}, err
	}
	if value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return Wave{}, err
	}
	return value, nil
}

func normalizeIntentInput(input IntentInput) IntentInput {
	input.ID = strings.TrimSpace(input.ID)
	input.SourceCommandID = strings.TrimSpace(input.SourceCommandID)
	input.ActionID = strings.TrimSpace(input.ActionID)
	input.ActionType = strings.TrimSpace(input.ActionType)
	input.Repository = strings.TrimSpace(input.Repository)
	input.Role = strings.TrimSpace(input.Role)
	input.ExpectedHead = strings.TrimSpace(input.ExpectedHead)
	input.ExpectedPreviousHead = strings.TrimSpace(input.ExpectedPreviousHead)
	input.ReasonCode = strings.TrimSpace(input.ReasonCode)
	input.DecisionCategory = strings.TrimSpace(input.DecisionCategory)
	input.WaveID = strings.TrimSpace(input.WaveID)
	input.LaneKey = strings.TrimSpace(input.LaneKey)
	input.Status = strings.TrimSpace(input.Status)
	return input
}

func normalizeWaveInput(input WaveInput) WaveInput {
	input.WaveID = strings.TrimSpace(input.WaveID)
	input.SourceCommandID = strings.TrimSpace(input.SourceCommandID)
	input.Status = strings.TrimSpace(input.Status)
	input.Issues = append([]int(nil), input.Issues...)
	return input
}

func validateIntentInput(input IntentInput) error {
	if !identifierPattern.MatchString(input.ID) || input.ProjectID <= 0 || input.SourceResultCommentID <= 0 || !identifierPattern.MatchString(input.SourceCommandID) || !identifierPattern.MatchString(input.ActionID) {
		return fmt.Errorf("invalid Workflow Intent identity")
	}
	if !validAction(input.ActionType) || !repositoryPattern.MatchString(input.Repository) || input.IssueNumber < 0 || input.PRNumber < 0 {
		return fmt.Errorf("invalid Workflow Intent target")
	}
	if input.Role != "" && input.Role != "lead" && input.Role != "implementor" && input.Role != "qa" {
		return fmt.Errorf("invalid Workflow Intent role")
	}
	if input.ExpectedHead != "" && !fullSHA.MatchString(input.ExpectedHead) {
		return fmt.Errorf("expected Head must be empty or a full SHA")
	}
	if input.ExpectedPreviousHead != "" && !fullSHA.MatchString(input.ExpectedPreviousHead) {
		return fmt.Errorf("expected previous Head must be empty or a full SHA")
	}
	if input.WaveID != "" && !identifierPattern.MatchString(input.WaveID) {
		return fmt.Errorf("invalid Workflow Intent Wave identity")
	}
	if input.Priority < 0 || !validIntentStatus(input.Status) {
		return fmt.Errorf("invalid Workflow Intent priority or status")
	}
	return nil
}

func validateWaveInput(input WaveInput) error {
	if input.ProjectID <= 0 || !identifierPattern.MatchString(input.WaveID) || input.ControlIssueNumber <= 0 || !identifierPattern.MatchString(input.SourceCommandID) || !validWaveStatus(input.Status) {
		return fmt.Errorf("invalid Wave identity")
	}
	seen := make(map[int]struct{}, len(input.Issues))
	for _, issue := range input.Issues {
		if issue <= 0 {
			return fmt.Errorf("Wave Issue must be positive")
		}
		if _, exists := seen[issue]; exists {
			return fmt.Errorf("duplicate Wave Issue %d", issue)
		}
		seen[issue] = struct{}{}
	}
	return nil
}

func sameIntentInput(value Intent, input IntentInput) bool {
	return value.ID == input.ID && value.ProjectID == input.ProjectID && value.SourceResultCommentID == input.SourceResultCommentID && value.SourceCommandID == input.SourceCommandID && value.ActionID == input.ActionID && value.ActionType == input.ActionType && value.Repository == input.Repository && value.IssueNumber == input.IssueNumber && value.Role == input.Role && value.PRNumber == input.PRNumber && value.ExpectedHead == input.ExpectedHead && value.ExpectedPreviousHead == input.ExpectedPreviousHead && value.ReasonCode == input.ReasonCode && value.DecisionCategory == input.DecisionCategory && value.WaveID == input.WaveID && value.Priority == input.Priority && value.LaneKey == input.LaneKey && value.Status == input.Status
}

func sameWaveInput(value Wave, input WaveInput) bool {
	if value.ProjectID != input.ProjectID || value.WaveID != input.WaveID || value.ControlIssueNumber != input.ControlIssueNumber || value.SourceCommandID != input.SourceCommandID || value.Status != input.Status || len(value.Issues) != len(input.Issues) {
		return false
	}
	for index := range value.Issues {
		if value.Issues[index] != input.Issues[index] {
			return false
		}
	}
	return true
}

func validAction(value string) bool {
	switch value {
	case ActionDispatch, ActionCorrect, ActionPlanNextWave, ActionMerge, ActionHold, ActionOwnerRequired:
		return true
	default:
		return false
	}
}

func validIntentStatus(value string) bool {
	switch value {
	case IntentPending, IntentBlocked, IntentClaimed, IntentCompleted, IntentSuperseded, IntentRejected, IntentAmbiguous:
		return true
	default:
		return false
	}
}

func validWaveStatus(value string) bool {
	switch value {
	case WavePlanned, WaveActive, WaveWaiting, WaveCompleted, WaveBlocked, WaveSuperseded:
		return true
	default:
		return false
	}
}

func stamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
