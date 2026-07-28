package workerloop

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("workflow command not found")
	ErrConflict = errors.New("workflow command conflict")
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) CreateCommand(ctx context.Context, input CreateCommandInput) (Command, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.IdentityKey = strings.TrimSpace(input.IdentityKey)
	input.Role = strings.TrimSpace(input.Role)
	input.Action = strings.TrimSpace(input.Action)
	input.ResourceProfile = strings.TrimSpace(input.ResourceProfile)
	input.ContextHash = strings.TrimSpace(input.ContextHash)
	input.ExpectedHead = strings.TrimSpace(input.ExpectedHead)
	input.Status = strings.TrimSpace(input.Status)
	if input.ID == "" {
		input.ID = randomID()
	}
	if input.Status == "" {
		input.Status = CommandCreated
	}
	if input.ProjectID <= 0 || input.IssueNumber <= 0 || input.IdentityKey == "" || input.Action == "" || input.ResourceProfile == "" || input.ContextHash == "" {
		return Command{}, fmt.Errorf("project, issue, identity, action, resource profile and context hash are required")
	}
	if !commandID.MatchString(input.ID) || !validNextRole(input.Role) || !knownCommandStatus(input.Status) {
		return Command{}, fmt.Errorf("invalid workflow command identity, role or status")
	}
	if input.ExpectedHead != "" && !fullSHA.MatchString(input.ExpectedHead) {
		return Command{}, fmt.Errorf("expected Head must be empty or a full SHA")
	}

	if existing, err := s.commandByIdentity(ctx, input.ProjectID, input.IssueNumber, input.IdentityKey); err == nil {
		if sameCommandInput(existing, input) {
			return existing, nil
		}
		return Command{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return Command{}, err
	}

	now := s.now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO workflow_commands (id,project_id,issue_number,identity_key,role,action,resource_profile,context_hash,expected_head,status,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, input.ID, input.ProjectID, input.IssueNumber, input.IdentityKey, input.Role, input.Action, input.ResourceProfile, input.ContextHash, input.ExpectedHead, input.Status, stamp(now))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			if existing, readErr := s.commandByIdentity(ctx, input.ProjectID, input.IssueNumber, input.IdentityKey); readErr == nil && sameCommandInput(existing, input) {
				return existing, nil
			}
			return Command{}, ErrConflict
		}
		return Command{}, fmt.Errorf("create workflow command: %w", err)
	}
	return s.GetCommand(ctx, input.ID)
}

func (s *Store) GetCommand(ctx context.Context, id string) (Command, error) {
	var command Command
	var createdAt, completedAt string
	err := s.db.QueryRowContext(ctx, commandSelect+` WHERE id=?`, id).Scan(
		&command.ID, &command.ProjectID, &command.IssueNumber, &command.IdentityKey, &command.Role,
		&command.Action, &command.ResourceProfile, &command.ContextHash, &command.ExpectedHead,
		&command.Status, &createdAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Command{}, ErrNotFound
	}
	if err != nil {
		return Command{}, fmt.Errorf("read workflow command: %w", err)
	}
	command.CreatedAt = parseStamp(createdAt)
	if completedAt != "" {
		value := parseStamp(completedAt)
		command.CompletedAt = &value
	}
	return command, nil
}

func (s *Store) ListCommands(ctx context.Context, projectID int64, issueNumber int) ([]Command, error) {
	rows, err := s.db.QueryContext(ctx, commandSelect+` WHERE project_id=? AND issue_number=? ORDER BY created_at,id`, projectID, issueNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := make([]Command, 0)
	for rows.Next() {
		var command Command
		var createdAt, completedAt string
		if err := rows.Scan(&command.ID, &command.ProjectID, &command.IssueNumber, &command.IdentityKey, &command.Role, &command.Action, &command.ResourceProfile, &command.ContextHash, &command.ExpectedHead, &command.Status, &createdAt, &completedAt); err != nil {
			return nil, err
		}
		command.CreatedAt = parseStamp(createdAt)
		if completedAt != "" {
			value := parseStamp(completedAt)
			command.CompletedAt = &value
		}
		commands = append(commands, command)
	}
	return commands, rows.Err()
}

func (s *Store) SetCommandStatus(ctx context.Context, id, status string) (Command, error) {
	command, err := s.GetCommand(ctx, id)
	if err != nil {
		return Command{}, err
	}
	if command.Status == status {
		return command, nil
	}
	if !allowedTransition(command.Status, status) {
		return Command{}, ErrConflict
	}
	completedAt := ""
	if terminalCommandStatus(status) {
		completedAt = stamp(s.now().UTC())
	}
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_commands SET status=?,completed_at=? WHERE id=? AND status=?`, status, completedAt, id, command.Status)
	if err != nil {
		return Command{}, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return Command{}, ErrConflict
	}
	return s.GetCommand(ctx, id)
}

func (s *Store) UpsertResult(ctx context.Context, result Result) error {
	acceptedAt := ""
	if result.AcceptedAt != nil {
		acceptedAt = stamp(result.AcceptedAt.UTC())
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO workflow_results (project_id,github_comment_id,issue_number,asserted_command_id,role,result,payload_json,payload_hash,validation_status,validation_reason,accepted_at,observed_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id,github_comment_id) DO UPDATE SET issue_number=excluded.issue_number,asserted_command_id=excluded.asserted_command_id,role=excluded.role,result=excluded.result,payload_json=excluded.payload_json,payload_hash=excluded.payload_hash,validation_status=excluded.validation_status,validation_reason=excluded.validation_reason,accepted_at=excluded.accepted_at,observed_at=excluded.observed_at`, result.ProjectID, result.GitHubCommentID, result.IssueNumber, result.CommandID, result.Role, result.Result, string(result.Payload), result.PayloadHash, result.ValidationStatus, result.ValidationReason, acceptedAt, stamp(result.ObservedAt.UTC()))
	if err != nil {
		return fmt.Errorf("upsert workflow result: %w", err)
	}
	return nil
}

func (s *Store) DeleteResult(ctx context.Context, projectID, commentID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workflow_results WHERE project_id=? AND github_comment_id=?`, projectID, commentID)
	return err
}

func (s *Store) DeleteMissingResults(ctx context.Context, projectID int64, seen []int64) error {
	if len(seen) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM workflow_results WHERE project_id=?`, projectID)
		return err
	}
	sort.Slice(seen, func(i, j int) bool { return seen[i] < seen[j] })
	placeholders := make([]string, len(seen))
	args := make([]any, 0, len(seen)+1)
	args = append(args, projectID)
	for index, id := range seen {
		placeholders[index] = "?"
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM workflow_results WHERE project_id=? AND github_comment_id NOT IN (`+strings.Join(placeholders, ",")+`)`, args...)
	return err
}

func (s *Store) ResultsForCommand(ctx context.Context, projectID int64, commandID string) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx, resultSelect+` WHERE project_id=? AND asserted_command_id=? ORDER BY github_comment_id`, projectID, commandID)
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

func (s *Store) MarkCommandResultsAmbiguous(ctx context.Context, projectID int64, commandID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workflow_results SET validation_status='ambiguous',validation_reason='conflicting_terminal_results',accepted_at='' WHERE project_id=? AND asserted_command_id=? AND validation_status='accepted'`, projectID, commandID)
	return err
}

func (s *Store) commandByIdentity(ctx context.Context, projectID int64, issueNumber int, identity string) (Command, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM workflow_commands WHERE project_id=? AND issue_number=? AND identity_key=?`, projectID, issueNumber, identity).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Command{}, ErrNotFound
	}
	if err != nil {
		return Command{}, err
	}
	return s.GetCommand(ctx, id)
}

func scanResult(scanner interface{ Scan(...any) error }) (Result, error) {
	var result Result
	var payload, acceptedAt, observedAt string
	if err := scanner.Scan(&result.ProjectID, &result.GitHubCommentID, &result.IssueNumber, &result.CommandID, &result.Role, &result.Result, &payload, &result.PayloadHash, &result.ValidationStatus, &result.ValidationReason, &acceptedAt, &observedAt); err != nil {
		return Result{}, err
	}
	result.Payload = []byte(payload)
	result.ObservedAt = parseStamp(observedAt)
	if acceptedAt != "" {
		value := parseStamp(acceptedAt)
		result.AcceptedAt = &value
	}
	return result, nil
}

func sameCommandInput(command Command, input CreateCommandInput) bool {
	return command.ProjectID == input.ProjectID && command.IssueNumber == input.IssueNumber && command.IdentityKey == input.IdentityKey && command.Role == input.Role && command.Action == input.Action && command.ResourceProfile == input.ResourceProfile && command.ContextHash == input.ContextHash && command.ExpectedHead == input.ExpectedHead
}

func allowedTransition(from, to string) bool {
	if to == CommandAmbiguous && from != CommandAmbiguous && from != CommandSuperseded {
		return true
	}
	switch from {
	case CommandCreated:
		return to == CommandDeliveryPending || to == CommandAwaitingResult || to == CommandCompleted || to == CommandBlocked || to == CommandInconclusive || to == CommandFailed || to == CommandSuperseded
	case CommandDeliveryPending:
		return to == CommandAwaitingResult || to == CommandCompleted || to == CommandBlocked || to == CommandInconclusive || to == CommandFailed || to == CommandSuperseded
	case CommandAwaitingResult:
		return to == CommandCompleted || to == CommandBlocked || to == CommandInconclusive || to == CommandFailed || to == CommandSuperseded
	default:
		return false
	}
}

func terminalCommandStatus(status string) bool {
	return status == CommandCompleted || status == CommandBlocked || status == CommandInconclusive || status == CommandFailed || status == CommandAmbiguous || status == CommandSuperseded
}

func knownCommandStatus(status string) bool {
	return status == CommandCreated || status == CommandDeliveryPending || status == CommandAwaitingResult || terminalCommandStatus(status)
}

func randomID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return "cmd-" + hex.EncodeToString(bytes)
}

func stamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseStamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

const commandSelect = `SELECT id,project_id,issue_number,identity_key,role,action,resource_profile,context_hash,expected_head,status,created_at,completed_at FROM workflow_commands`
const resultSelect = `SELECT project_id,github_comment_id,issue_number,asserted_command_id,role,result,payload_json,payload_hash,validation_status,validation_reason,accepted_at,observed_at FROM workflow_results`
