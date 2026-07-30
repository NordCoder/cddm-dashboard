import {
  AutopilotCommand,
  AutopilotIntent,
  AutopilotLease,
  AutopilotMergeCycle,
  AutopilotProvisioning,
  AutopilotResult,
  AutopilotStatus,
} from './autopilot-domain.js'

export type AutopilotEvidenceRow = {
  intent: AutopilotIntent
  lease?: AutopilotLease
  provisioning?: AutopilotProvisioning
  command?: AutopilotCommand
  results: AutopilotResult[]
  merge_cycle?: AutopilotMergeCycle
}

export function buildAutopilotEvidenceRows(status: AutopilotStatus): AutopilotEvidenceRow[] {
  const leaseByIntent = new Map(status.leases.map((lease) => [lease.intent_id, lease]))
  const provisionByIntent = new Map(status.provisioning.map((request) => [request.intent_id, request]))
  const commandByIntent = new Map(status.commands.map((command) => [command.intent_id, command]))
  const mergeByIntent = new Map(status.merge_cycles.map((cycle) => [cycle.intent_id, cycle]))
  const resultsByCommand = new Map<string, AutopilotResult[]>()
  for (const result of status.results) {
    const current = resultsByCommand.get(result.command_id) ?? []
    current.push(result)
    resultsByCommand.set(result.command_id, current)
  }

  return status.intents.map((intent) => {
    const command = commandByIntent.get(intent.intent_id)
    return {
      intent,
      lease: leaseByIntent.get(intent.intent_id),
      provisioning: provisionByIntent.get(intent.intent_id),
      command,
      results: command?.workflow_command_id ? resultsByCommand.get(command.workflow_command_id) ?? [] : [],
      merge_cycle: mergeByIntent.get(intent.intent_id),
    }
  })
}
