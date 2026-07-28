import { BrowserApiClient } from './browser-api.js'
import {
  WorkerRole,
  chatCreationWorker,
  createWorkerChat,
  projectChatCandidates,
  routedCreationRole,
} from './chat-bootstrap.js'
import { WorkUnitState } from './domain.js'
import { api } from './app-runtime.js'
import { ExecutionProfile, WorkerLoopApiClient } from './workerloop-api.js'

const browserApi = new BrowserApiClient()
const workerLoopApi = new WorkerLoopApiClient()
const PROJECT_AUTOMATION_INTERVAL_MS = 5_000

function attemptKey(workUnit: WorkUnitState, role: WorkerRole, bindingVersion: number, chatGPTProjectURL: string): string {
  return `${workUnit.identity.project_id}:${workUnit.identity.issue_number}:${role}:${workUnit.route.lane_key ?? ''}:v${bindingVersion}:${chatGPTProjectURL || 'global'}:${workUnit.route.reason_code}`
}

export async function provisionNextProjectChat(input: {
  projectID: number
  profile: ExecutionProfile
  attempted: Set<string>
  signal?: AbortSignal
}): Promise<{ status: 'unavailable' | 'idle' | 'created' | 'failed'; issueNumber?: number; role?: WorkerRole; reason?: string }> {
  const [state, workers] = await Promise.all([
    api.projectState(input.projectID, input.signal),
    browserApi.workers(input.signal),
  ])
  if (!chatCreationWorker(workers)) return { status: 'unavailable', reason: 'cddm_extension_unavailable' }

  for (const workUnit of projectChatCandidates(state)) {
    const execution = await workerLoopApi.execution(input.projectID, workUnit.identity.issue_number, input.signal)
    const role = routedCreationRole(workUnit, execution.role_bindings)
    if (!role) continue
    const roleBinding = execution.role_bindings.find((item) => item.role === role)
    if (!roleBinding) continue
    const version = roleBinding.binding?.binding_version ?? 0
    const key = attemptKey(workUnit, role, version, input.profile.chatgpt_project_url)
    if (input.attempted.has(key)) continue
    input.attempted.add(key)

    const result = await createWorkerChat({
      projectID: input.projectID,
      issueNumber: workUnit.identity.issue_number,
      role,
      roleBinding,
      workUnit,
      chatGPTProjectURL: input.profile.chatgpt_project_url,
    })
    if (!result.ok) {
      return {
        status: 'failed',
        issueNumber: workUnit.identity.issue_number,
        role,
        reason: result.reason || 'chat_creation_failed',
      }
    }
    return { status: 'created', issueNumber: workUnit.identity.issue_number, role }
  }
  return { status: 'idle' }
}

export async function provisionNextEnabledProjectChat(input: {
  attempted: Set<string>
  signal?: AbortSignal
}): Promise<{ status: 'disabled' | 'unavailable' | 'idle' | 'created' | 'failed'; projectID?: number; issueNumber?: number; role?: WorkerRole; reason?: string }> {
  const projects = await api.projects(input.signal)
  let enabled = false

  for (const project of projects) {
    const profile = await workerLoopApi.profile(project.id, input.signal)
    if (profile.chat_creation_mode !== 'automatic') continue
    enabled = true
    const result = await provisionNextProjectChat({ projectID: project.id, profile, attempted: input.attempted, signal: input.signal })
    if (result.status === 'created' || result.status === 'failed' || result.status === 'unavailable') {
      return { ...result, projectID: project.id }
    }
  }
  return { status: enabled ? 'idle' : 'disabled' }
}

export function ChatAutomationSupervisor(): unknown {
  const attempted = React.useRef(new Set<string>())
  const inFlight = React.useRef(false)

  React.useEffect(() => {
    let active = true
    let timer: number | undefined
    let controller: AbortController | undefined

    const schedule = () => {
      if (!active) return
      timer = globalThis.setTimeout(run, PROJECT_AUTOMATION_INTERVAL_MS)
    }
    const run = () => {
      if (!active || inFlight.current) {
        schedule()
        return
      }
      inFlight.current = true
      controller = new AbortController()
      void provisionNextEnabledProjectChat({ attempted: attempted.current, signal: controller.signal })
        .catch(() => undefined)
        .finally(() => {
          inFlight.current = false
          schedule()
        })
    }

    run()
    return () => {
      active = false
      controller?.abort()
      if (timer !== undefined) globalThis.clearTimeout(timer)
    }
  }, [])

  return null
}
