import { ApiClient, BackendResponseError, PlanningMode } from './api.js'
import { BrowserApiClient, BrowserWorker } from './browser-api.js'
import { WorkerRole, chatCreationWorker, createWorkerChat } from './chat-bootstrap.js'
import { GenerationResult, WorkUnitState } from './domain.js'
import { paths } from './router.js'
import {
  Navigate,
  PlanLauncher,
  PlanningPageContent,
  PlanReview,
  PromptEditState,
  reducePromptEdit,
  WorkUnitContent,
} from './ui.js'
import { api, errorMessage, resourceContent, useResource } from './app-runtime.js'
import { PilotReadiness, WorkerLoopApiClient, WorkUnitExecution } from './workerloop-api.js'

const workerLoopApi = new WorkerLoopApiClient()
const browserApi = new BrowserApiClient()

function usePlanGeneration(projectID: number, issueNumber: number, navigate: Navigate, afterGenerate?: () => void): {
  mode: PlanningMode
  setMode: (mode: PlanningMode) => void
  generating: boolean
  error?: string
  generate: () => void
} {
  const [mode, setMode] = React.useState<PlanningMode>('opencode')
  const [generating, setGenerating] = React.useState(false)
  const [generationError, setGenerationError] = React.useState('')
  const generationInFlight = React.useRef(false)

  const generate = () => {
    if (generationInFlight.current) return
    generationInFlight.current = true
    setGenerating(true)
    setGenerationError('')
    void api
      .generatePlan(projectID, issueNumber, mode)
      .then((result) => {
        afterGenerate?.()
        if (result.plan_id) navigate(paths.plan(projectID, issueNumber, result.plan_id))
        else navigate(paths.plans(projectID, issueNumber))
      })
      .catch((error: unknown) => setGenerationError(errorMessage(error)))
      .finally(() => {
        generationInFlight.current = false
        setGenerating(false)
      })
  }

  return { mode, setMode, generating, error: generationError || undefined, generate }
}

type WorkUnitBundle = {
  workUnit: WorkUnitState
  execution: WorkUnitExecution
  readiness: PilotReadiness
  workers: BrowserWorker[]
}

async function loadWorkUnit(projectID: number, issueNumber: number, signal: AbortSignal): Promise<WorkUnitBundle> {
  const [workUnit, execution, readiness, workers] = await Promise.all([
    api.workUnitState(projectID, issueNumber, signal),
    workerLoopApi.execution(projectID, issueNumber, signal),
    workerLoopApi.readiness(projectID, issueNumber, signal),
    browserApi.workers(signal),
  ])
  if (workUnit.identity.project_id !== execution.project_id || workUnit.identity.issue_number !== execution.issue_number) {
    throw new BackendResponseError('Malformed backend response: worker-loop execution identity does not match the requested work unit')
  }
  return { workUnit, execution, readiness, workers }
}

export function WorkUnitPage(props: { projectID: number; issueNumber: number; navigate: Navigate }): unknown {
  const resource = useResource<WorkUnitBundle>(
    `work-unit:${props.projectID}:${props.issueNumber}`,
    (signal) => loadWorkUnit(props.projectID, props.issueNumber, signal),
  )
  const generation = usePlanGeneration(props.projectID, props.issueNumber, props.navigate)
  const [mutationBusy, setMutationBusy] = React.useState(false)
  const [mutationFeedback, setMutationFeedback] = React.useState('')
  const creationInFlight = React.useRef(false)

  const mutate = (action: () => Promise<unknown>, success: string) => {
    if (mutationBusy || creationInFlight.current) return
    setMutationBusy(true)
    setMutationFeedback('')
    void action()
      .then(() => {
        setMutationFeedback(success)
        resource.refresh()
      })
      .catch((error: unknown) => setMutationFeedback(errorMessage(error)))
      .finally(() => setMutationBusy(false))
  }

  const provisionChat = (bundle: WorkUnitBundle, role: WorkerRole) => {
    if (mutationBusy || creationInFlight.current) return
    const roleBinding = bundle.execution.role_bindings.find((item) => item.role === role)
    if (!roleBinding) {
      setMutationFeedback(`Chat creation failed: ${role} lane is unavailable.`)
      return
    }
    if (!chatCreationWorker(bundle.workers)) {
      setMutationFeedback('Chat creation failed: reload the updated CDDM extension and refresh browser workers.')
      return
    }
    creationInFlight.current = true
    setMutationBusy(true)
    setMutationFeedback(`Creating a fresh ${role} chat…`)
    void createWorkerChat({
      projectID: props.projectID,
      issueNumber: props.issueNumber,
      role,
      roleBinding,
      workUnit: bundle.workUnit,
      chatGPTProjectURL: bundle.execution.profile.chatgpt_project_url,
    }).then((result) => {
      if (!result.ok) throw new Error(result.reason || 'chat_creation_failed')
      setMutationFeedback(`${role} chat created and bound${result.reused ? ' from the existing bootstrap request' : ''}.`)
      resource.refresh()
    }).catch((error: unknown) => {
      setMutationFeedback(`Chat creation failed: ${errorMessage(error)}`)
    }).finally(() => {
      creationInFlight.current = false
      setMutationBusy(false)
    })
  }

  return resourceContent(resource, (bundle) => WorkUnitContent({
    workUnit: bundle.workUnit,
    execution: bundle.execution,
    readiness: bundle.readiness,
    workers: bundle.workers,
    navigate: props.navigate,
    onRefresh: resource.refresh,
    mutationBusy,
    mutationFeedback: mutationFeedback || undefined,
    chatCreationMode: bundle.execution.profile.chat_creation_mode,
    chatCreationAvailable: Boolean(chatCreationWorker(bundle.workers)),
    onCreateRole: (role) => provisionChat(bundle, role),
    onChatCreationMode: (mode) => mutate(
      () => workerLoopApi.updateProfile(props.projectID, { ...bundle.execution.profile, chat_creation_mode: mode }),
      mode === 'automatic' ? 'Automatic Implementor and QA chat creation enabled for this Project.' : 'Worker chat creation set to manual for this Project.',
    ),
    onBindRole: (role, worker) => {
      if (!worker.target) return
      const current = bundle.execution.role_bindings.find((item) => item.role === role)?.binding
      mutate(
        () => workerLoopApi.bindRole(props.projectID, props.issueNumber, role, {
          expected_binding_version: current?.binding_version,
          worker_id: worker.worker_id,
          target: worker.target!,
        }),
        `${role} binding updated.`,
      )
    },
    onDisableRole: (role) => {
      const current = bundle.execution.role_bindings.find((item) => item.role === role)?.binding
      if (!current) return
      mutate(
        () => workerLoopApi.disableRole(props.projectID, props.issueNumber, role, current.binding_version),
        `${role} binding disabled.`,
      )
    },
    onDeliveryMode: (mode) => mutate(
      () => workerLoopApi.updateProfile(props.projectID, { ...bundle.execution.profile, delivery_mode: mode }),
      `Delivery mode set to ${mode}.`,
    ),
    launcher: PlanLauncher({
      ownerRequired: bundle.workUnit.attention.kind === 'owner_required',
      mode: generation.mode,
      onMode: generation.setMode,
      onGenerate: generation.generate,
      generating: generation.generating,
      error: generation.error,
    }),
  }), 'Loading work unit…')
}

type PlanningBundle = {
  workUnit: WorkUnitState
  current: GenerationResult | null
  history: GenerationResult[]
  context: Awaited<ReturnType<ApiClient['planningContext']>>
}

async function loadPlanning(
  projectID: number,
  issueNumber: number,
  planID: number | undefined,
  signal: AbortSignal,
): Promise<PlanningBundle> {
  const currentPromise = planID === undefined ? api.latestPlan(projectID, issueNumber, signal) : api.plan(projectID, issueNumber, planID, signal)
  const [workUnit, current, history, context] = await Promise.all([
    api.workUnitState(projectID, issueNumber, signal),
    currentPromise,
    api.planHistory(projectID, issueNumber, 30, signal),
    api.planningContext(projectID, issueNumber, signal),
  ])
  if (workUnit.identity.project_id !== context.repository.project_id || workUnit.identity.issue_number !== context.issue.number) {
    throw new BackendResponseError('Malformed backend response: planning context identity does not match the requested work unit')
  }
  return { workUnit, current, history, context }
}

export function PlanningPage(props: { projectID: number; issueNumber: number; planID?: number; navigate: Navigate }): unknown {
  const key = `planning:${props.projectID}:${props.issueNumber}:${props.planID ?? 'latest'}`
  const resource = useResource<PlanningBundle>(key, (signal) => loadPlanning(props.projectID, props.issueNumber, props.planID, signal))
  const generation = usePlanGeneration(props.projectID, props.issueNumber, props.navigate, resource.refresh)
  const [edit, setEdit] = React.useState<PromptEditState>({ generated: '', value: '' })
  const [copyFeedback, setCopyFeedback] = React.useState('')

  const currentIdentity = resource.state.kind === 'ready' && resource.state.data.current
    ? `${resource.state.data.current.plan_id ?? 'none'}:${resource.state.data.current.created_at}`
    : 'none'

  React.useEffect(() => {
    if (resource.state.kind !== 'ready' || !resource.state.data.current?.plan) {
      setEdit({ generated: '', value: '' })
      setCopyFeedback('')
      return
    }
    const generated = resource.state.data.current.plan.prompt
    setEdit({ generated, value: generated })
    setCopyFeedback('')
  }, [currentIdentity])

  const copy = () => {
    if (resource.state.kind !== 'ready' || !resource.state.data.current?.plan) return
    if (!globalThis.navigator.clipboard?.writeText) {
      setCopyFeedback('Copy failed: Clipboard API is unavailable in this browser context.')
      return
    }
    void globalThis.navigator.clipboard
      .writeText(edit.value)
      .then(() => setCopyFeedback('Prompt copied. Manually paste/send it in the intended ChatGPT chat.'))
      .catch((error: unknown) => setCopyFeedback(`Copy failed: ${errorMessage(error)}`))
  }

  return resourceContent(resource, (bundle) => {
    const review = bundle.current
      ? PlanReview({
          result: bundle.current,
          edit,
          onEdit: (value) => setEdit((current) => reducePromptEdit(current, { type: 'edit', value })),
          onReset: () => setEdit((current) => reducePromptEdit(current, { type: 'reset' })),
          onCopy: copy,
          copyFeedback: copyFeedback || undefined,
          ownerRequired: bundle.workUnit.attention.kind === 'owner_required',
        })
      : undefined
    return PlanningPageContent({
      workUnit: bundle.workUnit,
      current: bundle.current,
      history: bundle.history,
      context: bundle.context,
      navigate: props.navigate,
      onRefresh: resource.refresh,
      launcher: PlanLauncher({
        ownerRequired: bundle.workUnit.attention.kind === 'owner_required',
        mode: generation.mode,
        onMode: generation.setMode,
        onGenerate: generation.generate,
        generating: generation.generating,
        error: generation.error,
      }),
      review,
      selectedPlanID: props.planID,
    })
  }, 'Loading Prompt Plans…')
}
