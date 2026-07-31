package browserbinding

import (
	"fmt"
	"strings"
)

// WithFreshTargetSession proves that exactly one live process-local session owns
// the supplied worker/target observation and keeps that presence snapshot locked
// until use returns. Callers may durably commit the returned session identity in
// use without a concurrent Register or Heartbeat changing the in-memory owner.
// The callback must not call another Service method because the presence mutex is
// held for its entire execution.
func (s *Service) WithFreshTargetSession(workerID string, target TargetRef, use func(string) error) error {
	normalized, err := NormalizeTarget(target)
	if err != nil {
		return err
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" || use == nil {
		return fmt.Errorf("%w: worker and session callback required", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(s.now().UTC())
	bySession := s.sessions[workerID]
	if len(bySession) != 1 {
		return ErrConflict
	}
	for _, current := range bySession {
		if current.conflict || current.id == "" || !sameTarget(current.target, &normalized) {
			return ErrConflict
		}
		return use(current.id)
	}
	return ErrConflict
}

// RequireFreshTargetSession returns the exact live session that currently owns
// the supplied worker/target observation. The returned identity is suitable for
// immediate correlation, while presence tokens remain process-local credentials.
// Durable callers that need the presence identity to remain stable through a
// commit must use WithFreshTargetSession instead.
func (s *Service) RequireFreshTargetSession(workerID string, target TargetRef) (string, error) {
	var sessionID string
	err := s.WithFreshTargetSession(workerID, target, func(current string) error {
		sessionID = current
		return nil
	})
	return sessionID, err
}
