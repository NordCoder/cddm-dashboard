import { BackendResponseError } from './api.js'
import { BrowserApiClient, BrowserWorker } from './browser-api.js'
import { ChatCreationMode, chatCreationMode, chatCreationWorker, setChatCreationMode } from './chat-bootstrap.js'
import { paths } from './router.js'
import {
  Navigate,
  ProjectBundle,
  ProjectContent,
  WorkspaceBundle,
  WorkspaceContent,
} from './ui.js'
import { api, errorMessage, isAbort, resourceContent, useResource } from './app-runtime.js'

const h = React.createElement
const browserApi = new BrowserApiClient()

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

export function WorkspacePage(props: { navigate: Navigate }): unknown {
  const resource = useResource<WorkspaceBundle>('workspace', loadWorkspace)
  return resourceContent(resource, (bundle) => WorkspaceContent({
    bundle,
    navigate: props.navigate,
    createPanel: h(CreateProjectPanel, { navigate: props.navigate }),
    onRefresh: resource.refresh,
  }), 'Loading workspace…')
}

type ProjectPageBundle = ProjectBundle & { workers: BrowserWorker[] }

async function loadProject(projectID: number, signal: AbortSignal): Promise<ProjectPageBundle> {
  const [project, state, workers] = await Promise.all([
    api.projectMetadata(projectID, signal),
    api.projectState(projectID, signal),
    browserApi.workers(signal),
  ])
  if (state.project.id !== project.id) {
    throw new BackendResponseError('Malformed backend response: Project metadata and workflow state identities do not match')
  }
  return { project, state, workers }
}

export function ProjectPage(props: { projectID: number; navigate: Navigate }): unknown {
  const resource = useResource<ProjectPageBundle>(`project:${props.projectID}`, (signal) => loadProject(props.projectID, signal))
  const [syncing, setSyncing] = React.useState(false)
  const [deleting, setDeleting] = React.useState(false)
  const [feedback, setFeedback] = React.useState('')
  const [creationMode, setCreationModeState] = React.useState<ChatCreationMode>(() => chatCreationMode(props.projectID))

  React.useEffect(() => {
    setCreationModeState(chatCreationMode(props.projectID))
  }, [props.projectID])

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

  return resourceContent(resource, (bundle) => {
    const available = Boolean(chatCreationWorker(bundle.workers))
    const automationPanel = h(
      'section',
      { className: 'panel-section' },
      h('div', { className: 'card-topline' }, h('div', null, h('span', { className: 'eyebrow' }, 'Worker sessions'), h('h2', null, 'Automatic chat creation'))),
      h('p', { className: 'muted' }, 'When enabled, this Project route watches every open Work Unit and creates the next missing Implementor or fresh QA chat from the backend route.'),
      h('div', { className: 'segmented', role: 'group', 'aria-label': 'Project worker chat creation mode' },
        h('button', {
          type: 'button',
          className: creationMode === 'manual' ? 'segmented__active' : '',
          onClick: () => {
            setChatCreationMode(props.projectID, 'manual')
            setCreationModeState('manual')
            setFeedback('Worker chat creation set to manual for this Project.')
          },
        }, 'Manual'),
        h('button', {
          type: 'button',
          className: creationMode === 'automatic' ? 'segmented__active' : '',
          disabled: !available,
          onClick: () => {
            setChatCreationMode(props.projectID, 'automatic')
            setCreationModeState('automatic')
            setFeedback('Automatic Implementor and QA chat creation enabled for this Project.')
          },
        }, 'Auto-create Implementor + QA'),
      ),
      available
        ? h('p', { className: 'muted' }, 'Automation remains active while a Dashboard page scoped to this Project is open. Lead chat creation stays explicit.')
        : h('p', { className: 'inline-alert inline-alert--warning' }, 'Reload the updated CDDM extension to enable fresh-chat creation.'),
    )

    return ProjectContent({
      bundle,
      navigate: props.navigate,
      onRefresh: resource.refresh,
      onSync: sync,
      syncing,
      syncFeedback: feedback || undefined,
      automationPanel,
      onDelete: remove,
      deleting,
    })
  }, 'Loading Project…')
}
