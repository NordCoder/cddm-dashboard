import { ApiError, BackendResponseError } from './api.js'
import { ValidationError } from './domain.js'

export type BrowserTarget = { kind: string; origin: string; path: string; label?: string }
export type BrowserWorker = {
  worker_id: string
  protocol_version?: string
  capabilities: string[]
  worker_session_id?: string
  last_seen?: string
  state: string
  target?: BrowserTarget
}
export type BrowserBinding = {
  binding_id: string
  binding_version: number
  project_id: number
  lane_key: string
  worker_id: string
  target: BrowserTarget
  enabled: boolean
  readiness: string
  worker_session_id?: string
  presence_token?: string
  last_seen?: string
  updated_at: string
}
export type BrowserBindingState = { lane_key: string; binding: BrowserBinding | null }
export type BindingInput = { expected_lane_key: string; expected_binding_version?: number; worker_id: string; target: BrowserTarget }
export type DisableBindingInput = { expected_lane_key: string; expected_binding_version: number }
export type DeliveryConfirmationInput = {
  plan_id: number
  idempotency_key: string
  expected_plan_hash: string
  expected_context_hash: string
  expected_head: string
  expected_lane_key: string
  expected_binding_id: string
  expected_binding_version: number
  expected_presence_token: string
}
export type DeliveryCommand = {
  id: string
  project_id: number
  issue_number: number
  plan_id: number
  plan_hash: string
  context_hash: string
  prompt_hash: string
  action: string
  target_role: string
  lane_key: string
  expected_head: string
  binding_id: string
  binding_version: number
  worker_id: string
  worker_session_id: string
  target_kind: string
  target_ref: string
  status: string
  created_at: string
  expires_at: string
  claimed_at?: string
  claim_deadline_at?: string
  claim_id?: string
  terminal_at?: string
  outcome_reason?: string
  outcome_evidence?: string
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
function number(value: unknown, path: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) throw new ValidationError(path, 'finite number')
  return value
}
function bool(value: unknown, path: string): boolean {
  if (typeof value !== 'boolean') throw new ValidationError(path, 'boolean')
  return value
}
function optionalText(value: unknown, path: string): string | undefined {
  if (value === undefined || value === null || value === '') return undefined
  return text(value, path)
}
function stringArray(value: unknown, path: string): string[] {
  if (value === undefined || value === null) return []
  if (!Array.isArray(value)) throw new ValidationError(path, 'array')
  return value.map((item, index) => text(item, `${path}[${index}]`))
}
function target(value: unknown, path: string): BrowserTarget {
  const item = object(value, path)
  return { kind: text(item.kind, `${path}.kind`), origin: text(item.origin, `${path}.origin`), path: text(item.path, `${path}.path`), label: optionalText(item.label, `${path}.label`) }
}
function optionalTarget(value: unknown, path: string): BrowserTarget | undefined {
  if (value === undefined || value === null) return undefined
  return target(value, path)
}
function worker(value: unknown, path: string): BrowserWorker {
  const item = object(value, path)
  return {
    worker_id: text(item.worker_id, `${path}.worker_id`),
    protocol_version: optionalText(item.protocol_version, `${path}.protocol_version`),
    capabilities: stringArray(item.capabilities, `${path}.capabilities`),
    worker_session_id: optionalText(item.worker_session_id, `${path}.worker_session_id`),
    last_seen: optionalText(item.last_seen, `${path}.last_seen`),
    state: text(item.state, `${path}.state`),
    target: optionalTarget(item.target, `${path}.target`),
  }
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
function command(value: unknown, path: string): DeliveryCommand {
  const item = object(value, path)
  return {
    id: text(item.id, `${path}.id`), project_id: number(item.project_id, `${path}.project_id`), issue_number: number(item.issue_number, `${path}.issue_number`), plan_id: number(item.plan_id, `${path}.plan_id`),
    plan_hash: text(item.plan_hash, `${path}.plan_hash`), context_hash: text(item.context_hash, `${path}.context_hash`), prompt_hash: text(item.prompt_hash, `${path}.prompt_hash`),
    action: text(item.action, `${path}.action`), target_role: text(item.target_role, `${path}.target_role`), lane_key: text(item.lane_key, `${path}.lane_key`), expected_head: text(item.expected_head, `${path}.expected_head`),
    binding_id: text(item.binding_id, `${path}.binding_id`), binding_version: number(item.binding_version, `${path}.binding_version`), worker_id: text(item.worker_id, `${path}.worker_id`), worker_session_id: text(item.worker_session_id, `${path}.worker_session_id`),
    target_kind: text(item.target_kind, `${path}.target_kind`), target_ref: text(item.target_ref, `${path}.target_ref`), status: text(item.status, `${path}.status`), created_at: text(item.created_at, `${path}.created_at`), expires_at: text(item.expires_at, `${path}.expires_at`),
    claimed_at: optionalText(item.claimed_at, `${path}.claimed_at`), claim_deadline_at: optionalText(item.claim_deadline_at, `${path}.claim_deadline_at`), claim_id: optionalText(item.claim_id, `${path}.claim_id`), terminal_at: optionalText(item.terminal_at, `${path}.terminal_at`),
    outcome_reason: optionalText(item.outcome_reason, `${path}.outcome_reason`), outcome_evidence: optionalText(item.outcome_evidence, `${path}.outcome_evidence`),
  }
}

function backendMessage(value: unknown, fallback: string): string {
  if (typeof value === 'object' && value !== null && !Array.isArray(value) && typeof (value as RecordValue).error === 'string') return (value as RecordValue).error as string
  return fallback
}

function ambiguousConfirmationError(error: unknown): ApiError | null {
  if (error instanceof ApiError && (error.status === 0 || error.status === 408 || error.status >= 500)) return new ApiError(0, error.message)
  if (error instanceof BackendResponseError) return new ApiError(0, error.message)
  return null
}

export class BrowserApiClient {
  constructor(private readonly fetcher: FetchLike = globalThis.fetch.bind(globalThis)) {}

  private async request<T>(path: string, parser: (value: unknown) => T, init?: RequestInit): Promise<T> {
    let response: Response
    try { response = await this.fetcher(path, init) } catch (error) { throw new ApiError(0, error instanceof Error ? error.message : 'Backend is unavailable') }
    let raw: string
    try {
      raw = await response.text()
    } catch (error) {
      throw new BackendResponseError('Backend response body could not be read', error)
    }
    let body: unknown = undefined
    if (raw.trim()) {
      try { body = JSON.parse(raw) as unknown } catch (error) {
        if (!response.ok) throw new ApiError(response.status, `Backend returned HTTP ${response.status}`)
        throw new BackendResponseError('Backend returned malformed JSON', error)
      }
    }
    if (!response.ok) throw new ApiError(response.status, backendMessage(body, `Backend returned HTTP ${response.status}`))
    try { return parser(body) } catch (error) {
      if (error instanceof ValidationError) throw new BackendResponseError(error.message, error)
      throw error
    }
  }

  workers(signal?: AbortSignal): Promise<BrowserWorker[]> {
    return this.request('/api/browser/workers', (value) => {
      const root = object(value, '$')
      if (!Array.isArray(root.workers)) throw new ValidationError('$.workers', 'array')
      return root.workers.map((item, index) => worker(item, `$.workers[${index}]`))
    }, { signal })
  }

  browserBinding(projectID: number, issueNumber: number, signal?: AbortSignal): Promise<BrowserBindingState> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/browser-binding`, (value) => {
      const root = object(value, '$')
      if (Object.prototype.hasOwnProperty.call(root, 'binding')) {
        const lane = text(root.lane_key, '$.lane_key')
        return { lane_key: lane, binding: root.binding == null ? null : binding(root.binding, '$.binding') }
      }
      const current = binding(root, '$')
      return { lane_key: current.lane_key, binding: current }
    }, { signal })
  }

  bind(projectID: number, issueNumber: number, input: BindingInput): Promise<BrowserBinding> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/browser-binding`, (value) => binding(value, '$'), {
      method: 'PUT', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input),
    })
  }

  disableBinding(projectID: number, issueNumber: number, input: DisableBindingInput): Promise<BrowserBinding> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/browser-binding`, (value) => binding(value, '$'), {
      method: 'DELETE', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input),
    })
  }

  deliveries(projectID: number, issueNumber: number, signal?: AbortSignal): Promise<DeliveryCommand[]> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/deliveries`, (value) => {
      const root = object(value, '$')
      if (!Array.isArray(root.deliveries)) throw new ValidationError('$.deliveries', 'array')
      return root.deliveries.map((item, index) => command(item, `$.deliveries[${index}]`))
    }, { signal })
  }

  async confirm(projectID: number, issueNumber: number, input: DeliveryConfirmationInput): Promise<DeliveryCommand> {
    try {
      return await this.request(`/api/projects/${projectID}/work-units/${issueNumber}/deliveries`, (value) => command(value, '$'), {
        method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input),
      })
    } catch (error) {
      const ambiguous = ambiguousConfirmationError(error)
      if (ambiguous) throw ambiguous
      throw error
    }
  }
}
