import { ApiError, BackendResponseError } from './api.js'
import { AutopilotStatus, parseAutopilotStatus } from './autopilot-domain.js'

export type AutopilotAction = 'enable' | 'pause' | 'resume' | 'stop'
export type BreakerInput = { expected_revision: number; scope_kind: 'project' | 'lane'; lane_key?: string; reason: string; evidence?: string; expected_head?: string }

async function request(path: string, init: RequestInit = {}): Promise<AutopilotStatus> {
  let response: Response
  try {
    response = await globalThis.fetch(path, { ...init, headers: init.body === undefined ? init.headers : { 'Content-Type': 'application/json', ...init.headers } })
  } catch (error) {
    throw new ApiError(0, error instanceof Error ? error.message : 'Backend is unavailable')
  }
  const text = await response.text()
  let body: unknown
  try {
    body = text.trim() === '' ? undefined : JSON.parse(text)
  } catch (error) {
    throw new BackendResponseError('Backend returned malformed JSON', error)
  }
  if (!response.ok) {
    const message = typeof body === 'object' && body !== null && !Array.isArray(body) && typeof (body as Record<string, unknown>).error === 'string'
      ? String((body as Record<string, unknown>).error)
      : `Backend returned HTTP ${response.status}`
    throw new ApiError(response.status, message)
  }
  return parseAutopilotStatus(body)
}

export const autopilotApi = {
  status: (projectID: number, signal?: AbortSignal): Promise<AutopilotStatus> => request(`/api/projects/${projectID}/autopilot`, { signal }),
  control: (projectID: number, action: AutopilotAction, expectedRevision: number): Promise<AutopilotStatus> => request(`/api/projects/${projectID}/autopilot/${action}`, { method: 'POST', body: JSON.stringify({ expected_revision: expectedRevision }) }),
  tripBreaker: (projectID: number, code: string, input: BreakerInput): Promise<AutopilotStatus> => request(`/api/projects/${projectID}/autopilot/circuit-breakers/${encodeURIComponent(code)}`, { method: 'POST', body: JSON.stringify(input) }),
  transitionBreaker: (projectID: number, breakerID: string, action: 'acknowledge' | 'resolve', expectedRevision: number): Promise<AutopilotStatus> => request(`/api/projects/${projectID}/autopilot/circuit-breakers/${encodeURIComponent(breakerID)}/${action}`, { method: 'POST', body: JSON.stringify({ expected_revision: expectedRevision }) }),
}
