package browserbinding

import (
	"fmt"
	"strings"
)

// RequireFreshTargetSession returns the exact live session that currently owns
// the supplied worker/target observation. The returned identity is suitable for
// durable correlation, while presence tokens remain process-local credentials.
func (s *Service) RequireFreshTargetSession(workerID string, target TargetRef) (string, error) {
	normalized, err := NormalizeTarget(target)
	if err != nil {
		return "", err
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return "", fmt.Errorf("%w: worker required", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(s.now().UTC())
	bySession := s.sessions[workerID]
	if len(bySession) != 1 {
		return "", ErrConflict
	}
	for _, current := range bySession {
		if current.conflict || current.id == "" || !sameTarget(current.target, &normalized) {
			return "", ErrConflict
		}
		return current.id, nil
	}
	return "", ErrConflict
}
