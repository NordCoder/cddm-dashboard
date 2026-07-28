import { BrowserApiClient } from './browser-api.js'
import {
  WorkerRole,
  chatCreationMode,
  chatCreationWorker,
  createWorkerChat,
  routedCreationRole,
} from './chat-bootstrap.js'
import { ProjectState, WorkUnitState } from './domain.js'
import { api } from './app-runtime.js'
import { WorkerLoopApiClient } from './workerloop-api.js'

const browserApi = new BrowserApiClient()
const workerLoopApi = new WorkerLoopApiClient()
const PROJECT_AUTOMATION_INTERVAL_MS = 5_000

export function projectChatCandidates(state: ProjectState): WorkUnitState[] {
  return state.work_units
    .filter((item) => item.route.action === 'dispatch' && (item.route.target_role === 'implementor' || item.route.target_role === 'qa'))
    .sort((left, right) => left.identity.issue_number - right.identity.issue_number)
}

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

export function ProjectChatAutomation(props: { projectID: number }): unknown {
  const attempted = React.useRef(new Set<string>())
  const inFlight = React.useRef(false)

  React.useEffect(() => {
    attempted.current.clear()
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
      void provisionNextProjectChat({ projectID: props.projectID, attempted: attempted.current, signal: controller.signal })
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
  }, [props.projectID])

  return null
}
