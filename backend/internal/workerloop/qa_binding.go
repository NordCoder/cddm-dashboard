package workerloop

import (
	"context"
	"database/sql"
	"time"
)

type QABindingRetirer struct {
	db  *sql.DB
	now func() time.Time
}

func NewQABindingRetirer(db *sql.DB) *QABindingRetirer {
	return &QABindingRetirer{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (r *QABindingRetirer) RetireAcceptedTerminalQA(ctx context.Context, projectID int64) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT d.binding_id,d.binding_version
		FROM workflow_commands c
		JOIN workflow_results result
		  ON result.project_id=c.project_id
		 AND result.asserted_command_id=c.id
		 AND result.validation_status='accepted'
		JOIN workflow_delivery_links link ON link.workflow_command_id=c.id
		JOIN delivery_commands d ON d.id=link.delivery_command_id
		WHERE c.project_id=?
		  AND c.role='qa'
		  AND c.status IN ('completed','blocked','inconclusive','failed','ambiguous')
	`, projectID)
	if err != nil {
		return err
	}
	type captured struct {
		id      string
		version int64
	}
	values := make([]captured, 0)
	for rows.Next() {
		var value captured
		if err := rows.Scan(&value.id, &value.version); err != nil {
			rows.Close()
			return err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	now := r.now().UTC().Format(time.RFC3339Nano)
	for _, value := range values {
		if _, err := r.db.ExecContext(ctx, `UPDATE browser_lane_bindings SET enabled=0,binding_version=binding_version+1,updated_at=? WHERE binding_id=? AND binding_version=? AND enabled=1`, now, value.id, value.version); err != nil {
			return err
		}
	}
	return nil
}
