import { ApiClient, ApiError, BackendResponseError, PlanningMode } from './api.js'
import { GenerationResult, HealthResponse, PlannerHealth, Project, ProjectState, WorkUnitState, WorkspaceState } from './domain.js'
import { parseRoute, paths, RouteState } from './router.js'
import {
  AppShell,
  EmptyState,
  ErrorState,
  InternalLink,
  LoadingState,
  Navigate,
  PlanLauncher,
  PlanningPageContent,
  PlanReview,
  ProjectBundle,
  ProjectContent,
  PromptEditState,
  reducePromptEdit,
  SettingsContent,
  WorkUnitContent,
  WorkspaceBundle,
  WorkspaceContent,
} from './ui.js'

const h = React.createElement
const api = new ApiClient()
const REFRESH_INTERVAL_MS = 60_000

type ResourceState<T> =
  | { kind: 'loading' }
  | { kind: 'ready'; data: T }
  | { kind: 'error'; message: string }

type Resource<T> = {
  state: ResourceState<T>
  refresh: () => void
}

function errorMessage(error: unknown): string {
  if (error instanceof BackendResponseError) return error.message
  if (error instanceof ApiError) {
    return error.status === 0 ? `Backend unavailable: ${error.message}` : `Backend error (${error.status}): ${error.message}`
  }
  return error instanceof Error ? error.message : 'Unknown dashboard error'
}

function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}

function useResource<T>(key: string, loader: (signal: AbortSignal) => Promise<T>, refreshInterval = REFRESH_INTERVAL_MS): Resource<T> {
  const [state, setState] = React.useState<ResourceState<T>>({ kind: 'loading' })
  const [revision, setRevision] = React.useState(0)

  React.useEffect(() => {
    const controller = new AbortController()
    let active = true
    setState({ kind: 'loading' })

    void loader(controller.signal)
      .then((data) => {
        if (active) setState({ kind: 'ready', data })
      })
      .catch((error: unknown) => {
        if (active && !isAbort(error)) setState({ kind: 'error', message: errorMessage(error) })
      })

    const timer = refreshInterval > 0
      ? globalThis.setInterval(() => setRevision((current) => current + 1), refreshInterval)
      : undefined

    return () => {
      active = false
      controller.abort()
      if (timer !== undefined) globalThis.clearInterval(timer)
    }
  }, [key, revision])

  return {
    state,
    refresh: () => setRevision((current) => current + 1),
  }
}

function useRoute(): [RouteState, Navigate] {
  const [route, setRoute] = React.useState<RouteState>(() => parseRoute(globalThis.location.pathname))

  React.useEffect(() => {
    const onPopState = () => setRoute(parseRoute(globalThis.location.pathname))
    globalThis.addEventListener('popstate', onPopState)
    return () => globalThis.removeEventListener('popstate', onPopState)
  }, [])

  const navigate: Navigate = (path) => {
    if (path === globalThis.location.pathname) return
    globalThis.history.pushState({}, '', path)
    setRoute(parseRoute(path))
    globalThis.scrollTo({ top: 0, behavior: 'auto' })
  }

  return [route, navigate]
}

function resourceContent<T>(resource: Resource<T>, render: (data: T) => unknown, loadingLabel?: string): unknown {
  if (resource.state.kind === 'loading') return LoadingState({ label: loadingLabel })
  if (resource.state.kind === 'error') return ErrorState({ message: resource.state.message, onRetry: resource.refresh })
  return render(resource.state.data)
}

function CreateProjectPanel(props: { navigate: Navigate }): unknown {
  const [owner, setOwner] = React.useState('')
  const [repository, setRepository] = React.useState('')
  const [submitting, setSubmitting] = React.useState(false)
  const [feedback, setFeedback] = React.useState('')

  const submit = (event: { preventDefault(): void }) => {
    event.preventDefault()
    if (submitting) return
    const normalizedOwner = owner.trim()
    const normalizedRepository = repository.trim()
    if (!normalizedOwner || !normalizedRepository) {
      setFeedback('Owner and repository are required.')
      return
    }
    setSubmitting(true)
    setFeedback('')
    void api
      .createProject({ owner: normalizedOwner, repository: normalizedRepository })
      .then((project) => props.navigate(paths.project(project.id)))
      .catch((error: unknown) => setFeedback(errorMessage(error)))
      .finally(() => setSubmitting(false))
  }

  return h(
    'form',
    { className: 'create-project-form', onSubmit: submit },
    h('div', { className: 'form-copy' }, h('h2', null, 'Create Project'), h('p', { className: 'muted' }, 'Repository identity only. GitHub credentials remain backend process configuration.')),
    h('label', null, h('span', null, 'Owner'), h('input', { value: owner, onChange: (event: { currentTarget: HTMLInputElement }) => setOwner(event.currentTarget.value), autoComplete: 'off', placeholder: 'NordCoder', required: true })),
    h('label', null, h('span', null, 'Repository'), h('input', { value: repository, onChange: (event: { currentTarget: HTMLInputElement }) => setRepository(event.currentTarget.value), autoComplete: 'off', placeholder: 'cddm-dashboard', required: true })),
    h('button', { type: 'submit', className: 'button button--primary', disabled: submitting }, submitting ? 'Creating…' : 'Create Project'),
    feedback ? h('p', { className: 'inline-alert inline-alert--danger', role: 'alert' }, feedback) : null,
  )
}

async function loadWorkspace(signal: AbortSignal): Promise<WorkspaceBundle> {
  const [projects, state] = await Promise.all([api.projects(signal), api.workspaceState(signal)])
  try {
    const planner = await api.plannerHealth(signal)
    return { projects, state, planner }
  } catch (error) {
    if (isAbort(error)) throw error
    return { projects, state, plannerError: errorMessage(error) }
  }
}

function WorkspacePage(props: { navigate: Navigate }): unknown {
  const resource = useResource<WorkspaceBundle>('workspace', loadWorkspace)
  return resourceContent(resource, (bundle) => WorkspaceContent({
    bundle,
    navigate: props.navigate,
    createPanel: CreateProjectPanel({ navigate: props.navigate }),
    onRefresh: resource.refresh,
  }), 'Loading workspace…')
}

async function loadProject(projectID: number, signal: AbortSignal): Promise<ProjectBundle> {
  const [project, state] = await Promise.all([api.projectMetadata(projectID, signal), api.projectState(projectID, signal)])
  if (state.project.id !== project.id) {
    throw new BackendResponseError('Malformed backend response: Project metadata and workflow state identities do not match')
  }
  return { project, state }
}

function ProjectPage(props: { projectID: number; navigate: Navigate }): unknown {
  const resource = useResource<ProjectBundle>(`project:${props.projectID}`, (signal) => loadProject(props.projectID, signal))
  const [syncing, setSyncing] = React.useState(false)
  const [deleting, setDeleting] = React.useState(false)
  const [feedback, setFeedback] = React.useState('')

  const sync = () => {
    if (syncing) return
    setSyncing(true)
    setFeedback('')
    void api
      .syncProject(props.projectID)
      .then(() => {
        setFeedback('Read-only synchronization completed.')
        resource.refresh()
      })
      .catch((error: unknown) => setFeedback(errorMessage(error)))
      .finally(() => setSyncing(false))
  }

  const remove = () => {
    if (deleting || resource.state.kind !== 'ready') return
    const identity = `${resource.state.data.project.owner}/${resource.state.data.project.repository}`
    const confirmed = globalThis.confirm(`Delete Project ${identity}? This removes its isolated persisted snapshots and planning history.`)
    if (!confirmed) return
    setDeleting(true)
    setFeedback('')
    void api
      .deleteProject(props.projectID)
      .then(() => props.navigate(paths.workspace()))
      .catch((error: unknown) => {
        setFeedback(errorMessage(error))
        setDeleting(false)
      })
  }

  return resourceContent(resource, (bundle) => ProjectContent({
    bundle,
    navigate: props.navigate,
    onRefresh: resource.refresh,
    onSync: sync,
    syncing,
    syncFeedback: feedback || undefined,
    onDelete: remove,
    deleting,
  }), 'Loading Project…')
}

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

function WorkUnitPage(props: { projectID: number; issueNumber: number; navigate: Navigate }): unknown {
  const resource = useResource<WorkUnitState>(
    `work-unit:${props.projectID}:${props.issueNumber}`,
    (signal) => api.workUnitState(props.projectID, props.issueNumber, signal),
  )
  const generation = usePlanGeneration(props.projectID, props.issueNumber, props.navigate)

  return resourceContent(resource, (workUnit) => WorkUnitContent({
    workUnit,
    navigate: props.navigate,
    onRefresh: resource.refresh,
    launcher: PlanLauncher({
      ownerRequired: workUnit.attention.kind === 'owner_required',
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

function PlanningPage(props: { projectID: number; issueNumber: number; planID?: number; navigate: Navigate }): unknown {
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

type SettingsBundle = { backend: HealthResponse; planner: PlannerHealth }

function SettingsPage(): unknown {
  const resource = useResource<SettingsBundle>('settings', async (signal) => {
    const [backend, planner] = await Promise.all([api.health(signal), api.plannerHealth(signal)])
    return { backend, planner }
  })
  return resourceContent(resource, (bundle) => SettingsContent({ backend: bundle.backend, planner: bundle.planner, onRefresh: resource.refresh }), 'Checking runtime health…')
}

function routeLabel(route: RouteState): string {
  switch (route.kind) {
    case 'workspace': return 'Workspace'
    case 'settings': return 'Settings / Health'
    case 'project': return `Project ${route.projectID}`
    case 'work-unit': return `Project ${route.projectID} · Issue #${route.issueNumber}`
    case 'plans': return `Project ${route.projectID} · #${route.issueNumber} · Plans`
    case 'not-found': return 'Not found'
  }
}

function App(): unknown {
  const [route, navigate] = useRoute()
  let page: unknown
  switch (route.kind) {
    case 'workspace':
      page = WorkspacePage({ navigate })
      break
    case 'project':
      page = ProjectPage({ projectID: route.projectID, navigate })
      break
    case 'work-unit':
      page = WorkUnitPage({ projectID: route.projectID, issueNumber: route.issueNumber, navigate })
      break
    case 'plans':
      page = PlanningPage({ projectID: route.projectID, issueNumber: route.issueNumber, planID: route.planID, navigate })
      break
    case 'settings':
      page = SettingsPage()
      break
    case 'not-found':
      page = h(
        React.Fragment,
        null,
        h('h1', null, 'Page not found'),
        h('p', { className: 'muted' }, `No dashboard route matches ${route.path}.`),
        InternalLink({ href: paths.workspace(), navigate, className: 'button button--primary', children: 'Back to Workspace' }),
      )
      break
  }
  return AppShell({ routeLabel: routeLabel(route), navigate, children: page })
}

function installFatalFallback(root: HTMLElement): void {
  const show = () => {
    root.innerHTML = '<main class="fatal-fallback" role="alert"><strong>Dashboard rendering failed</strong><p>Reload the page. If the problem persists, check the browser console and backend availability.</p></main>'
  }
  globalThis.addEventListener('error', show)
  globalThis.addEventListener('unhandledrejection', show)
}

const rootElement = document.getElementById('root')
if (!(rootElement instanceof HTMLElement)) {
  throw new Error('Missing #root element')
}
installFatalFallback(rootElement)
ReactDOM.createRoot(rootElement).render(React.createElement(React.StrictMode, null, React.createElement(App)))
