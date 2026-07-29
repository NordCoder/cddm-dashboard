package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	MaterializationMaterialized = "materialized"
	MaterializationSkipped      = "skipped"
	MaterializationBlocked      = "blocked"
	MaterializationAmbiguous    = "ambiguous"
)

type Materialization struct {
	ProjectID             int64     `json:"project_id"`
	SourceResultCommentID int64     `json:"source_result_comment_id"`
	SourceCommandID       string    `json:"source_command_id"`
	PayloadHash           string    `json:"payload_hash"`
	Status                string    `json:"status"`
	ReasonCode            string    `json:"reason_code,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type MaterializationInput struct {
	ProjectID             int64
	SourceResultCommentID int64
	SourceCommandID       string
	PayloadHash           string
	Status                string
	ReasonCode            string
}

func (s *Store) Materialization(ctx context.Context, projectID, commentID int64) (Materialization, error) {
	return scanMaterialization(s.db.QueryRowContext(ctx, materializationSelect+` WHERE project_id=? AND source_result_comment_id=?`, projectID, commentID))
}

func (s *Store) RecordMaterialization(ctx context.Context, input MaterializationInput) (Materialization, error) {
	input = normalizeMaterializationInput(input)
	if err := validateMaterializationInput(input); err != nil {
		return Materialization{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Materialization{}, fmt.Errorf("begin materialization outcome: %w", err)
	}
	defer tx.Rollback()
	value, err := s.recordMaterializationTx(ctx, tx, input, false)
	if err != nil {
		return Materialization{}, err
	}
	if err := tx.Commit(); err != nil {
		return Materialization{}, fmt.Errorf("commit materialization outcome: %w", err)
	}
	return value, nil
}

func (s *Store) MaterializeBatch(ctx context.Context, outcome MaterializationInput, wave *WaveInput, intents []IntentInput) (Materialization, []Intent, error) {
	outcome = normalizeMaterializationInput(outcome)
	outcome.Status = MaterializationMaterialized
	if err := validateMaterializationInput(outcome); err != nil {
		return Materialization{}, nil, err
	}
	for index := range intents {
		intents[index] = normalizeIntentInput(intents[index])
		if err := validateIntentInput(intents[index]); err != nil {
			return Materialization{}, nil, fmt.Errorf("intent %d: %w", index, err)
		}
		if intents[index].ProjectID != outcome.ProjectID || intents[index].SourceResultCommentID != outcome.SourceResultCommentID || intents[index].SourceCommandID != outcome.SourceCommandID {
			return Materialization{}, nil, fmt.Errorf("materialization batch source mismatch")
		}
	}
	if len(intents) == 0 {
		return Materialization{}, nil, fmt.Errorf("materialized batch requires intents")
	}
	if wave != nil {
		normalized := normalizeWaveInput(*wave)
		if err := validateWaveInput(normalized); err != nil {
			return Materialization{}, nil, err
		}
		if normalized.ProjectID != outcome.ProjectID || normalized.SourceCommandID != outcome.SourceCommandID {
			return Materialization{}, nil, fmt.Errorf("materialization Wave source mismatch")
		}
		wave = &normalized
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Materialization{}, nil, fmt.Errorf("begin materialization batch: %w", err)
	}
	defer tx.Rollback()

	existing, err := materializationTx(ctx, tx, outcome.ProjectID, outcome.SourceResultCommentID)
	if err == nil {
		if existing.PayloadHash != outcome.PayloadHash || existing.SourceCommandID != outcome.SourceCommandID {
			return Materialization{}, nil, ErrConflict
		}
		if existing.Status == MaterializationMaterialized {
			values, readErr := s.intentsBySourceTx(ctx, tx, outcome.ProjectID, outcome.SourceResultCommentID)
			if readErr != nil {
				return Materialization{}, nil, readErr
			}
			if err := tx.Commit(); err != nil {
				return Materialization{}, nil, fmt.Errorf("commit idempotent materialization: %w", err)
			}
			return existing, values, nil
		}
	} else if !errors.Is(err, ErrNotFound) {
		return Materialization{}, nil, err
	}

	if wave != nil {
		if _, err := s.createWaveTx(ctx, tx, *wave); err != nil {
			return Materialization{}, nil, err
		}
	}
	created := make([]Intent, 0, len(intents))
	for _, input := range intents {
		value, err := s.createIntentTx(ctx, tx, input)
		if err != nil {
			return Materialization{}, nil, err
		}
		created = append(created, value)
	}
	value, err := s.recordMaterializationTx(ctx, tx, outcome, true)
	if err != nil {
		return Materialization{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return Materialization{}, nil, fmt.Errorf("commit materialization batch: %w", err)
	}
	return value, created, nil
}

func (s *Store) MarkMaterializationAmbiguous(ctx context.Context, projectID, commentID int64, payloadHash, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ambiguous materialization: %w", err)
	}
	defer tx.Rollback()
	value, err := materializationTx(ctx, tx, projectID, commentID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if payloadHash != "" && value.PayloadHash == payloadHash && value.Status == MaterializationAmbiguous {
		return tx.Commit()
	}
	now := stamp(s.now())
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_materializations SET status='ambiguous',reason_code=?,updated_at=? WHERE project_id=? AND source_result_comment_id=?`, reason, now, projectID, commentID); err != nil {
		return fmt.Errorf("mark materialization ambiguous: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_intents SET status='ambiguous',updated_at=? WHERE project_id=? AND source_result_comment_id=? AND status NOT IN ('completed','superseded')`, now, projectID, commentID); err != nil {
		return fmt.Errorf("mark materialized intents ambiguous: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ambiguous materialization: %w", err)
	}
	return nil
}

func (s *Store) recordMaterializationTx(ctx context.Context, tx *sql.Tx, input MaterializationInput, allowPromote bool) (Materialization, error) {
	existing, err := materializationTx(ctx, tx, input.ProjectID, input.SourceResultCommentID)
	if err == nil {
		if existing.PayloadHash != input.PayloadHash || existing.SourceCommandID != input.SourceCommandID {
			return Materialization{}, ErrConflict
		}
		if existing.Status == input.Status && existing.ReasonCode == input.ReasonCode {
			return existing, nil
		}
		if !allowPromote || existing.Status == MaterializationMaterialized || existing.Status == MaterializationAmbiguous {
			return Materialization{}, ErrConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE workflow_materializations SET status=?,reason_code=?,updated_at=? WHERE project_id=? AND source_result_comment_id=?`, input.Status, input.ReasonCode, stamp(s.now()), input.ProjectID, input.SourceResultCommentID)
		if err != nil {
			return Materialization{}, fmt.Errorf("promote materialization outcome: %w", err)
		}
		return materializationTx(ctx, tx, input.ProjectID, input.SourceResultCommentID)
	}
	if !errors.Is(err, ErrNotFound) {
		return Materialization{}, err
	}
	now := stamp(s.now())
	_, err = tx.ExecContext(ctx, `INSERT INTO workflow_materializations(project_id,source_result_comment_id,source_command_id,payload_hash,status,reason_code,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, input.ProjectID, input.SourceResultCommentID, input.SourceCommandID, input.PayloadHash, input.Status, input.ReasonCode, now, now)
	if err != nil {
		return Materialization{}, fmt.Errorf("create materialization outcome: %w", err)
	}
	return materializationTx(ctx, tx, input.ProjectID, input.SourceResultCommentID)
}

func (s *Store) intentsBySourceTx(ctx context.Context, tx *sql.Tx, projectID, commentID int64) ([]Intent, error) {
	rows, err := tx.QueryContext(ctx, intentSelect+` WHERE project_id=? AND source_result_comment_id=? ORDER BY priority,created_at,id`, projectID, commentID)
	if err != nil {
		return nil, err
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

const materializationSelect = `SELECT project_id,source_result_comment_id,source_command_id,payload_hash,status,reason_code,created_at,updated_at FROM workflow_materializations`

func materializationTx(ctx context.Context, tx *sql.Tx, projectID, commentID int64) (Materialization, error) {
	return scanMaterialization(tx.QueryRowContext(ctx, materializationSelect+` WHERE project_id=? AND source_result_comment_id=?`, projectID, commentID))
}

func scanMaterialization(row rowScanner) (Materialization, error) {
	var value Materialization
	var created, updated string
	if err := row.Scan(&value.ProjectID, &value.SourceResultCommentID, &value.SourceCommandID, &value.PayloadHash, &value.Status, &value.ReasonCode, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Materialization{}, ErrNotFound
		}
		return Materialization{}, fmt.Errorf("scan materialization outcome: %w", err)
	}
	var err error
	value.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Materialization{}, err
	}
	value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return value, err
}

func normalizeMaterializationInput(input MaterializationInput) MaterializationInput {
	input.SourceCommandID = strings.TrimSpace(input.SourceCommandID)
	input.PayloadHash = strings.TrimSpace(input.PayloadHash)
	input.Status = strings.TrimSpace(input.Status)
	input.ReasonCode = strings.TrimSpace(input.ReasonCode)
	return input
}

func validateMaterializationInput(input MaterializationInput) error {
	if input.ProjectID <= 0 || input.SourceResultCommentID <= 0 || !identifierPattern.MatchString(input.SourceCommandID) || len(input.PayloadHash) != 64 {
		return fmt.Errorf("invalid materialization identity")
	}
	switch input.Status {
	case MaterializationMaterialized, MaterializationSkipped, MaterializationBlocked, MaterializationAmbiguous:
	default:
		return fmt.Errorf("invalid materialization status %q", input.Status)
	}
	return nil
}
