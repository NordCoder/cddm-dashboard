// Package browserbinding owns durable lane bindings and process-local browser presence.
package browserbinding

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const TargetKindChatGPTConversation = "chatgpt_conversation"

var (
	ErrNotFound = errors.New("browser binding not found")
	ErrConflict = errors.New("browser binding conflict")
	ErrInvalid  = errors.New("invalid browser binding input")
)

type TargetRef struct {
	Kind   string `json:"kind"`
	Origin string `json:"origin"`
	Path   string `json:"path"`
	Label  string `json:"label,omitempty"`
}

type Observation struct {
	Target *TargetRef `json:"target,omitempty"`
}
type RegisterInput struct {
	WorkerID        string      `json:"worker_id"`
	SessionID       string      `json:"worker_session_id"`
	ProtocolVersion string      `json:"protocol_version,omitempty"`
	Capabilities    []string    `json:"capabilities,omitempty"`
	Observation     Observation `json:"observation"`
}

type Worker struct {
	WorkerID        string     `json:"worker_id"`
	ProtocolVersion string     `json:"protocol_version,omitempty"`
	Capabilities    []string   `json:"capabilities"`
	SessionID       string     `json:"worker_session_id,omitempty"`
	LastSeen        *time.Time `json:"last_seen,omitempty"`
	State           string     `json:"state"`
}

type Binding struct {
	BindingID       string     `json:"binding_id"`
	BindingVersion  int64      `json:"binding_version"`
	ProjectID       int64      `json:"project_id"`
	LaneKey         string     `json:"lane_key"`
	WorkerID        string     `json:"worker_id"`
	Target          TargetRef  `json:"target"`
	Enabled         bool       `json:"enabled"`
	Readiness       string     `json:"readiness"`
	WorkerSessionID string     `json:"worker_session_id,omitempty"`
	PresenceToken   string     `json:"presence_token,omitempty"`
	LastSeen        *time.Time `json:"last_seen,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type PutInput struct {
	ExpectedVersion *int64
	WorkerID        string
	Target          TargetRef
}

type session struct {
	id       string
	lastSeen time.Time
	target   *TargetRef
	token    string
	conflict bool
}
type Service struct {
	db       *sql.DB
	ttl      time.Duration
	now      func() time.Time
	mu       sync.Mutex
	sessions map[string]map[string]*session
}

func New(db *sql.DB, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Service{db: db, ttl: ttl, now: func() time.Time { return time.Now().UTC() }, sessions: make(map[string]map[string]*session)}
}

func NormalizeTarget(target TargetRef) (TargetRef, error) {
	target.Kind = strings.TrimSpace(target.Kind)
	target.Origin = strings.TrimSpace(target.Origin)
	target.Path = strings.TrimSpace(target.Path)
	target.Label = strings.TrimSpace(target.Label)
	if target.Kind != TargetKindChatGPTConversation || target.Origin != "https://chatgpt.com" || len(target.Label) > 120 {
		return TargetRef{}, fmt.Errorf("%w: unsupported target", ErrInvalid)
	}
	u, err := url.Parse(target.Origin)
	if err != nil || u.Scheme != "https" || u.Host != "chatgpt.com" || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return TargetRef{}, fmt.Errorf("%w: invalid target origin", ErrInvalid)
	}
	parsed, err := url.Parse(target.Path)
	if err != nil || parsed.IsAbs() || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != target.Path || !strings.HasPrefix(target.Path, "/c/") || path.Clean(target.Path) != target.Path {
		return TargetRef{}, fmt.Errorf("%w: invalid conversation path", ErrInvalid)
	}
	if key := strings.TrimPrefix(target.Path, "/c/"); key == "" || strings.Contains(key, "/") || len(key) > 200 {
		return TargetRef{}, fmt.Errorf("%w: invalid conversation key", ErrInvalid)
	}
	// The first browser protocol carries no display text. Keeping labels empty makes
	// the durable target identity unable to accidentally retain page content.
	target.Label = ""
	return target, nil
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (Worker, error) {
	input.WorkerID, input.SessionID = strings.TrimSpace(input.WorkerID), strings.TrimSpace(input.SessionID)
	if input.WorkerID == "" || input.SessionID == "" || len(input.WorkerID) > 200 || len(input.SessionID) > 200 {
		return Worker{}, fmt.Errorf("%w: worker and session IDs are required", ErrInvalid)
	}
	if input.Observation.Target != nil {
		normalized, err := NormalizeTarget(*input.Observation.Target)
		if err != nil {
			return Worker{}, err
		}
		input.Observation.Target = &normalized
	}
	if len(input.ProtocolVersion) > 80 || len(input.Capabilities) > 32 {
		return Worker{}, fmt.Errorf("%w: metadata too large", ErrInvalid)
	}
	capabilities, err := json.Marshal(input.Capabilities)
	if err != nil {
		return Worker{}, err
	}
	now := s.now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO browser_workers(worker_id, protocol_version, capabilities_json, created_at, updated_at) VALUES(?,?,?,?,?) ON CONFLICT(worker_id) DO UPDATE SET protocol_version=excluded.protocol_version, capabilities_json=excluded.capabilities_json, updated_at=excluded.updated_at`, input.WorkerID, strings.TrimSpace(input.ProtocolVersion), string(capabilities), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Worker{}, fmt.Errorf("store worker: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	bySession := s.sessions[input.WorkerID]
	if bySession == nil {
		bySession = make(map[string]*session)
		s.sessions[input.WorkerID] = bySession
	}
	if current := bySession[input.SessionID]; current != nil {
		if !sameTarget(current.target, input.Observation.Target) {
			current.token = randomID()
		}
		current.lastSeen, current.target = now, input.Observation.Target
	} else {
		bySession[input.SessionID] = &session{id: input.SessionID, lastSeen: now, target: input.Observation.Target, token: randomID()}
	}
	s.markConflictLocked(input.WorkerID)
	return s.workerLocked(input.WorkerID), nil
}

func (s *Service) Heartbeat(ctx context.Context, workerID string, input RegisterInput) (Worker, error) {
	if strings.TrimSpace(workerID) != strings.TrimSpace(input.WorkerID) {
		return Worker{}, fmt.Errorf("%w: worker ID mismatch", ErrInvalid)
	}
	return s.Register(ctx, input)
}

func (s *Service) ListWorkers(ctx context.Context) ([]Worker, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT worker_id, protocol_version, capabilities_json FROM browser_workers ORDER BY worker_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(s.now().UTC())
	workers := []Worker{}
	for rows.Next() {
		var w Worker
		var caps string
		if err := rows.Scan(&w.WorkerID, &w.ProtocolVersion, &caps); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(caps), &w.Capabilities)
		live := s.workerLocked(w.WorkerID)
		w.SessionID, w.LastSeen, w.State = live.SessionID, live.LastSeen, live.State
		workers = append(workers, w)
	}
	return workers, rows.Err()
}

// RequireFreshTarget proves that exactly one live process-local worker session
// currently observes the supplied exact conversation target. It is intended for
// callers that perform their own durable transaction after this presence check.
func (s *Service) RequireFreshTarget(workerID string, target TargetRef) error {
	normalized, err := NormalizeTarget(target)
	if err != nil {
		return err
	}
	if strings.TrimSpace(workerID) == "" {
		return fmt.Errorf("%w: worker required", ErrInvalid)
	}
	return s.requireFreshTarget(strings.TrimSpace(workerID), normalized)
}

func (s *Service) Put(ctx context.Context, projectID int64, laneKey string, input PutInput) (Binding, error) {
	target, err := NormalizeTarget(input.Target)
	if err != nil {
		return Binding{}, err
	}
	if projectID <= 0 || strings.TrimSpace(laneKey) == "" || strings.TrimSpace(input.WorkerID) == "" {
		return Binding{}, fmt.Errorf("%w: project, lane, worker required", ErrInvalid)
	}
	if err := s.requireFreshTarget(input.WorkerID, target); err != nil {
		return Binding{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Binding{}, err
	}
	defer tx.Rollback()
	var existing Binding
	existing, err = readBinding(ctx, tx, projectID, laneKey)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Binding{}, err
	}
	if errors.Is(err, ErrNotFound) {
		if input.ExpectedVersion != nil && *input.ExpectedVersion != 0 {
			return Binding{}, ErrConflict
		}
		if err := ensureTargetFree(ctx, tx, projectID, laneKey, input.WorkerID, target); err != nil {
			return Binding{}, err
		}
		now := s.now().UTC()
		existing = Binding{BindingID: randomID(), BindingVersion: 1, ProjectID: projectID, LaneKey: laneKey, WorkerID: input.WorkerID, Target: target, Enabled: true, UpdatedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO browser_lane_bindings(binding_id,project_id,lane_key,worker_id,target_kind,target_origin,target_path,target_label,enabled,binding_version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, existing.BindingID, projectID, laneKey, input.WorkerID, target.Kind, target.Origin, target.Path, target.Label, 1, 1, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	} else {
		if input.ExpectedVersion == nil || *input.ExpectedVersion != existing.BindingVersion {
			return Binding{}, ErrConflict
		}
		if err := ensureTargetFree(ctx, tx, projectID, laneKey, input.WorkerID, target); err != nil {
			return Binding{}, err
		}
		now := s.now().UTC()
		existing.BindingVersion++
		existing.WorkerID, existing.Target, existing.Enabled, existing.UpdatedAt = input.WorkerID, target, true, now
		_, err = tx.ExecContext(ctx, `UPDATE browser_lane_bindings SET worker_id=?,target_kind=?,target_origin=?,target_path=?,target_label=?,enabled=1,binding_version=?,updated_at=? WHERE binding_id=?`, input.WorkerID, target.Kind, target.Origin, target.Path, target.Label, existing.BindingVersion, now.Format(time.RFC3339Nano), existing.BindingID)
	}
	if err != nil {
		return Binding{}, fmt.Errorf("write binding: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Binding{}, err
	}
	return s.project(existing), nil
}

func (s *Service) Disable(ctx context.Context, projectID int64, laneKey string, expectedVersion int64) (Binding, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Binding{}, err
	}
	defer tx.Rollback()
	b, err := readBinding(ctx, tx, projectID, laneKey)
	if err != nil {
		return Binding{}, err
	}
	if expectedVersion != b.BindingVersion {
		return Binding{}, ErrConflict
	}
	b.Enabled = false
	b.BindingVersion++
	b.UpdatedAt = s.now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE browser_lane_bindings SET enabled=0,binding_version=?,updated_at=? WHERE binding_id=?`, b.BindingVersion, b.UpdatedAt.Format(time.RFC3339Nano), b.BindingID); err != nil {
		return Binding{}, err
	}
	if err = tx.Commit(); err != nil {
		return Binding{}, err
	}
	return s.project(b), nil
}

func (s *Service) Get(ctx context.Context, projectID int64, laneKey string) (Binding, error) {
	b, err := readBinding(ctx, s.db, projectID, laneKey)
	if err != nil {
		return Binding{}, err
	}
	return s.project(b), nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readBinding(ctx context.Context, q queryer, projectID int64, laneKey string) (Binding, error) {
	var b Binding
	var enabled int
	var updated string
	err := q.QueryRowContext(ctx, `SELECT binding_id,binding_version,project_id,lane_key,worker_id,target_kind,target_origin,target_path,target_label,enabled,updated_at FROM browser_lane_bindings WHERE project_id=? AND lane_key=?`, projectID, laneKey).Scan(&b.BindingID, &b.BindingVersion, &b.ProjectID, &b.LaneKey, &b.WorkerID, &b.Target.Kind, &b.Target.Origin, &b.Target.Path, &b.Target.Label, &enabled, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrNotFound
	}
	if err != nil {
		return Binding{}, err
	}
	b.Enabled = enabled != 0
	b.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return b, err
}
func ensureTargetFree(ctx context.Context, tx *sql.Tx, projectID int64, laneKey, workerID string, target TargetRef) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT binding_id FROM browser_lane_bindings WHERE enabled=1 AND worker_id=? AND target_kind=? AND target_origin=? AND target_path=? AND NOT(project_id=? AND lane_key=?)`, workerID, target.Kind, target.Origin, target.Path, projectID, laneKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrConflict
}
func (s *Service) requireFreshTarget(workerID string, target TargetRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(s.now().UTC())
	by := s.sessions[workerID]
	if len(by) != 1 {
		return ErrConflict
	}
	for _, v := range by {
		if v.conflict || !sameTarget(v.target, &target) {
			return ErrConflict
		}
	}
	return nil
}
func (s *Service) project(b Binding) Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	s.pruneLocked(now)
	if !b.Enabled {
		b.Readiness = "disabled"
		return b
	}
	by := s.sessions[b.WorkerID]
	if len(by) > 1 {
		b.Readiness = "conflict"
		return b
	}
	if len(by) == 0 {
		b.Readiness = "stale"
		return b
	}
	for _, v := range by {
		b.LastSeen = &v.lastSeen
		if v.conflict {
			b.Readiness = "conflict"
		} else if !sameTarget(v.target, &b.Target) {
			b.Readiness = "unavailable"
		} else {
			b.Readiness = "ready"
			b.WorkerSessionID = v.id
			b.PresenceToken = v.token
		}
		return b
	}
	b.Readiness = "stale"
	return b
}
func (s *Service) pruneLocked(now time.Time) {
	for worker, by := range s.sessions {
		for id, v := range by {
			if now.Sub(v.lastSeen) > s.ttl {
				delete(by, id)
			}
		}
		if len(by) == 0 {
			delete(s.sessions, worker)
		} else {
			s.markConflictLocked(worker)
		}
	}
}
func (s *Service) markConflictLocked(workerID string) {
	by := s.sessions[workerID]
	conflict := len(by) > 1
	wasConflict := false
	for _, v := range by {
		wasConflict = wasConflict || v.conflict
	}
	if conflict && !wasConflict {
		// A conflict invalidates every prior observation for this installation.
		// If one session later wins by staleness, it must prove freshness with a
		// token that cannot equal any token usable before the conflict.
		for _, v := range by {
			v.token = randomID()
		}
	}
	for _, v := range by {
		v.conflict = conflict
	}
}
func (s *Service) workerLocked(workerID string) Worker {
	by := s.sessions[workerID]
	if len(by) == 0 {
		return Worker{WorkerID: workerID, State: "stale"}
	}
	if len(by) > 1 {
		return Worker{WorkerID: workerID, State: "conflict"}
	}
	for _, v := range by {
		return Worker{WorkerID: workerID, SessionID: v.id, LastSeen: &v.lastSeen, State: "live"}
	}
	return Worker{}
}
func sameTarget(left, right *TargetRef) bool {
	return left != nil && right != nil && left.Kind == right.Kind && left.Origin == right.Origin && left.Path == right.Path
}
func randomID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}
