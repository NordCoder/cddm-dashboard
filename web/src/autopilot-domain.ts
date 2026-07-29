import { ValidationError } from './domain.js'

export type AutopilotControl = {
  project_id: number
  revision: number
  last_action: string
  updated_at: string
}

export type AutopilotProfile = {
  autonomy_mode: string
  autonomy_state: string
  control_issue_number: number
  resource_version: string
  methodology_version: string
  result_protocol: string
  chatgpt_project_url?: string
}

export type AutopilotCounts = {
  pending_intents: number
  blocked_intents: number
  claimed_intents: number
  active_leases: number
  pending_provisioning: number
  managed_sessions: number
  active_commands: number
  active_circuit_breakers: number
  ambiguous_records: number
}

export type AutopilotIntent = {
  intent_id: string
  action_type: string
  issue_number: number
  role?: string
  pr_number: number
  expected_head?: string
  priority: number
  lane_key?: string
  status: string
}

export type AutopilotQueueItem = {
  intent: AutopilotIntent
  waiting_reason?: string
}

export type AutopilotLease = {
  lease_id: string
  lane_key: string
  intent_id: string
  lease_owner: string
  status: string
  acquired_at: string
  expires_at: string
}

export type AutopilotBreaker = {
  id: string
  scope_kind: string
  lane_key?: string
  code: string
  reason: string
  recovery_requirements: string
  evidence?: string
  expected_head?: string
  status: string
  occurrence_count: number
  updated_at: string
}

export type AutopilotProvisioning = {
  id: string
  intent_id: string
  lane_key: string
  issue_number: number
  role: string
  expected_head?: string
  status: string
  completion_reason?: string
  worker_id?: string
  observed_chatgpt_url?: string
  bound_binding_id?: string
  updated_at: string
}

export type AutopilotCommand = {
  materialization_id: string
  intent_id: string
  lane_key: string
  status: string
  reason_code?: string
  workflow_command_id?: string
  workflow_status?: string
  delivery_command_id?: string
  delivery_status?: string
  updated_at: string
}

export type AutopilotWarning = {
  code: string
  intent_id?: string
  issue_number?: number
  pr_number?: number
  expected_head?: string
  observed_head?: string
  message: string
}

export type AutopilotWave = {
  wave_id: string
  status: string
  issues: number[]
}

export type AutopilotStatus = {
  project_id: number
  repository: string
  sync_status: string
  sync_error?: string
  profile: AutopilotProfile
  control: AutopilotControl
  active_wave?: AutopilotWave
  queue: AutopilotQueueItem[]
  active_leases: AutopilotLease[]
  provisioning: AutopilotProvisioning[]
  commands: AutopilotCommand[]
  circuit_breakers: AutopilotBreaker[]
  warnings: AutopilotWarning[]
  counts: AutopilotCounts
  project_hold_reason?: string
  lead_busy: boolean
  next_action: string
  generated_at: string
}

type RecordValue = Record<string, unknown>

function record(value: unknown, path: string): RecordValue {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new ValidationError(path, 'object')
  return value as RecordValue
}
function stringValue(value: unknown, path: string): string {
  if (typeof value !== 'string') throw new ValidationError(path, 'string')
  return value
}
function optionalString(value: unknown, path: string): string | undefined {
  if (value === undefined || value === null || value === '') return undefined
  return stringValue(value, path)
}
function numberValue(value: unknown, path: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) throw new ValidationError(path, 'finite number')
  return value
}
function optionalNumber(value: unknown, path: string): number | undefined {
  if (value === undefined || value === null) return undefined
  return numberValue(value, path)
}
function booleanValue(value: unknown, path: string): boolean {
  if (typeof value !== 'boolean') throw new ValidationError(path, 'boolean')
  return value
}
function arrayValue<T>(value: unknown, path: string, parser: (item: unknown, path: string) => T): T[] {
  if (value === undefined || value === null) return []
  if (!Array.isArray(value)) throw new ValidationError(path, 'array')
  return value.map((item, index) => parser(item, `${path}[${index}]`))
}

function parseControl(value: unknown, path: string): AutopilotControl {
  const item = record(value, path)
  return { project_id: numberValue(item.project_id, `${path}.project_id`), revision: numberValue(item.revision, `${path}.revision`), last_action: stringValue(item.last_action, `${path}.last_action`), updated_at: stringValue(item.updated_at, `${path}.updated_at`) }
}
function parseProfile(value: unknown, path: string): AutopilotProfile {
  const item = record(value, path)
  return {
    autonomy_mode: stringValue(item.autonomy_mode, `${path}.autonomy_mode`),
    autonomy_state: stringValue(item.autonomy_state, `${path}.autonomy_state`),
    control_issue_number: numberValue(item.control_issue_number, `${path}.control_issue_number`),
    resource_version: stringValue(item.resource_version, `${path}.resource_version`),
    methodology_version: stringValue(item.methodology_version, `${path}.methodology_version`),
    result_protocol: stringValue(item.result_protocol, `${path}.result_protocol`),
    chatgpt_project_url: optionalString(item.chatgpt_project_url, `${path}.chatgpt_project_url`),
  }
}
function parseCounts(value: unknown, path: string): AutopilotCounts {
  const item = record(value, path)
  return {
    pending_intents: numberValue(item.pending_intents, `${path}.pending_intents`), blocked_intents: numberValue(item.blocked_intents, `${path}.blocked_intents`),
    claimed_intents: numberValue(item.claimed_intents, `${path}.claimed_intents`), active_leases: numberValue(item.active_leases, `${path}.active_leases`),
    pending_provisioning: numberValue(item.pending_provisioning, `${path}.pending_provisioning`), managed_sessions: numberValue(item.managed_sessions, `${path}.managed_sessions`),
    active_commands: numberValue(item.active_commands, `${path}.active_commands`), active_circuit_breakers: numberValue(item.active_circuit_breakers, `${path}.active_circuit_breakers`),
    ambiguous_records: numberValue(item.ambiguous_records, `${path}.ambiguous_records`),
  }
}
function parseIntent(value: unknown, path: string): AutopilotIntent {
  const item = record(value, path)
  return { intent_id: stringValue(item.intent_id, `${path}.intent_id`), action_type: stringValue(item.action_type, `${path}.action_type`), issue_number: optionalNumber(item.issue_number, `${path}.issue_number`) ?? 0, role: optionalString(item.role, `${path}.role`), pr_number: optionalNumber(item.pr_number, `${path}.pr_number`) ?? 0, expected_head: optionalString(item.expected_head, `${path}.expected_head`), priority: numberValue(item.priority, `${path}.priority`), lane_key: optionalString(item.lane_key, `${path}.lane_key`), status: stringValue(item.status, `${path}.status`) }
}
function parseQueue(value: unknown, path: string): AutopilotQueueItem {
  const item = record(value, path)
  return { intent: parseIntent(item.intent, `${path}.intent`), waiting_reason: optionalString(item.waiting_reason, `${path}.waiting_reason`) }
}
function parseLease(value: unknown, path: string): AutopilotLease {
  const item = record(value, path)
  return { lease_id: stringValue(item.lease_id, `${path}.lease_id`), lane_key: stringValue(item.lane_key, `${path}.lane_key`), intent_id: stringValue(item.intent_id, `${path}.intent_id`), lease_owner: stringValue(item.lease_owner, `${path}.lease_owner`), status: stringValue(item.status, `${path}.status`), acquired_at: stringValue(item.acquired_at, `${path}.acquired_at`), expires_at: stringValue(item.expires_at, `${path}.expires_at`) }
}
function parseBreaker(value: unknown, path: string): AutopilotBreaker {
  const item = record(value, path)
  return { id: stringValue(item.id, `${path}.id`), scope_kind: stringValue(item.scope_kind, `${path}.scope_kind`), lane_key: optionalString(item.lane_key, `${path}.lane_key`), code: stringValue(item.code, `${path}.code`), reason: stringValue(item.reason, `${path}.reason`), recovery_requirements: stringValue(item.recovery_requirements, `${path}.recovery_requirements`), evidence: optionalString(item.evidence, `${path}.evidence`), expected_head: optionalString(item.expected_head, `${path}.expected_head`), status: stringValue(item.status, `${path}.status`), occurrence_count: numberValue(item.occurrence_count, `${path}.occurrence_count`), updated_at: stringValue(item.updated_at, `${path}.updated_at`) }
}
function parseProvisioning(value: unknown, path: string): AutopilotProvisioning {
  const item = record(value, path)
  return { id: stringValue(item.id, `${path}.id`), intent_id: stringValue(item.intent_id, `${path}.intent_id`), lane_key: stringValue(item.lane_key, `${path}.lane_key`), issue_number: numberValue(item.issue_number, `${path}.issue_number`), role: stringValue(item.role, `${path}.role`), expected_head: optionalString(item.expected_head, `${path}.expected_head`), status: stringValue(item.status, `${path}.status`), completion_reason: optionalString(item.completion_reason, `${path}.completion_reason`), worker_id: optionalString(item.worker_id, `${path}.worker_id`), observed_chatgpt_url: optionalString(item.observed_chatgpt_url, `${path}.observed_chatgpt_url`), bound_binding_id: optionalString(item.bound_binding_id, `${path}.bound_binding_id`), updated_at: stringValue(item.updated_at, `${path}.updated_at`) }
}
function parseCommand(value: unknown, path: string): AutopilotCommand {
  const item = record(value, path)
  return { materialization_id: stringValue(item.materialization_id, `${path}.materialization_id`), intent_id: stringValue(item.intent_id, `${path}.intent_id`), lane_key: stringValue(item.lane_key, `${path}.lane_key`), status: stringValue(item.status, `${path}.status`), reason_code: optionalString(item.reason_code, `${path}.reason_code`), workflow_command_id: optionalString(item.workflow_command_id, `${path}.workflow_command_id`), workflow_status: optionalString(item.workflow_status, `${path}.workflow_status`), delivery_command_id: optionalString(item.delivery_command_id, `${path}.delivery_command_id`), delivery_status: optionalString(item.delivery_status, `${path}.delivery_status`), updated_at: stringValue(item.updated_at, `${path}.updated_at`) }
}
function parseWarning(value: unknown, path: string): AutopilotWarning {
  const item = record(value, path)
  return { code: stringValue(item.code, `${path}.code`), intent_id: optionalString(item.intent_id, `${path}.intent_id`), issue_number: optionalNumber(item.issue_number, `${path}.issue_number`), pr_number: optionalNumber(item.pr_number, `${path}.pr_number`), expected_head: optionalString(item.expected_head, `${path}.expected_head`), observed_head: optionalString(item.observed_head, `${path}.observed_head`), message: stringValue(item.message, `${path}.message`) }
}
function parseWave(value: unknown, path: string): AutopilotWave {
  const item = record(value, path)
  return { wave_id: stringValue(item.wave_id, `${path}.wave_id`), status: stringValue(item.status, `${path}.status`), issues: arrayValue(item.issues, `${path}.issues`, (entry, itemPath) => numberValue(entry, itemPath)) }
}

export function parseAutopilotStatus(value: unknown): AutopilotStatus {
  const item = record(value, '$')
  return {
    project_id: numberValue(item.project_id, '$.project_id'), repository: stringValue(item.repository, '$.repository'),
    sync_status: stringValue(item.sync_status, '$.sync_status'), sync_error: optionalString(item.sync_error, '$.sync_error'),
    profile: parseProfile(item.profile, '$.profile'), control: parseControl(item.control, '$.control'),
    active_wave: item.active_wave === undefined || item.active_wave === null ? undefined : parseWave(item.active_wave, '$.active_wave'),
    queue: arrayValue(item.queue, '$.queue', parseQueue), active_leases: arrayValue(item.active_leases, '$.active_leases', parseLease),
    provisioning: arrayValue(item.provisioning, '$.provisioning', parseProvisioning), commands: arrayValue(item.commands, '$.commands', parseCommand),
    circuit_breakers: arrayValue(item.circuit_breakers, '$.circuit_breakers', parseBreaker), warnings: arrayValue(item.warnings, '$.warnings', parseWarning),
    counts: parseCounts(item.counts, '$.counts'), project_hold_reason: optionalString(item.project_hold_reason, '$.project_hold_reason'),
    lead_busy: booleanValue(item.lead_busy, '$.lead_busy'), next_action: stringValue(item.next_action, '$.next_action'), generated_at: stringValue(item.generated_at, '$.generated_at'),
  }
}
