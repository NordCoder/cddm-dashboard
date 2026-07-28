package workflow

import "sync"

// ExternalResult overlays one independently validated result onto the matching
// durable GitHub comment. The raw comment remains the display/audit source; this
// projection only supplies the typed terminal meaning established outside the
// legacy supervisor:event parser.
type ExternalResult struct {
	CommentID      int64
	Event          *WorkerEvent
	TransitionSafe bool
	Warnings       []Warning
	HardError      *ProtocolError
}

var projectExternalResults sync.Map

// SetProjectExternalResults replaces the process-local derived overlay for one
// Project. Durable authority remains GitHub plus the persisted validation audit;
// this cache only keeps all existing DeriveProject callers on one read model.
func SetProjectExternalResults(projectID int64, values map[int64]ExternalResult) {
	copy := make(map[int64]ExternalResult, len(values))
	for id, value := range values {
		copy[id] = value
	}
	projectExternalResults.Store(projectID, copy)
}

func projectExternal(projectID int64) map[int64]ExternalResult {
	value, ok := projectExternalResults.Load(projectID)
	if !ok {
		return nil
	}
	stored, _ := value.(map[int64]ExternalResult)
	copy := make(map[int64]ExternalResult, len(stored))
	for id, result := range stored {
		copy[id] = result
	}
	return copy
}
