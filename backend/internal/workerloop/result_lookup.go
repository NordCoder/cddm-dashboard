package workerloop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) ResultByComment(ctx context.Context, projectID, commentID int64) (Result, error) {
	result, err := scanResult(s.db.QueryRowContext(ctx, resultSelect+` WHERE project_id=? AND github_comment_id=?`, projectID, commentID))
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, fmt.Errorf("read workflow result: %w", err)
	}
	return result, nil
}
