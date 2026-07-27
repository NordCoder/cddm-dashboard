import { completionPayload } from "./api.js";
import { CLAIM_FAILED_PRE_SEND, CLAIM_SENT, CLAIM_UNCERTAIN } from "./ledger.js";
import { sameTarget, safeDiagnostic, targetRefFromCommand } from "./protocol.js";

export class PreSendError extends Error {}
export class AmbiguousSendError extends Error {}

export function validateExecution(execution, identity, currentTarget) {
  const command = execution?.command;
  if (!command || !execution.claim_id || command.id === "") return "execution_identity_invalid";
  if (execution.claim_id !== command.claim_id) return "claim_identity_invalid";
  if (command.worker_id !== identity.workerId || command.worker_session_id !== identity.sessionId) return "worker_session_invalid";
  const target = targetRefFromCommand(command);
  if (!target || !sameTarget(target, currentTarget)) return "target_identity_invalid";
  if (command.status !== "claimed" || typeof execution.prompt !== "string") return "execution_payload_invalid";
  return null;
}

export class ExecutionCoordinator {
  constructor({ ledger, backend, adapter }) {
    this.ledger = ledger;
    this.backend = backend;
    this.adapter = adapter;
  }

  async acknowledge(entry, reason = entry.diagnostic) {
    if (entry.acknowledged) return entry;
    const outcome = entry.state === CLAIM_SENT ? "delivered" : entry.state === CLAIM_FAILED_PRE_SEND ? "failed" : "uncertain";
    try {
      await this.backend.complete(entry.command_id, completionPayload(entry.command_id, entry.claim_id, outcome, reason));
      return await this.ledger.acknowledge(entry.claim_id, true);
    } catch (error) {
      if (error?.status === 409) return entry;
      return entry;
    }
  }

  async execute(execution, identity, currentTarget) {
    const invalid = validateExecution(execution, identity, currentTarget);
    if (invalid) return { outcome: "uncertain", reason: invalid };
    const command = execution.command;
    const existing = await this.ledger.get(execution.claim_id);
    if (existing) {
      await this.acknowledge(existing);
      return { outcome: existing.state === CLAIM_SENT ? "delivered" : existing.state === CLAIM_FAILED_PRE_SEND ? "failed" : "uncertain", reason: existing.diagnostic };
    }

    let reserved;
    try {
      reserved = await this.ledger.reserve(execution.claim_id, command.id);
    } catch {
      await this.tryComplete(command.id, execution.claim_id, "uncertain", "ledger_unavailable");
      return { outcome: "uncertain", reason: "ledger_unavailable" };
    }
    if (!reserved.created) {
      await this.acknowledge(reserved.entry);
      return { outcome: "uncertain", reason: "duplicate_claim" };
    }

    let state = CLAIM_UNCERTAIN;
    let reason = "send_outcome_unknown";
    try {
      await this.adapter.insertPrompt(execution.prompt, targetRefFromCommand(command));
      const sent = await this.adapter.sendPrompt(targetRefFromCommand(command), execution.prompt);
      if (sent?.sent) {
        state = CLAIM_SENT;
        reason = "prompt_send_confirmed";
      } else {
        state = CLAIM_FAILED_PRE_SEND;
        reason = sent?.reason || "send_control_unavailable";
      }
    } catch (error) {
      if (error instanceof PreSendError) {
        state = CLAIM_FAILED_PRE_SEND;
        reason = error.message;
      } else if (error instanceof AmbiguousSendError) {
        state = CLAIM_UNCERTAIN;
        reason = error.message;
      }
    }
    const entry = await this.ledger.mark(execution.claim_id, state, safeDiagnostic(reason));
    await this.acknowledge(entry);
    await this.ledger.prune();
    return { outcome: state === CLAIM_SENT ? "delivered" : state === CLAIM_FAILED_PRE_SEND ? "failed" : "uncertain", reason };
  }

  async tryComplete(commandId, claimId, outcome, reason) {
    try { await this.backend.complete(commandId, completionPayload(commandId, claimId, outcome, reason)); } catch { /* durable backend reconciliation owns the final state */ }
  }
}
