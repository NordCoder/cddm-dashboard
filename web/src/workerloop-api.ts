import { ApiError, BackendResponseError } from './api.js'
import { BrowserBinding, BrowserTarget } from './browser-api.js'
import { ValidationError, Warning } from './domain.js'

export type ExecutionProfile = {
  project_id: number
  resource_version: string
  methodology_version: string
  result_protocol: string
  delivery_mode: 'reviewed' | 'auto'
  qa_session_mode: string
  chat_creation_mode: 'manual' | 'automatic'
  chatgpt_project_url: string
  auto_merge: boolean
  updated_at: string
}

export type WorkflowCommand = {
  command_id: string
  project_id: number
  issue_number: number
  role: string
  action: string
  resource_version: string
  context_hash: string
  expected_head?: string
  status: string
  created_at: string
  completed_at?: string
}

export type DeliveryEvidence = {
  command_id: string
  status: string
  binding_id: string
  binding_version: number
  worker_id: string
  target_role: string
  lane_key: string
  outcome_reason?: string
}

export type WorkerResultView = {
  github_comment_id: number
  role: string
  result: string
  validation_status: string
  validation_reason?: string
  accepted_at?: string
}

export type RoleBinding = { role: string; lane_key: string; binding?: BrowserBinding }

export type WorkUnitExecution = {
  project_id: number
  issue_number: number
  profile: ExecutionProfile
  active_workflow_command?: WorkflowCommand
  delivery?: DeliveryEvidence
  delivery_status: string
  execution_status: string
  worker_result?: WorkerResultView
  validation_status: string
  role_bindings: RoleBinding[]
  next_action: string
}

export type ReadinessCheck = { code: string; ready: boolean; status: string; detail?: string }
export type PilotReadiness = {
  project_id: number
  issue_number: number
  ready: boolean
  status: string
  resource_digest: string
  profile: ExecutionProfile
  checks: ReadinessCheck[]
  protocol_warnings: Warning[]
}

export type RoleBindingInput = {
  expected_binding_version?: number
  worker_id: string
  target: BrowserTarget
}

type RecordValue = Record<string, unknown>
type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

function object(value: unknown, path: string): RecordValue {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new ValidationError(path, 'object')
  return value as RecordValue
}
function text(value: unknown, path: string): string {
  if (typeof value !== 'string') throw new ValidationError(path, 'string')
  return value
}
function optionalText(value: unknown, path: string): string | undefined {
  if (value === undefined || value === null || value === '') return undefined
  return text(value, path)
}
function number(value: unknown, path: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) throw new ValidationError(path, 'finite number')
  return value
}
function bool(value: unknown, path: string): boolean {
  if (typeof value !== 'boolean') throw new ValidationError(path, 'boolean')
  return value
}
function target(value: unknown, path: string): BrowserTarget {
  const item = object(value, path)
  return { kind: text(item.kind, `${path}.kind`), origin: text(item.origin, `${path}.origin`), path: text(item.path, `${path}.path`), label: optionalText(item.label, `${path}.label`) }
}
function binding(value: unknown, path: string): BrowserBinding {
  const item = object(value, path)
  return {
    binding_id: text(item.binding_id, `${path}.binding_id`), binding_version: number(item.binding_version, `${path}.binding_version`),
    project_id: number(item.project_id, `${path}.project_id`), lane_key: text(item.lane_key, `${path}.lane_key`), worker_id: text(item.worker_id, `${path}.worker_id`),
    target: target(item.target, `${path}.target`), enabled: bool(item.enabled, `${path}.enabled`), readiness: text(item.readiness, `${path}.readiness`),
    worker_session_id: optionalText(item.worker_session_id, `${path}.worker_session_id`), presence_token: optionalText(item.presence_token, `${path}.presence_token`),
    last_seen: optionalText(item.last_seen, `${path}.last_seen`), updated_at: text(item.updated_at, `${path}.updated_at`),
  }
}
function profile(value: unknown, path = '$'): ExecutionProfile {
  const item = object(value, path)
  const deliveryMode = text(item.delivery_mode, `${path}.delivery_mode`)
  if (deliveryMode !== 'reviewed' && deliveryMode !== 'auto') throw new ValidationError(`${path}.delivery_mode`, 'reviewed or auto')
  const chatCreationMode = text(item.chat_creation_mode, `${path}.chat_creation_mode`)
  if (chatCreationMode !== 'manual' && chatCreationMode !== 'automatic') throw new ValidationError(`${path}.chat_creation_mode`, 'manual or automatic')
  return {
    project_id: number(item.project_id, `${path}.project_id`), resource_version: text(item.resource_version, `${path}.resource_version`),
    methodology_version: text(item.methodology_version, `${path}.methodology_version`), result_protocol: text(item.result_protocol, `${path}.result_protocol`),
    delivery_mode: deliveryMode, qa_session_mode: text(item.qa_session_mode, `${path}.qa_session_mode`), chat_creation_mode: chatCreationMode,
    chatgpt_project_url: text(item.chatgpt_project_url, `${path}.chatgpt_project_url`),
    auto_merge: bool(item.auto_merge, `${path}.auto_merge`), updated_at: text(item.updated_at, `${path}.updated_at`),
  }
}
function command(value: unknown, path: string): WorkflowCommand {
  const item = object(value, path)
  return {
    command_id: text(item.command_id, `${path}.command_id`), project_id: number(item.project_id, `${path}.project_id`), issue_number: number(item.issue_number, `${path}.issue_number`),
    role: text(item.role, `${path}.role`), action: text(item.action, `${path}.action`), resource_version: text(item.resource_version, `${path}.resource_version`),
    context_hash: text(item.context_hash, `${path}.context_hash`), expected_head: optionalText(item.expected_head, `${path}.expected_head`), status: text(item.status, `${path}.status`),
    created_at: text(item.created_at, `${path}.created_at`), completed_at: optionalText(item.completed_at, `${path}.completed_at`),
  }
}
function delivery(value: unknown, path: string): DeliveryEvidence {
  const item = object(value, path)
  return {
    command_id: text(item.command_id, `${path}.command_id`), status: text(item.status, `${path}.status`), binding_id: text(item.binding_id, `${path}.binding_id`),
    binding_version: number(item.binding_version, `${path}.binding_version`), worker_id: text(item.worker_id, `${path}.worker_id`), target_role: text(item.target_role, `${path}.target_role`),
    lane_key: text(item.lane_key, `${path}.lane_key`), outcome_reason: optionalText(item.outcome_reason, `${path}.outcome_reason`),
  }
}
function result(value: unknown, path: string): WorkerResultView {
  const item = object(value, path)
  return {
    github_comment_id: number(item.github_comment_id, `${path}.github_comment_id`), role: text(item.role, `${path}.role`), result: text(item.result, `${path}.result`),
    validation_status: text(item.validation_status, `${path}.validation_status`), validation_reason: optionalText(item.validation_reason, `${path}.validation_reason`),
    accepted_at: optionalText(item.accepted_at, `${path}.accepted_at`),
  }
}
function roleBinding(value: unknown, path: string): RoleBinding {
  const item = object(value, path)
  return { role: text(item.role, `${path}.role`), lane_key: text(item.lane_key, `${path}.lane_key`), binding: item.binding == null ? undefined : binding(item.binding, `${path}.binding`) }
}
function execution(value: unknown): WorkUnitExecution {
  const item = object(value, '$')
  if (!Array.isArray(item.role_bindings)) throw new ValidationError('$.role_bindings', 'array')
  return {
    project_id: number(item.project_id, '$.project_id'), issue_number: number(item.issue_number, '$.issue_number'), profile: profile(item.profile, '$.profile'),
    active_workflow_command: item.active_workflow_command == null ? undefined : command(item.active_workflow_command, '$.active_workflow_command'),
    delivery: item.delivery == null ? undefined : delivery(item.delivery, '$.delivery'), delivery_status: text(item.delivery_status, '$.delivery_status'),
    execution_status: text(item.execution_status, '$.execution_status'), worker_result: item.worker_result == null ? undefined : result(item.worker_result, '$.worker_result'),
    validation_status: text(item.validation_status, '$.validation_status'), role_bindings: item.role_bindings.map((entry, index) => roleBinding(entry, `$.role_bindings[${index}]`)),
    next_action: text(item.next_action, '$.next_action'),
  }
}
function warning(value: unknown, path: string): Warning {
  const item = object(value, path)
  return { code: text(item.code, `${path}.code`), message: text(item.message, `${path}.message`), comment_id: item.comment_id === undefined ? undefined : number(item.comment_id, `${path}.comment_id`) }
}
function readiness(value: unknown): PilotReadiness {
  const item = object(value, '$')
  if (!Array.isArray(item.checks) || !Array.isArray(item.protocol_warnings)) throw new ValidationError('$', 'readiness arrays')
  return {
    project_id: number(item.project_id, '$.project_id'), issue_number: number(item.issue_number, '$.issue_number'), ready: bool(item.ready, '$.ready'), status: text(item.status, '$.status'),
    resource_digest: text(item.resource_digest, '$.resource_digest'), profile: profile(item.profile, '$.profile'),
    checks: item.checks.map((entry, index) => { const check = object(entry, `$.checks[${index}]`); return { code: text(check.code, `$.checks[${index}].code`), ready: bool(check.ready, `$.checks[${index}].ready`), status: text(check.status, `$.checks[${index}].status`), detail: optionalText(check.detail, `$.checks[${index}].detail`) } }),
    protocol_warnings: item.protocol_warnings.map((entry, index) => warning(entry, `$.protocol_warnings[${index}]`)),
  }
}

function backendMessage(value: unknown, fallback: string): string {
  return typeof value === 'object' && value !== null && !Array.isArray(value) && typeof (value as RecordValue).error === 'string' ? (value as RecordValue).error as string : fallback
}

export class WorkerLoopApiClient {
  constructor(private readonly fetcher: FetchLike = globalThis.fetch.bind(globalThis)) {}

  private async request<T>(path: string, parser: (value: unknown) => T, init?: RequestInit): Promise<T> {
    let response: Response
    try { response = await this.fetcher(path, init) } catch (error) { throw new ApiError(0, error instanceof Error ? error.message : 'Backend is unavailable') }
    const raw = await response.text()
    let body: unknown = undefined
    if (raw.trim()) {
      try { body = JSON.parse(raw) as unknown } catch (error) { throw new BackendResponseError('Backend returned malformed JSON', error) }
    }
    if (!response.ok) throw new ApiError(response.status, backendMessage(body, `Backend returned HTTP ${response.status}`))
    try { return parser(body) } catch (error) { if (error instanceof ValidationError) throw new BackendResponseError(error.message, error); throw error }
  }

  profile(projectID: number, signal?: AbortSignal): Promise<ExecutionProfile> {
    return this.request(`/api/projects/${projectID}/execution-profile`, profile, { signal })
  }
  execution(projectID: number, issueNumber: number, signal?: AbortSignal): Promise<WorkUnitExecution> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/execution`, execution, { signal })
  }
  readiness(projectID: number, issueNumber: number, signal?: AbortSignal): Promise<PilotReadiness> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/pilot-readiness`, readiness, { signal })
  }
  updateProfile(projectID: number, value: ExecutionProfile): Promise<ExecutionProfile> {
    return this.request(`/api/projects/${projectID}/execution-profile`, profile, { method: 'PUT', headers: { 'content-type': 'application/json' }, body: JSON.stringify(value) })
  }
  bindRole(projectID: number, issueNumber: number, role: string, input: RoleBindingInput): Promise<RoleBinding> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/role-bindings/${role}`, (value) => roleBinding(value, '$'), { method: 'PUT', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) })
  }
  disableRole(projectID: number, issueNumber: number, role: string, expectedVersion: number): Promise<RoleBinding> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/role-bindings/${role}`, (value) => roleBinding(value, '$'), { method: 'DELETE', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ expected_binding_version: expectedVersion }) })
  }
}
