import { BackendResponseError } from './api.js'
import { paths } from './router.js'
import {
  InternalLink,
  Navigate,
  ProjectBundle,
  ProjectContent,
  WorkspaceBundle,
  WorkspaceContent,
} from './ui.js'
import { api, errorMessage, isAbort, resourceContent, useResource } from './app-runtime.js'

const h = React.createElement

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
    void api.createProject({ owner: normalizedOwner, repository: normalizedRepository })
      .then((project) => props.navigate(paths.project(project.id)))
      .catch((error: unknown) => setFeedback(errorMessage(error)))
      .finally(() => setSubmitting(false))
  }

  return h('form', { className: 'create-project-form', onSubmit: submit },
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
  return resourceContent(resource, (bundle) => WorkspaceContent({ bundle, navigate: props.navigate, createPanel: h(CreateProjectPanel, { navigate: props.navigate }), onRefresh: resource.refresh }), 'Loading workspace…')
}

async function loadProject(projectID: number, signal: AbortSignal): Promise<ProjectBundle> {
  const [project, state] = await Promise.all([api.projectMetadata(projectID, signal), api.projectState(projectID, signal)])
  if (state.project.id !== project.id) throw new BackendResponseError('Malformed backend response: Project metadata and workflow state identities do not match')
  return { project, state }
}

export function ProjectPage(props: { projectID: number; navigate: Navigate }): unknown {
  const resource = useResource<ProjectBundle>(`project:${props.projectID}`, (signal) => loadProject(props.projectID, signal))
  const [syncing, setSyncing] = React.useState(false)
  const [deleting, setDeleting] = React.useState(false)
  const [feedback, setFeedback] = React.useState('')

  const sync = () => {
    if (syncing) return
    setSyncing(true)
    setFeedback('')
    void api.syncProject(props.projectID).then(() => { setFeedback('Read-only synchronization completed.'); resource.refresh() }).catch((error: unknown) => setFeedback(errorMessage(error))).finally(() => setSyncing(false))
  }

  const remove = () => {
    if (deleting || resource.state.kind !== 'ready') return
    const identity = `${resource.state.data.project.owner}/${resource.state.data.project.repository}`
    if (!globalThis.confirm(`Delete Project ${identity}? This removes its isolated persisted snapshots and planning history.`)) return
    setDeleting(true)
    setFeedback('')
    void api.deleteProject(props.projectID).then(() => props.navigate(paths.workspace())).catch((error: unknown) => { setFeedback(errorMessage(error)); setDeleting(false) })
  }

  return resourceContent(resource, (bundle) => h(React.Fragment, null,
    h('div', { className: 'autopilot-entry' }, InternalLink({ href: paths.autopilot(props.projectID), navigate: props.navigate, className: 'button button--primary', children: 'Open Autopilot operations' })),
    ProjectContent({ bundle, navigate: props.navigate, onRefresh: resource.refresh, onSync: sync, syncing, syncFeedback: feedback || undefined, onDelete: remove, deleting }),
  ), 'Loading Project…')
}
