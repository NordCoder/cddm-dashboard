package workflow

import (
	"sync"
	"time"
)

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

// ExternalCommand is the compact active execution state that prevents the
// existing Work Unit router from redispatching while a Dashboard-issued command
// is still being delivered or is awaiting its GitHub terminal result.
type ExternalCommand struct {
	ID              string
	IssueNumber     int
	Role            string
	Action          string
	ResourceProfile string
	ContextHash     string
	ExpectedHead    string
	Status          string
	CreatedAt       time.Time
}

type externalProjectState struct {
	results  map[int64]ExternalResult
	commands map[int]ExternalCommand
}

var projectExternalState sync.Map

// SetProjectExternalState atomically replaces the process-local projection for
// one Project. Durable authority remains GitHub plus persisted command/result
// records; this cache keeps all existing DeriveProject callers on one read model.
func SetProjectExternalState(projectID int64, results map[int64]ExternalResult, commands map[int]ExternalCommand) {
	resultCopy := make(map[int64]ExternalResult, len(results))
	for id, value := range results {
		resultCopy[id] = value
	}
	commandCopy := make(map[int]ExternalCommand, len(commands))
	for issueNumber, value := range commands {
		commandCopy[issueNumber] = value
	}
	projectExternalState.Store(projectID, externalProjectState{results: resultCopy, commands: commandCopy})
}

func projectExternal(projectID int64) (map[int64]ExternalResult, map[int]ExternalCommand) {
	value, ok := projectExternalState.Load(projectID)
	if !ok {
		return nil, nil
	}
	stored, _ := value.(externalProjectState)
	results := make(map[int64]ExternalResult, len(stored.results))
	for id, result := range stored.results {
		results[id] = result
	}
	commands := make(map[int]ExternalCommand, len(stored.commands))
	for issueNumber, command := range stored.commands {
		commands[issueNumber] = command
	}
	return results, commands
}
