package workerloop

import "context"

func (s *Store) ResultsForProject(ctx context.Context, projectID int64) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx, resultSelect+` WHERE project_id=? ORDER BY github_comment_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]Result, 0)
	for rows.Next() {
		value, scanErr := scanResult(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		results = append(results, value)
	}
	return results, rows.Err()
}

func (s *Store) ResultsForIssue(ctx context.Context, projectID int64, issueNumber int) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx, resultSelect+` WHERE project_id=? AND issue_number=? ORDER BY github_comment_id`, projectID, issueNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]Result, 0)
	for rows.Next() {
		value, scanErr := scanResult(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		results = append(results, value)
	}
	return results, rows.Err()
}
