import { BrowserApiClient } from './browser-api.js'
import {
  WorkerRole,
  chatCreationMode,
  chatCreationWorker,
  createWorkerChat,
  projectChatCandidates,
  routedCreationRole,
} from './chat-bootstrap.js'
import { WorkUnitState } from './domain.js'
import { api } from './app-runtime.js'
import { WorkerLoopApiClient } from './workerloop-api.js'

const browserApi = new BrowserApiClient()
const workerLoopApi = new WorkerLoopApiClient()
const PROJECT_AUTOMATION_INTERVAL_MS = 5_000

function attemptKey(workUnit: WorkUnitState, role: WorkerRole, bindingVersion: number): string {
  return `${workUnit.identity.project_id}:${workUnit.identity.issue_number}:${role}:${workUnit.route.lane_key ?? ''}:v${bindingVersion}:${workUnit.route.reason_code}`
}

export async function provisionNextProjectChat(input: {
  projectID: number
  attempted: Set<string>
  signal?: AbortSignal
}): Promise<{ status: 'disabled' | 'unavailable' | 'idle' | 'created' | 'failed'; issueNumber?: number; role?: WorkerRole; reason?: string }> {
  if (chatCreationMode(input.projectID) !== 'automatic') return { status: 'disabled' }

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
    const key = attemptKey(workUnit, role, version)
    if (input.attempted.has(key)) continue
    input.attempted.add(key)

    const result = await createWorkerChat({
      projectID: input.projectID,
      issueNumber: workUnit.identity.issue_number,
      role,
      roleBinding,
      workUnit,
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
  const enabled = projects.filter((project) => chatCreationMode(project.id) === 'automatic')
  if (enabled.length === 0) return { status: 'disabled' }

  for (const project of enabled) {
    const result = await provisionNextProjectChat({ projectID: project.id, attempted: input.attempted, signal: input.signal })
    if (result.status === 'created' || result.status === 'failed' || result.status === 'unavailable') {
      return { ...result, projectID: project.id }
    }
  }
  return { status: 'idle' }
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
