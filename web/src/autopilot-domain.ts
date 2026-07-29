import { ValidationError } from './domain.js'

export type AutopilotControl = {
  project_id: number
  revision: number
  last_action: string
  updated_at: string
}

export type AutopilotProfile = {
  project_id: number
  resource_version: string
  methodology_version: string
  result_protocol: string
  delivery_mode: string
  qa_session_mode: string
  auto_merge: boolean
  autonomy_mode: string
  autonomy_state: string
  control_issue_number: number
  max_active_work_units: number
  max_parallel_implementors: number
  max_parallel_qa: number
  chatgpt_project_url?: string
  updated_at: string
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
  project_id: number
  source_result_comment_id: number
  source_command_id: string
  action_id: string
  action_type: string
  repository: string
  issue_number?: number
  role?: string
  pr_number?: number
  expected_head?: string
  expected_previous_head?: string
  reason_code?: string
  decision_category?: string
  wave_id?: string
  priority: number
  lane_key?: string
  status: string
  created_at: string
  updated_at: string
}

export type AutopilotQueueItem = {
  intent: AutopilotIntent
  waiting_reason?: string
}

export type AutopilotLease = {
  lease_id: string
  project_id: number
  lane_key: string
  intent_id: string
  claim_id: string
  lease_owner: string
  status: string
  acquired_at: string
  expires_at: string
  released_at?: string
}

export type AutopilotBreaker = {
  id: string
  project_id: number
  scope_kind: string
  lane_key?: string
  code: string
  reason: string
  recovery_requirements: string
  evidence?: string
  expected_head?: string
  status: string
  occurrence_count: number
  created_at: string
  updated_at: string
  acknowledged_at?: string
  resolved_at?: string
}

export type AutopilotProvisioning = {
  id: string
  project_id: number
  intent_id: string
  lease_id: string
  lane_key: string
  issue_number: number
  role: string
  expected_head?: string
  status: string
  completion_reason?: string
  worker_id?: string
  worker_session_id?: string
  tab_id?: number
  observed_chatgpt_url?: string
  bound_binding_id?: string
  bound_binding_version?: number
  created_at: string
  updated_at: string
}

export type AutopilotCommand = {
  project_id: number
  materialization_id: string
  intent_id: string
  lease_id: string
  provision_request_id: string
  lane_key: string
  issue_number: number
  role: string
  expected_head?: string
  status: string
  reason_code?: string
  workflow_command_id?: string
  workflow_status?: string
  delivery_command_id?: string
  delivery_status?: string
  worker_id?: string
  worker_session_id?: string
  tab_id?: number
  binding_id?: string
  binding_version?: number
  observed_chatgpt_url?: string
  context_hash?: string
  prompt_hash?: string
  updated_at: string
}

export type AutopilotResult = {
  project_id: number
  github_comment_id: number
  issue_number: number
  command_id: string
  role: string
  result: string
  payload_hash: string
  validation_status: string
  validation_reason?: string
  accepted_at?: string
  observed_at: string
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
  project_id: number
  wave_id: string
  control_issue_number: number
  source_command_id: string
  status: string
  issues: number[]
  created_at: string
  updated_at: string
}

export type AutopilotMergeCycle = {
  id: string
  project_id: number
  intent_id: string
  issue_number: number
  pr_number: number
  approved_head: string
  observed_merge_commit?: string
  status: string
  reason_code?: string
  updated_at: string
}

export type AutopilotStatus = {
  project_id: number
  repository: string
  sync_status: string
  sync_error?: string
  profile: AutopilotProfile
  control: AutopilotControl
  active_wave?: AutopilotWave
  intents: AutopilotIntent[]
  queue: AutopilotQueueItem[]
  leases: AutopilotLease[]
  active_leases: AutopilotLease[]
  provisioning: AutopilotProvisioning[]
  commands: AutopilotCommand[]
  results: AutopilotResult[]
  circuit_breakers: AutopilotBreaker[]
  warnings: AutopilotWarning[]
  merge_cycles: AutopilotMergeCycle[]
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

function nonEmptyString(value: unknown, path: string): string {
  const parsed = stringValue(value, path)
  if (parsed.trim() === '') throw new ValidationError(path, 'non-empty string')
  return parsed
}

function optionalString(value: unknown, path: string): string | undefined {
  if (value === undefined || value === null || value === '') return undefined
  return stringValue(value, path)
}

function numberValue(value: unknown, path: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) throw new ValidationError(path, 'finite number')
  return value
}

function integerValue(value: unknown, path: string): number {
  const parsed = numberValue(value, path)
  if (!Number.isInteger(parsed)) throw new ValidationError(path, 'integer')
  return parsed
}

function nonNegativeInteger(value: unknown, path: string): number {
  const parsed = integerValue(value, path)
  if (parsed < 0) throw new ValidationError(path, 'non-negative integer')
  return parsed
}

function positiveInteger(value: unknown, path: string): number {
  const parsed = integerValue(value, path)
  if (parsed <= 0) throw new ValidationError(path, 'positive integer')
  return parsed
}

function optionalPositiveInteger(value: unknown, path: string): number | undefined {
  if (value === undefined || value === null) return undefined
  return positiveInteger(value, path)
}

function booleanValue(value: unknown, path: string): boolean {
  if (typeof value !== 'boolean') throw new ValidationError(path, 'boolean')
  return value
}

function shaValue(value: unknown, path: string): string {
  const parsed = nonEmptyString(value, path)
  if (!/^[0-9a-f]{40}$/i.test(parsed)) throw new ValidationError(path, '40-character Git commit SHA')
  return parsed
}

function optionalSha(value: unknown, path: string): string | undefined {
  if (value === undefined || value === null || value === '') return undefined
  return shaValue(value, path)
}

function requiredArray<T>(value: unknown, path: string, parser: (item: unknown, path: string) => T): T[] {
  if (!Array.isArray(value)) throw new ValidationError(path, 'array')
  return value.map((item, index) => parser(item, `${path}[${index}]`))
}

function same(actual: unknown, expected: unknown, path: string, expectedDescription: string): void {
  if (actual !== expected) throw new ValidationError(path, expectedDescription)
}

function parseControl(value: unknown, path: string): AutopilotControl {
  const item = record(value, path)
  return {
    project_id: positiveInteger(item.project_id, `${path}.project_id`),
    revision: nonNegativeInteger(item.revision, `${path}.revision`),
    last_action: nonEmptyString(item.last_action, `${path}.last_action`),
    updated_at: nonEmptyString(item.updated_at, `${path}.updated_at`),
  }
}

function parseProfile(value: unknown, path: string): AutopilotProfile {
  const item = record(value, path)
  return {
    project_id: positiveInteger(item.project_id, `${path}.project_id`),
    resource_version: nonEmptyString(item.resource_version, `${path}.resource_version`),
    methodology_version: nonEmptyString(item.methodology_version, `${path}.methodology_version`),
    result_protocol: nonEmptyString(item.result_protocol, `${path}.result_protocol`),
    delivery_mode: nonEmptyString(item.delivery_mode, `${path}.delivery_mode`),
    qa_session_mode: nonEmptyString(item.qa_session_mode, `${path}.qa_session_mode`),
    auto_merge: booleanValue(item.auto_merge, `${path}.auto_merge`),
    autonomy_mode: nonEmptyString(item.autonomy_mode, `${path}.autonomy_mode`),
    autonomy_state: nonEmptyString(item.autonomy_state, `${path}.autonomy_state`),
    control_issue_number: nonNegativeInteger(item.control_issue_number, `${path}.control_issue_number`),
    max_active_work_units: positiveInteger(item.max_active_work_units, `${path}.max_active_work_units`),
    max_parallel_implementors: positiveInteger(item.max_parallel_implementors, `${path}.max_parallel_implementors`),
    max_parallel_qa: positiveInteger(item.max_parallel_qa, `${path}.max_parallel_qa`),
    chatgpt_project_url: optionalString(item.chatgpt_project_url, `${path}.chatgpt_project_url`),
    updated_at: nonEmptyString(item.updated_at, `${path}.updated_at`),
  }
}

function parseCounts(value: unknown, path: string): AutopilotCounts {
  const item = record(value, path)
  return {
    pending_intents: nonNegativeInteger(item.pending_intents, `${path}.pending_intents`),
    blocked_intents: nonNegativeInteger(item.blocked_intents, `${path}.blocked_intents`),
    claimed_intents: nonNegativeInteger(item.claimed_intents, `${path}.claimed_intents`),
    active_leases: nonNegativeInteger(item.active_leases, `${path}.active_leases`),
    pending_provisioning: nonNegativeInteger(item.pending_provisioning, `${path}.pending_provisioning`),
    managed_sessions: nonNegativeInteger(item.managed_sessions, `${path}.managed_sessions`),
    active_commands: nonNegativeInteger(item.active_commands, `${path}.active_commands`),
    active_circuit_breakers: nonNegativeInteger(item.active_circuit_breakers, `${path}.active_circuit_breakers`),
    ambiguous_records: nonNegativeInteger(item.ambiguous_records, `${path}.ambiguous_records`),
  }
}

function parseIntent(value: unknown, path: string): AutopilotIntent {
  const item = record(value, path)
  const parsed: AutopilotIntent = {
    intent_id: nonEmptyString(item.intent_id, `${path}.intent_id`),
    project_id: positiveInteger(item.project_id, `${path}.project_id`),
    source_result_comment_id: nonNegativeInteger(item.source_result_comment_id, `${path}.source_result_comment_id`),
    source_command_id: stringValue(item.source_command_id, `${path}.source_command_id`),
    action_id: nonEmptyString(item.action_id, `${path}.action_id`),
    action_type: nonEmptyString(item.action_type, `${path}.action_type`),
    repository: nonEmptyString(item.repository, `${path}.repository`),
    issue_number: optionalPositiveInteger(item.issue_number, `${path}.issue_number`),
    role: optionalString(item.role, `${path}.role`),
    pr_number: optionalPositiveInteger(item.pr_number, `${path}.pr_number`),
    expected_head: optionalSha(item.expected_head, `${path}.expected_head`),
    expected_previous_head: optionalSha(item.expected_previous_head, `${path}.expected_previous_head`),
    reason_code: optionalString(item.reason_code, `${path}.reason_code`),
    decision_category: optionalString(item.decision_category, `${path}.decision_category`),
    wave_id: optionalString(item.wave_id, `${path}.wave_id`),
    priority: integerValue(item.priority, `${path}.priority`),
    lane_key: optionalString(item.lane_key, `${path}.lane_key`),
    status: nonEmptyString(item.status, `${path}.status`),
    created_at: nonEmptyString(item.created_at, `${path}.created_at`),
    updated_at: nonEmptyString(item.updated_at, `${path}.updated_at`),
  }
  if (parsed.action_type === 'dispatch' || parsed.action_type === 'correct' || parsed.action_type === 'merge_candidate') {
    if (parsed.issue_number === undefined) throw new ValidationError(`${path}.issue_number`, 'positive Issue number for Issue-scoped action')
    if (!parsed.lane_key) throw new ValidationError(`${path}.lane_key`, 'non-empty lane identity for Issue-scoped action')
  }
  if ((parsed.action_type === 'dispatch' || parsed.action_type === 'correct') && !parsed.role) {
    throw new ValidationError(`${path}.role`, 'worker role for dispatch or correction')
  }
  if (parsed.pr_number !== undefined && !parsed.expected_head) {
    throw new ValidationError(`${path}.expected_head`, 'exact Candidate Head when PR identity is present')
  }
  if (parsed.action_type === 'merge_candidate') {
    if (parsed.pr_number === undefined) throw new ValidationError(`${path}.pr_number`, 'positive PR number for merge action')
    if (!parsed.expected_head) throw new ValidationError(`${path}.expected_head`, 'exact approved Head for merge action')
  }
  return parsed
}

function parseQueue(value: unknown, path: string): AutopilotQueueItem {
  const item = record(value, path)
  return { intent: parseIntent(item.intent, `${path}.intent`), waiting_reason: optionalString(item.waiting_reason, `${path}.waiting_reason`) }
}

function parseLease(value: unknown, path: string): AutopilotLease {
  const item = record(value, path)
  return {
    lease_id: nonEmptyString(item.lease_id, `${path}.lease_id`),
    project_id: positiveInteger(item.project_id, `${path}.project_id`),
    lane_key: nonEmptyString(item.lane_key, `${path}.lane_key`),
    intent_id: nonEmptyString(item.intent_id, `${path}.intent_id`),
    claim_id: nonEmptyString(item.claim_id, `${path}.claim_id`),
    lease_owner: nonEmptyString(item.lease_owner, `${path}.lease_owner`),
    status: nonEmptyString(item.status, `${path}.status`),
    acquired_at: nonEmptyString(item.acquired_at, `${path}.acquired_at`),
    expires_at: nonEmptyString(item.expires_at, `${path}.expires_at`),
    released_at: optionalString(item.released_at, `${path}.released_at`),
  }
}

function parseBreaker(value: unknown, path: string): AutopilotBreaker {
  const item = record(value, path)
  const parsed: AutopilotBreaker = {
    id: nonEmptyString(item.id, `${path}.id`), project_id: positiveInteger(item.project_id, `${path}.project_id`),
    scope_kind: nonEmptyString(item.scope_kind, `${path}.scope_kind`), lane_key: optionalString(item.lane_key, `${path}.lane_key`),
    code: nonEmptyString(item.code, `${path}.code`), reason: nonEmptyString(item.reason, `${path}.reason`),
    recovery_requirements: nonEmptyString(item.recovery_requirements, `${path}.recovery_requirements`), evidence: optionalString(item.evidence, `${path}.evidence`),
    expected_head: optionalSha(item.expected_head, `${path}.expected_head`), status: nonEmptyString(item.status, `${path}.status`),
    occurrence_count: positiveInteger(item.occurrence_count, `${path}.occurrence_count`), created_at: nonEmptyString(item.created_at, `${path}.created_at`),
    updated_at: nonEmptyString(item.updated_at, `${path}.updated_at`), acknowledged_at: optionalString(item.acknowledged_at, `${path}.acknowledged_at`),
    resolved_at: optionalString(item.resolved_at, `${path}.resolved_at`),
  }
  if (parsed.scope_kind === 'lane' && !parsed.lane_key) throw new ValidationError(`${path}.lane_key`, 'lane identity for lane-scoped breaker')
  return parsed
}

function parseProvisioning(value: unknown, path: string): AutopilotProvisioning {
  const item = record(value, path)
  const parsed: AutopilotProvisioning = {
    id: nonEmptyString(item.id, `${path}.id`), project_id: positiveInteger(item.project_id, `${path}.project_id`),
    intent_id: nonEmptyString(item.intent_id, `${path}.intent_id`), lease_id: nonEmptyString(item.lease_id, `${path}.lease_id`),
    lane_key: nonEmptyString(item.lane_key, `${path}.lane_key`), issue_number: positiveInteger(item.issue_number, `${path}.issue_number`),
    role: nonEmptyString(item.role, `${path}.role`), expected_head: optionalSha(item.expected_head, `${path}.expected_head`),
    status: nonEmptyString(item.status, `${path}.status`), completion_reason: optionalString(item.completion_reason, `${path}.completion_reason`),
    worker_id: optionalString(item.worker_id, `${path}.worker_id`), worker_session_id: optionalString(item.worker_session_id, `${path}.worker_session_id`),
    tab_id: optionalPositiveInteger(item.tab_id, `${path}.tab_id`), observed_chatgpt_url: optionalString(item.observed_chatgpt_url, `${path}.observed_chatgpt_url`),
    bound_binding_id: optionalString(item.bound_binding_id, `${path}.bound_binding_id`), bound_binding_version: optionalPositiveInteger(item.bound_binding_version, `${path}.bound_binding_version`),
    created_at: nonEmptyString(item.created_at, `${path}.created_at`), updated_at: nonEmptyString(item.updated_at, `${path}.updated_at`),
  }
  if (parsed.worker_session_id && !parsed.worker_id) throw new ValidationError(`${path}.worker_id`, 'worker identity when managed session identity is present')
  if (parsed.bound_binding_id && parsed.bound_binding_version === undefined) throw new ValidationError(`${path}.bound_binding_version`, 'positive binding version when binding identity is present')
  if (!parsed.bound_binding_id && parsed.bound_binding_version !== undefined) throw new ValidationError(`${path}.bound_binding_id`, 'binding identity when binding version is present')
  if (parsed.status === 'provisioned') {
    if (!parsed.worker_id) throw new ValidationError(`${path}.worker_id`, 'managed worker identity for provisioned request')
    if (parsed.tab_id === undefined) throw new ValidationError(`${path}.tab_id`, 'positive exact-tab identity for provisioned request')
    if (!parsed.observed_chatgpt_url) throw new ValidationError(`${path}.observed_chatgpt_url`, 'observed ChatGPT target for provisioned request')
    if (!parsed.bound_binding_id) throw new ValidationError(`${path}.bound_binding_id`, 'binding identity for provisioned request')
  }
  return parsed
}

function parseCommand(value: unknown, path: string): AutopilotCommand {
  const item = record(value, path)
  const parsed: AutopilotCommand = {
    project_id: positiveInteger(item.project_id, `${path}.project_id`), materialization_id: nonEmptyString(item.materialization_id, `${path}.materialization_id`),
    intent_id: nonEmptyString(item.intent_id, `${path}.intent_id`), lease_id: nonEmptyString(item.lease_id, `${path}.lease_id`),
    provision_request_id: nonEmptyString(item.provision_request_id, `${path}.provision_request_id`), lane_key: nonEmptyString(item.lane_key, `${path}.lane_key`),
    issue_number: positiveInteger(item.issue_number, `${path}.issue_number`), role: nonEmptyString(item.role, `${path}.role`),
    expected_head: optionalSha(item.expected_head, `${path}.expected_head`), status: nonEmptyString(item.status, `${path}.status`),
    reason_code: optionalString(item.reason_code, `${path}.reason_code`), workflow_command_id: optionalString(item.workflow_command_id, `${path}.workflow_command_id`),
    workflow_status: optionalString(item.workflow_status, `${path}.workflow_status`), delivery_command_id: optionalString(item.delivery_command_id, `${path}.delivery_command_id`),
    delivery_status: optionalString(item.delivery_status, `${path}.delivery_status`), worker_id: optionalString(item.worker_id, `${path}.worker_id`),
    worker_session_id: optionalString(item.worker_session_id, `${path}.worker_session_id`), tab_id: optionalPositiveInteger(item.tab_id, `${path}.tab_id`),
    binding_id: optionalString(item.binding_id, `${path}.binding_id`), binding_version: optionalPositiveInteger(item.binding_version, `${path}.binding_version`),
    observed_chatgpt_url: optionalString(item.observed_chatgpt_url, `${path}.observed_chatgpt_url`), context_hash: optionalString(item.context_hash, `${path}.context_hash`),
    prompt_hash: optionalString(item.prompt_hash, `${path}.prompt_hash`), updated_at: nonEmptyString(item.updated_at, `${path}.updated_at`),
  }
  if (parsed.workflow_status && !parsed.workflow_command_id) throw new ValidationError(`${path}.workflow_command_id`, 'Workflow Command identity when workflow status is present')
  if (parsed.delivery_status && !parsed.delivery_command_id) throw new ValidationError(`${path}.delivery_command_id`, 'delivery command identity when delivery status is present')
  if (parsed.delivery_command_id && !parsed.workflow_command_id) throw new ValidationError(`${path}.workflow_command_id`, 'Workflow Command identity before delivery identity')
  if (parsed.binding_id && parsed.binding_version === undefined) throw new ValidationError(`${path}.binding_version`, 'binding version when binding identity is present')
  if (!parsed.binding_id && parsed.binding_version !== undefined) throw new ValidationError(`${path}.binding_id`, 'binding identity when binding version is present')
  if (parsed.status === 'materialized' || parsed.status === 'completed' || parsed.workflow_command_id || parsed.delivery_command_id) {
    if (!parsed.workflow_command_id) throw new ValidationError(`${path}.workflow_command_id`, 'Workflow Command identity for materialized command')
    if (!parsed.delivery_command_id) throw new ValidationError(`${path}.delivery_command_id`, 'delivery command identity for materialized command')
    if (!parsed.worker_id) throw new ValidationError(`${path}.worker_id`, 'worker identity for materialized command')
    if (!parsed.worker_session_id) throw new ValidationError(`${path}.worker_session_id`, 'managed session identity for materialized command')
    if (parsed.tab_id === undefined) throw new ValidationError(`${path}.tab_id`, 'exact tab identity for materialized command')
    if (!parsed.binding_id) throw new ValidationError(`${path}.binding_id`, 'binding identity for materialized command')
  }
  return parsed
}

function parseResult(value: unknown, path: string): AutopilotResult {
  const item = record(value, path)
  return {
    project_id: positiveInteger(item.project_id, `${path}.project_id`), github_comment_id: positiveInteger(item.github_comment_id, `${path}.github_comment_id`),
    issue_number: positiveInteger(item.issue_number, `${path}.issue_number`), command_id: nonEmptyString(item.command_id, `${path}.command_id`),
    role: nonEmptyString(item.role, `${path}.role`), result: nonEmptyString(item.result, `${path}.result`), payload_hash: nonEmptyString(item.payload_hash, `${path}.payload_hash`),
    validation_status: nonEmptyString(item.validation_status, `${path}.validation_status`), validation_reason: optionalString(item.validation_reason, `${path}.validation_reason`),
    accepted_at: optionalString(item.accepted_at, `${path}.accepted_at`), observed_at: nonEmptyString(item.observed_at, `${path}.observed_at`),
  }
}

function parseWarning(value: unknown, path: string): AutopilotWarning {
  const item = record(value, path)
  return {
    code: nonEmptyString(item.code, `${path}.code`), intent_id: optionalString(item.intent_id, `${path}.intent_id`),
    issue_number: item.issue_number === undefined || item.issue_number === null ? undefined : positiveInteger(item.issue_number, `${path}.issue_number`),
    pr_number: item.pr_number === undefined || item.pr_number === null ? undefined : positiveInteger(item.pr_number, `${path}.pr_number`),
    expected_head: optionalSha(item.expected_head, `${path}.expected_head`), observed_head: optionalSha(item.observed_head, `${path}.observed_head`),
    message: nonEmptyString(item.message, `${path}.message`),
  }
}

function parseWave(value: unknown, path: string): AutopilotWave {
  const item = record(value, path)
  return {
    project_id: positiveInteger(item.project_id, `${path}.project_id`), wave_id: nonEmptyString(item.wave_id, `${path}.wave_id`),
    control_issue_number: positiveInteger(item.control_issue_number, `${path}.control_issue_number`), source_command_id: nonEmptyString(item.source_command_id, `${path}.source_command_id`),
    status: nonEmptyString(item.status, `${path}.status`), issues: requiredArray(item.issues, `${path}.issues`, (entry, itemPath) => positiveInteger(entry, itemPath)),
    created_at: nonEmptyString(item.created_at, `${path}.created_at`), updated_at: nonEmptyString(item.updated_at, `${path}.updated_at`),
  }
}

function parseMergeCycle(value: unknown, path: string): AutopilotMergeCycle {
  const item = record(value, path)
  return {
    id: nonEmptyString(item.id, `${path}.id`), project_id: positiveInteger(item.project_id, `${path}.project_id`),
    intent_id: nonEmptyString(item.intent_id, `${path}.intent_id`), issue_number: positiveInteger(item.issue_number, `${path}.issue_number`),
    pr_number: positiveInteger(item.pr_number, `${path}.pr_number`), approved_head: shaValue(item.approved_head, `${path}.approved_head`),
    observed_merge_commit: optionalSha(item.observed_merge_commit, `${path}.observed_merge_commit`), status: nonEmptyString(item.status, `${path}.status`),
    reason_code: optionalString(item.reason_code, `${path}.reason_code`), updated_at: nonEmptyString(item.updated_at, `${path}.updated_at`),
  }
}

function compareIntent(actual: AutopilotIntent, expected: AutopilotIntent, path: string): void {
  same(actual.project_id, expected.project_id, `${path}.project_id`, 'project identity matching authoritative Intent')
  same(actual.repository, expected.repository, `${path}.repository`, 'repository matching authoritative Intent')
  same(actual.issue_number, expected.issue_number, `${path}.issue_number`, 'Issue identity matching authoritative Intent')
  same(actual.role, expected.role, `${path}.role`, 'role matching authoritative Intent')
  same(actual.pr_number, expected.pr_number, `${path}.pr_number`, 'PR identity matching authoritative Intent')
  same(actual.expected_head, expected.expected_head, `${path}.expected_head`, 'Head identity matching authoritative Intent')
  same(actual.wave_id, expected.wave_id, `${path}.wave_id`, 'Wave identity matching authoritative Intent')
  same(actual.lane_key, expected.lane_key, `${path}.lane_key`, 'lane identity matching authoritative Intent')
  same(actual.status, expected.status, `${path}.status`, 'status matching authoritative Intent')
}

function validateIdentityGraph(status: AutopilotStatus): void {
  same(status.profile.project_id, status.project_id, '$.profile.project_id', 'top-level project identity')
  same(status.control.project_id, status.project_id, '$.control.project_id', 'top-level project identity')
  if (status.profile.autonomy_mode === 'continuous_dashboard_orchestration' && status.profile.control_issue_number <= 0) {
    throw new ValidationError('$.profile.control_issue_number', 'positive Control Issue for continuous Autopilot')
  }
  if (status.active_wave) {
    same(status.active_wave.project_id, status.project_id, '$.active_wave.project_id', 'top-level project identity')
    same(status.active_wave.control_issue_number, status.profile.control_issue_number, '$.active_wave.control_issue_number', 'profile Control Issue identity')
  }

  const intentByID = new Map<string, AutopilotIntent>()
  for (const [index, intent] of status.intents.entries()) {
    const path = `$.intents[${index}]`
    if (intentByID.has(intent.intent_id)) throw new ValidationError(`${path}.intent_id`, 'unique Intent identity')
    intentByID.set(intent.intent_id, intent)
    same(intent.project_id, status.project_id, `${path}.project_id`, 'top-level project identity')
    same(intent.repository, status.repository, `${path}.repository`, 'top-level repository identity')
  }
  for (const [index, queueItem] of status.queue.entries()) {
    const path = `$.queue[${index}].intent`
    const intent = intentByID.get(queueItem.intent.intent_id)
    if (!intent) throw new ValidationError(`${path}.intent_id`, 'Intent identity present in authoritative intents')
    compareIntent(queueItem.intent, intent, path)
  }

  const leaseByID = new Map<string, AutopilotLease>()
  for (const [index, lease] of status.leases.entries()) {
    const path = `$.leases[${index}]`
    if (leaseByID.has(lease.lease_id)) throw new ValidationError(`${path}.lease_id`, 'unique durable lease identity')
    leaseByID.set(lease.lease_id, lease)
    same(lease.project_id, status.project_id, `${path}.project_id`, 'top-level project identity')
    const intent = intentByID.get(lease.intent_id)
    if (!intent) throw new ValidationError(`${path}.intent_id`, 'Intent identity present in authoritative intents')
    same(lease.lane_key, intent.lane_key, `${path}.lane_key`, 'lane identity matching referenced Intent')
  }
  for (const [index, active] of status.active_leases.entries()) {
    const path = `$.active_leases[${index}]`
    const lease = leaseByID.get(active.lease_id)
    if (!lease) throw new ValidationError(`${path}.lease_id`, 'lease identity present in durable leases')
    same(active.intent_id, lease.intent_id, `${path}.intent_id`, 'Intent identity matching durable lease')
    same(active.lane_key, lease.lane_key, `${path}.lane_key`, 'lane identity matching durable lease')
    same(active.status, 'active', `${path}.status`, 'active lease status')
    same(lease.status, 'active', `${path}.status`, 'durable lease marked active')
  }

  const provisionByID = new Map<string, AutopilotProvisioning>()
  for (const [index, request] of status.provisioning.entries()) {
    const path = `$.provisioning[${index}]`
    if (provisionByID.has(request.id)) throw new ValidationError(`${path}.id`, 'unique provisioning identity')
    provisionByID.set(request.id, request)
    same(request.project_id, status.project_id, `${path}.project_id`, 'top-level project identity')
    const intent = intentByID.get(request.intent_id)
    if (!intent) throw new ValidationError(`${path}.intent_id`, 'Intent identity present in authoritative intents')
    const lease = leaseByID.get(request.lease_id)
    if (!lease) throw new ValidationError(`${path}.lease_id`, 'lease identity present in durable leases')
    same(lease.intent_id, request.intent_id, `${path}.lease_id`, 'lease belonging to referenced Intent')
    same(request.lane_key, intent.lane_key, `${path}.lane_key`, 'lane identity matching referenced Intent')
    same(request.lane_key, lease.lane_key, `${path}.lane_key`, 'lane identity matching referenced lease')
    same(request.issue_number, intent.issue_number, `${path}.issue_number`, 'Issue identity matching referenced Intent')
    same(request.role, intent.role, `${path}.role`, 'role matching referenced Intent')
    if (request.expected_head || intent.expected_head) same(request.expected_head, intent.expected_head, `${path}.expected_head`, 'Head identity matching referenced Intent')
  }

  const commandByWorkflowID = new Map<string, AutopilotCommand>()
  for (const [index, command] of status.commands.entries()) {
    const path = `$.commands[${index}]`
    same(command.project_id, status.project_id, `${path}.project_id`, 'top-level project identity')
    const intent = intentByID.get(command.intent_id)
    if (!intent) throw new ValidationError(`${path}.intent_id`, 'Intent identity present in authoritative intents')
    const lease = leaseByID.get(command.lease_id)
    if (!lease) throw new ValidationError(`${path}.lease_id`, 'lease identity present in durable leases')
    const request = provisionByID.get(command.provision_request_id)
    if (!request) throw new ValidationError(`${path}.provision_request_id`, 'provisioning identity present in projection')
    same(lease.intent_id, command.intent_id, `${path}.lease_id`, 'lease belonging to referenced Intent')
    same(request.intent_id, command.intent_id, `${path}.provision_request_id`, 'provisioning belonging to referenced Intent')
    same(request.lease_id, command.lease_id, `${path}.lease_id`, 'lease identity matching provisioning')
    same(command.lane_key, intent.lane_key, `${path}.lane_key`, 'lane identity matching referenced Intent')
    same(command.lane_key, lease.lane_key, `${path}.lane_key`, 'lane identity matching referenced lease')
    same(command.lane_key, request.lane_key, `${path}.lane_key`, 'lane identity matching provisioning')
    same(command.issue_number, intent.issue_number, `${path}.issue_number`, 'Issue identity matching referenced Intent')
    same(command.role, intent.role, `${path}.role`, 'role matching referenced Intent')
    same(command.expected_head, request.expected_head, `${path}.expected_head`, 'Head identity matching provisioning')
    same(command.worker_id, request.worker_id, `${path}.worker_id`, 'worker identity matching provisioning')
    same(command.tab_id, request.tab_id, `${path}.tab_id`, 'tab identity matching provisioning')
    same(command.binding_id, request.bound_binding_id, `${path}.binding_id`, 'binding identity matching provisioning')
    same(command.binding_version, request.bound_binding_version, `${path}.binding_version`, 'binding version matching provisioning')
    same(command.observed_chatgpt_url, request.observed_chatgpt_url, `${path}.observed_chatgpt_url`, 'observed target matching provisioning')
    if (command.worker_session_id) same(request.worker_session_id, command.worker_session_id, `${path}.worker_session_id`, 'managed session matching command')
    if (command.workflow_command_id) {
      if (commandByWorkflowID.has(command.workflow_command_id)) throw new ValidationError(`${path}.workflow_command_id`, 'unique Workflow Command identity')
      commandByWorkflowID.set(command.workflow_command_id, command)
    }
  }

  for (const [index, result] of status.results.entries()) {
    const path = `$.results[${index}]`
    same(result.project_id, status.project_id, `${path}.project_id`, 'top-level project identity')
    const command = commandByWorkflowID.get(result.command_id)
    if (!command) throw new ValidationError(`${path}.command_id`, 'Workflow Command identity present in projected commands')
    same(result.issue_number, command.issue_number, `${path}.issue_number`, 'Issue identity matching Workflow Command')
    same(result.role, command.role, `${path}.role`, 'role matching Workflow Command')
  }

  for (const [index, breaker] of status.circuit_breakers.entries()) {
    same(breaker.project_id, status.project_id, `$.circuit_breakers[${index}].project_id`, 'top-level project identity')
  }
  for (const [index, cycle] of status.merge_cycles.entries()) {
    const path = `$.merge_cycles[${index}]`
    same(cycle.project_id, status.project_id, `${path}.project_id`, 'top-level project identity')
    const intent = intentByID.get(cycle.intent_id)
    if (!intent) throw new ValidationError(`${path}.intent_id`, 'Intent identity present in authoritative intents')
    same(cycle.issue_number, intent.issue_number, `${path}.issue_number`, 'Issue identity matching merge Intent')
    same(cycle.pr_number, intent.pr_number, `${path}.pr_number`, 'PR identity matching merge Intent')
    same(cycle.approved_head, intent.expected_head, `${path}.approved_head`, 'approved Head matching merge Intent')
  }
}

export function parseAutopilotStatus(value: unknown): AutopilotStatus {
  const item = record(value, '$')
  const parsed: AutopilotStatus = {
    project_id: positiveInteger(item.project_id, '$.project_id'), repository: nonEmptyString(item.repository, '$.repository'),
    sync_status: nonEmptyString(item.sync_status, '$.sync_status'), sync_error: optionalString(item.sync_error, '$.sync_error'),
    profile: parseProfile(item.profile, '$.profile'), control: parseControl(item.control, '$.control'),
    active_wave: item.active_wave === undefined || item.active_wave === null ? undefined : parseWave(item.active_wave, '$.active_wave'),
    intents: requiredArray(item.intents, '$.intents', parseIntent), queue: requiredArray(item.queue, '$.queue', parseQueue),
    leases: requiredArray(item.leases, '$.leases', parseLease), active_leases: requiredArray(item.active_leases, '$.active_leases', parseLease),
    provisioning: requiredArray(item.provisioning, '$.provisioning', parseProvisioning), commands: requiredArray(item.commands, '$.commands', parseCommand),
    results: requiredArray(item.results, '$.results', parseResult), circuit_breakers: requiredArray(item.circuit_breakers, '$.circuit_breakers', parseBreaker),
    warnings: requiredArray(item.warnings, '$.warnings', parseWarning), merge_cycles: requiredArray(item.merge_cycles, '$.merge_cycles', parseMergeCycle),
    counts: parseCounts(item.counts, '$.counts'), project_hold_reason: optionalString(item.project_hold_reason, '$.project_hold_reason'),
    lead_busy: booleanValue(item.lead_busy, '$.lead_busy'), next_action: nonEmptyString(item.next_action, '$.next_action'),
    generated_at: nonEmptyString(item.generated_at, '$.generated_at'),
  }
  validateIdentityGraph(parsed)
  return parsed
}
