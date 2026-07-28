package browserbinding

import (
	"context"
	"time"
)

// WorkerProjection is the bounded operator-facing browser projection. Target is
// identity-only data already normalized by Register; no page content is exposed.
type WorkerProjection struct {
	WorkerID        string     `json:"worker_id"`
	ProtocolVersion string     `json:"protocol_version,omitempty"`
	Capabilities    []string   `json:"capabilities"`
	SessionID       string     `json:"worker_session_id,omitempty"`
	LastSeen        *time.Time `json:"last_seen,omitempty"`
	State           string     `json:"state"`
	Target          *TargetRef `json:"target,omitempty"`
}

func (s *Service) ListWorkerProjections(ctx context.Context) ([]WorkerProjection, error) {
	workers, err := s.ListWorkers(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	s.pruneLocked(now)

	out := make([]WorkerProjection, 0, len(workers))
	for _, worker := range workers {
		projection := WorkerProjection{
			WorkerID: worker.WorkerID, ProtocolVersion: worker.ProtocolVersion,
			Capabilities: worker.Capabilities, State: worker.State,
		}
		bySession := s.sessions[worker.WorkerID]
		switch len(bySession) {
		case 0:
			projection.State = "stale"
		case 1:
			for _, current := range bySession {
				projection.SessionID = current.id
				lastSeen := current.lastSeen
				projection.LastSeen = &lastSeen
				if current.conflict {
					projection.State = "conflict"
				} else {
					projection.State = "live"
					if current.target != nil {
						target := *current.target
						projection.Target = &target
					}
				}
			}
		default:
			projection.State = "conflict"
		}
		out = append(out, projection)
	}
	return out, nil
}
