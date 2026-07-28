import { BrowserBinding, BrowserWorker } from './browser-api.js'
import { ProjectState, WorkUnitState } from './domain.js'
import { RoleBinding } from './workerloop-api.js'

export type ChatCreationMode = 'manual' | 'automatic'
export type WorkerRole = 'lead' | 'implementor' | 'qa'

export const CDDM_EXTENSION_ID = 'biakfbpkfdpniphmoafgldedkbnjfibp'
const REQUEST_TIMEOUT_MS = 45_000

export type ChatBootstrapResponse = {
  ok: boolean
  reason?: string
  reused?: boolean
  binding?: BrowserBinding
  target?: { kind: string; origin: string; path: string }
}

type ExternalRuntime = {
  lastError?: { message?: string }
  sendMessage: (
    extensionID: string,
    message: unknown,
    callback: (response: unknown) => void,
  ) => void
}

type ChromeLike = { runtime?: ExternalRuntime }

export function chatCreationWorker(workers: BrowserWorker[]): BrowserWorker | undefined {
  return workers.find((worker) => worker.state === 'live' && worker.capabilities.includes('chatgpt_conversation_create'))
}

export function bootstrapPrompt(role: WorkerRole, workUnit: WorkUnitState): string {
  const identity = `${workUnit.identity.owner}/${workUnit.identity.repository}#${workUnit.identity.issue_number}`
  const references = role === 'lead'
    ? '@01-workflow.md\n@cddm-minimal-issue-sizing-standard.md'
    : role === 'implementor'
      ? '@02-implementor-trigger.md\n@gpt-gh-connector-guidelines.md'
      : '@03-qa-trigger.md\n@gpt-gh-connector-guidelines.md'
  const roleLine = role === 'qa'
    ? `This is a fresh independent QA conversation for ${identity}.`
    : `This conversation is initialized as the ${role} lane for ${identity}.`
  return `${references}\n\n${roleLine}\n\nRead the attached role resources. This bootstrap message is not a Dashboard Workflow Command and contains no authority to change the repository. Wait for the next CDDM Dashboard command before performing any work.`
}

export function projectChatCandidates(state: ProjectState): WorkUnitState[] {
  return state.work_units
    .filter((item) => item.route.action === 'dispatch' && (item.route.target_role === 'implementor' || item.route.target_role === 'qa'))
    .sort((left, right) => left.identity.issue_number - right.identity.issue_number)
}

export function routedCreationRole(workUnit: WorkUnitState, bindings: RoleBinding[]): WorkerRole | undefined {
  if (workUnit.route.action !== 'dispatch') return undefined
  const role = workUnit.route.target_role
  if (role !== 'implementor' && role !== 'qa') return undefined
  const current = bindings.find((item) => item.role === role)?.binding
  return current?.enabled && current.readiness === 'ready' ? undefined : role
}

function stableToken(value: string): string {
  let hash = 2166136261
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return (hash >>> 0).toString(16).padStart(8, '0')
}

export function bootstrapRequestID(
  projectID: number,
  issueNumber: number,
  role: WorkerRole,
  laneKey: string,
  bindingVersion: number,
  chatGPTProjectURL = '',
): string {
  const scope = chatGPTProjectURL.trim() || 'global-chatgpt'
  return `chat-p${projectID}-i${issueNumber}-${role}-v${bindingVersion}-${stableToken(`${laneKey}|${scope}`)}`
}

function parseResponse(value: unknown): ChatBootstrapResponse {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return { ok: false, reason: 'extension_response_invalid' }
  const item = value as Record<string, unknown>
  return {
    ok: item.ok === true,
    reason: typeof item.reason === 'string' ? item.reason : undefined,
    reused: item.reused === true,
    binding: typeof item.binding === 'object' && item.binding !== null ? item.binding as BrowserBinding : undefined,
    target: typeof item.target === 'object' && item.target !== null ? item.target as ChatBootstrapResponse['target'] : undefined,
  }
}

export function createWorkerChat(input: {
  projectID: number
  issueNumber: number
  role: WorkerRole
  roleBinding: RoleBinding
  workUnit: WorkUnitState
  chatGPTProjectURL?: string
  chromeApi?: ChromeLike
}): Promise<ChatBootstrapResponse> {
  const runtime = input.chromeApi?.runtime ?? (globalThis as typeof globalThis & { chrome?: ChromeLike }).chrome?.runtime
  if (!runtime?.sendMessage) return Promise.resolve({ ok: false, reason: 'cddm_extension_unavailable' })
  const bindingVersion = input.roleBinding.binding?.binding_version ?? 0
  const chatGPTProjectURL = input.chatGPTProjectURL?.trim() ?? ''
  const requestID = bootstrapRequestID(input.projectID, input.issueNumber, input.role, input.roleBinding.lane_key, bindingVersion, chatGPTProjectURL)
  const payload: Record<string, unknown> = {
    type: 'create-worker-chat',
    request_id: requestID,
    project_id: input.projectID,
    issue_number: input.issueNumber,
    role: input.role,
    expected_lane_key: input.roleBinding.lane_key,
    bootstrap_prompt: bootstrapPrompt(input.role, input.workUnit),
  }
  if (chatGPTProjectURL) payload.chatgpt_project_url = chatGPTProjectURL
  if (input.roleBinding.binding) payload.expected_binding_version = input.roleBinding.binding.binding_version

  return new Promise((resolve) => {
    let settled = false
    const timer = globalThis.setTimeout(() => {
      if (settled) return
      settled = true
      resolve({ ok: false, reason: 'chat_creation_timeout' })
    }, REQUEST_TIMEOUT_MS)
    try {
      runtime.sendMessage(CDDM_EXTENSION_ID, payload, (response) => {
        if (settled) return
        settled = true
        globalThis.clearTimeout(timer)
        const error = runtime.lastError?.message
        resolve(error ? { ok: false, reason: 'cddm_extension_unavailable' } : parseResponse(response))
      })
    } catch {
      if (settled) return
      settled = true
      globalThis.clearTimeout(timer)
      resolve({ ok: false, reason: 'cddm_extension_unavailable' })
    }
  })
}
