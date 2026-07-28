package workerloop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
)

type CommandEngine struct {
	store     *Store
	resources resourcepack.Package
}

type PreparedWorkflowCommand struct {
	Input  CreateCommandInput
	Prompt string
}

func NewCommandEngine(store *Store, resources resourcepack.Package) *CommandEngine {
	return &CommandEngine{store: store, resources: resources}
}

func (e *CommandEngine) BuildWorkflowCommand(projectID int64, issueNumber int, generation planning.GenerationResult) (PreparedWorkflowCommand, error) {
	if generation.Plan == nil || generation.PolicyDecision.Status != planning.StatusApproved {
		return PreparedWorkflowCommand{}, fmt.Errorf("workflow command requires a policy-approved Prompt Plan")
	}
	plan := generation.Plan
	if plan.Action != "dispatch" || plan.TargetRole == "" || plan.LaneKey == "" {
		return PreparedWorkflowCommand{}, fmt.Errorf("workflow command requires a dispatchable role plan")
	}
	resource, err := e.resources.Role(plan.TargetRole)
	if err != nil {
		return PreparedWorkflowCommand{}, err
	}
	identity := commandIdentity(generation, e.resources)
	input := CreateCommandInput{
		ID: deterministicCommandID(identity), ProjectID: projectID, IssueNumber: issueNumber, IdentityKey: identity,
		Role: plan.TargetRole, Action: plan.Action, ResourceProfile: e.resources.Profile,
		ContextHash: generation.Context.ContextHash, ExpectedHead: plan.ExpectedHead,
		Status: CommandDeliveryPending,
	}
	command := Command{
		ID: input.ID, ProjectID: input.ProjectID, IssueNumber: input.IssueNumber,
		Role: input.Role, Action: input.Action, ResourceProfile: input.ResourceProfile,
		ContextHash: input.ContextHash, ExpectedHead: input.ExpectedHead, Status: input.Status,
	}
	prompt, err := renderCommandPrompt(command, generation, e.resources, resource)
	if err != nil {
		return PreparedWorkflowCommand{}, err
	}
	return PreparedWorkflowCommand{Input: input, Prompt: prompt}, nil
}

func (e *CommandEngine) PersistWorkflowCommand(ctx context.Context, prepared PreparedWorkflowCommand) (Command, error) {
	return e.store.CreateCommand(ctx, prepared.Input)
}

func (e *CommandEngine) PrepareWorkflowCommand(ctx context.Context, projectID int64, issueNumber int, generation planning.GenerationResult) (string, string, error) {
	prepared, err := e.BuildWorkflowCommand(projectID, issueNumber, generation)
	if err != nil {
		return "", "", err
	}
	command, err := e.PersistWorkflowCommand(ctx, prepared)
	if err != nil {
		return "", "", err
	}
	return command.ID, prepared.Prompt, nil
}

func (e *CommandEngine) RecordDeliveryOutcome(ctx context.Context, commandID, outcome string) error {
	if commandID == "" {
		return nil
	}
	status := ""
	switch outcome {
	case "pending", "claimed":
		status = CommandDeliveryPending
	case "delivered":
		status = CommandAwaitingResult
	case "uncertain":
		status = CommandAmbiguous
	case "invalidated":
		status = CommandSuperseded
	case "failed", "cancelled", "expired":
		status = CommandFailed
	default:
		return fmt.Errorf("unsupported delivery outcome %q", outcome)
	}
	command, err := e.store.GetCommand(ctx, commandID)
	if err != nil {
		return err
	}
	if command.Status == status {
		return nil
	}
	if terminalCommandStatus(command.Status) {
		return nil
	}
	_, err = e.store.SetCommandStatus(ctx, commandID, status)
	return err
}

func commandIdentity(generation planning.GenerationResult, resources resourcepack.Package) string {
	values := []string{
		fmt.Sprint(generation.PlanID), generation.PolicyDecision.PlanHash,
		generation.Context.ContextHash, generation.Plan.Action, generation.Plan.TargetRole,
		generation.Plan.LaneKey, generation.Plan.ExpectedHead, resources.Profile, resources.Digest,
	}
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}

func deterministicCommandID(identity string) string {
	if len(identity) > 32 {
		identity = identity[:32]
	}
	return "cmd-" + identity
}

func renderCommandPrompt(command Command, generation planning.GenerationResult, resources resourcepack.Package, roleResource string) (string, error) {
	contextJSON, err := json.MarshalIndent(generation.Context, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode workflow Context Pack: %w", err)
	}
	plan := generation.Plan
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Dashboard Command Header\n\n")
	fmt.Fprintf(&builder, "```text\nCommand ID: %s\nRepository: %s/%s\nIssue: #%d\nRole: %s\nAction: %s\nResources: %s\nBase methodology: %s/v%s\nResult protocol: %s/v%s\nExpected Head: %s\n```\n\n",
		command.ID, generation.Context.Repository.Owner, generation.Context.Repository.Repository,
		generation.Context.Issue.Number, command.Role, command.Action, resources.Profile,
		resources.Manifest.BaseMethodology.Package, resources.Manifest.BaseMethodology.Version,
		resources.Manifest.ResultProtocol.Package, resources.Manifest.ResultProtocol.Version,
		valueOrNone(command.ExpectedHead),
	)
	builder.WriteString("## Versioned Role Resource\n\n")
	builder.WriteString(strings.TrimSpace(roleResource))
	builder.WriteString("\n\n## Bounded Context Pack\n\n```json\n")
	builder.Write(contextJSON)
	builder.WriteString("\n```\n\n## Bounded Planned Action\n\n")
	builder.WriteString(strings.TrimSpace(plan.Prompt))
	builder.WriteString("\n\n## Terminal Publication Contract\n\n")
	fmt.Fprintf(&builder, "Publish exactly one GitHub Issue comment containing a human-readable role handoff and one live marker conforming to `%s/v%s`. The marker MUST use `command_id` = `%s`. Do not launch the next worker manually. Dashboard will verify the external facts and derive the next route.\n",
		resources.Manifest.ResultProtocol.Package, resources.Manifest.ResultProtocol.Version, command.ID)
	return builder.String(), nil
}

func valueOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return value
}
