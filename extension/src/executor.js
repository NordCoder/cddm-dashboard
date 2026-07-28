import { completionPayload } from "./api.js";
import { CLAIM_FAILED_PRE_SEND, CLAIM_RESERVED, CLAIM_SENT, CLAIM_UNCERTAIN } from "./ledger.js";
import { isOpaqueIdentifier, normalizeBackendOrigin, sameTarget, safeDiagnostic, sha256Hex, targetRefFromCommand } from "./protocol.js";

export class PreSendError extends Error {}
export class AmbiguousSendError extends Error {}

const PROMPT_HASH = /^[a-f0-9]{64}$/;
const TERMINAL_COMPLETION_HTTP = new Set([400, 404, 410, 422]);

export function validateExecution(execution, identity, currentTarget) {
  const command = execution?.command;
  if (!command || !isOpaqueIdentifier(execution?.claim_id) || !isOpaqueIdentifier(command.id)) return "execution_identity_invalid";
  if (!isOpaqueIdentifier(command.claim_id) || execution.claim_id !== command.claim_id) return "claim_identity_invalid";
  if (!isOpaqueIdentifier(command.worker_id) || !isOpaqueIdentifier(command.worker_session_id)
      || command.worker_id !== identity.workerId || command.worker_session_id !== identity.sessionId) return "worker_session_invalid";
  const target = targetRefFromCommand(command);
  if (!target || !sameTarget(target, currentTarget)) return "target_identity_invalid";
  if (command.status !== "claimed" || typeof execution.prompt !== "string" || execution.prompt.length === 0
      || !PROMPT_HASH.test(command.prompt_hash ?? "")) return "execution_payload_invalid";
  return null;
}

async function validatePromptIntegrity(execution) {
  try {
    return await sha256Hex(execution.prompt) === execution.command.prompt_hash ? null : "prompt_hash_mismatch";
  } catch {
    return "prompt_hash_unavailable";
  }
}

function inProgressResult() {
  return { outcome: "uncertain", reason: "claim_in_progress", completion_diagnostic: "" };
}

export class ExecutionCoordinator {
  constructor({ ledger, backend, adapter, backendOrigin }) {
    this.ledger = ledger;
    this.backend = backend;
    this.adapter = adapter;
    this.backendOrigin = normalizeBackendOrigin(backendOrigin);
  }

  async acknowledge(entry, reason = entry.diagnostic) {
    if (entry.acknowledged || entry.state === CLAIM_RESERVED || entry.backend_origin !== this.backendOrigin
        || !isOpaqueIdentifier(entry.command_id) || !isOpaqueIdentifier(entry.claim_id)) return entry;
    const outcome = entry.state === CLAIM_SENT ? "delivered" : entry.state === CLAIM_FAILED_PRE_SEND ? "failed" : "uncertain";
    try {
      await this.backend.complete(entry.command_id, completionPayload(entry.command_id, entry.claim_id, outcome, reason));
      return await this.ledger.acknowledge(entry.claim_id, true);
    } catch (error) {
      if (error?.status === 409) return await this.ledger.acknowledge(entry.claim_id, true, "completion_conflict");
      if (TERMINAL_COMPLETION_HTTP.has(error?.status)) {
        return await this.ledger.acknowledge(entry.claim_id, true, `completion_rejected_${error.status}`);
      }
      return entry;
    }
  }

  resultForEntry(entry, reason = entry.diagnostic) {
    return {
      outcome: entry.state === CLAIM_SENT ? "delivered" : entry.state === CLAIM_FAILED_PRE_SEND ? "failed" : "uncertain",
      reason,
      completion_diagnostic: entry.ack_diagnostic || ""
    };
  }

  async execute(execution, identity, currentTarget, preSendGuard = null) {
    const invalid = validateExecution(execution, identity, currentTarget);
    if (invalid) return { outcome: "uncertain", reason: invalid, completion_diagnostic: "" };
    const command = execution.command;

    let existing;
    try { existing = await this.ledger.get(execution.claim_id); } catch {
      await this.tryComplete(command.id, execution.claim_id, "uncertain", "ledger_unavailable");
      return { outcome: "uncertain", reason: "ledger_unavailable", completion_diagnostic: "" };
    }
    if (existing) {
      if (existing.command_id !== command.id || existing.backend_origin !== this.backendOrigin) {
        await this.tryComplete(command.id, execution.claim_id, "uncertain", "claim_authority_mismatch");
        return { outcome: "uncertain", reason: "claim_authority_mismatch", completion_diagnostic: "" };
      }
      if (existing.state === CLAIM_RESERVED) return inProgressResult();
      const acknowledged = await this.acknowledge(existing);
      return this.resultForEntry(acknowledged || existing);
    }

    let reserved;
    try {
      reserved = await this.ledger.reserve(execution.claim_id, command.id, this.backendOrigin);
    } catch {
      await this.tryComplete(command.id, execution.claim_id, "uncertain", "ledger_unavailable");
      return { outcome: "uncertain", reason: "ledger_unavailable", completion_diagnostic: "" };
    }
    if (!reserved.created) {
      if (reserved.entry.command_id !== command.id || reserved.entry.backend_origin !== this.backendOrigin) {
        await this.tryComplete(command.id, execution.claim_id, "uncertain", "claim_authority_mismatch");
        return { outcome: "uncertain", reason: "claim_authority_mismatch", completion_diagnostic: "" };
      }
      if (reserved.entry.state === CLAIM_RESERVED) return inProgressResult();
      const acknowledged = await this.acknowledge(reserved.entry);
      return this.resultForEntry(acknowledged || reserved.entry, "duplicate_claim");
    }

    const integrity = await validatePromptIntegrity(execution);
    if (integrity) {
      let entry;
      try {
        entry = await this.ledger.mark(execution.claim_id, CLAIM_UNCERTAIN, integrity);
      } catch {
        await this.tryComplete(command.id, execution.claim_id, "uncertain", "ledger_terminal_persist_failed");
        return { outcome: "uncertain", reason: "ledger_terminal_persist_failed", completion_diagnostic: "" };
      }
      const acknowledged = await this.acknowledge(entry, integrity);
      return this.resultForEntry(acknowledged || entry, integrity);
    }

    let state = CLAIM_UNCERTAIN;
    let reason = "send_outcome_unknown";
    try {
      if (preSendGuard) await preSendGuard();
      await this.adapter.insertPrompt(execution.prompt, targetRefFromCommand(command));
      if (preSendGuard) await preSendGuard();
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

    let entry;
    try {
      entry = await this.ledger.mark(execution.claim_id, state, safeDiagnostic(reason));
    } catch {
      await this.tryComplete(command.id, execution.claim_id, "uncertain", "ledger_terminal_persist_failed");
      return { outcome: "uncertain", reason: "ledger_terminal_persist_failed", completion_diagnostic: "" };
    }
    const acknowledged = await this.acknowledge(entry);
    try { await this.ledger.prune(); } catch { /* pruning cannot affect current claim durability */ }
    return this.resultForEntry(acknowledged || entry, reason);
  }

  async tryComplete(commandId, claimId, outcome, reason) {
    if (!isOpaqueIdentifier(commandId) || !isOpaqueIdentifier(claimId)) return;
    try { await this.backend.complete(commandId, completionPayload(commandId, claimId, outcome, reason)); } catch { /* durable backend reconciliation owns the final state */ }
  }
}
