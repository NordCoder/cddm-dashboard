import { AttentionItem, PlannerHealth, Project, ProjectState, ResultEvidence, WorkUnitState, WorkspaceState } from './domain.js'
import { paths } from './router.js'
import {
  EmptyState,
  ExternalLink,
  formatDate,
  InternalLink,
  Navigate,
  PageHeader,
  SectionHeading,
  shortSha,
  sortWorkUnitsForDisplay,
  StatusBadge,
} from './ui-shared.js'

const h = React.createElement

export type WorkspaceBundle = {
  projects: Project[]
  state: WorkspaceState
  planner?: PlannerHealth
  plannerError?: string
}

export type ProjectBundle = {
  project: Project
  state: ProjectState
}

function attentionSummary(workUnits: WorkUnitState[]): string {
  const actionable = workUnits.filter((item) => !['normal', 'waiting', 'terminal'].includes(item.attention.kind)).length
  return actionable === 0 ? 'No actionable attention' : `${actionable} need${actionable === 1 ? 's' : ''} attention`
}

function plannerSummary(planner?: PlannerHealth, error?: string): unknown {
  if (error) return h('span', { className: 'inline-alert inline-alert--danger' }, `Planner unavailable: ${error}`)
  if (!planner) return h('span', { className: 'muted' }, 'Planner health pending')
  return h(
    'span',
    { className: 'inline-status' },
    StatusBadge({ value: planner.status }),
    h('span', null, `${planner.runtime}${planner.enabled ? '' : ' disabled'}`),
    planner.error ? h('span', { className: 'muted' }, planner.error) : null,
  )
}

const attentionGroups = [
  'action_required',
  'ci_failed',
  'blocked',
  'owner_required',
  'qa_invalidated',
  'ambiguous',
  'protocol_warning',
] as const

function AttentionQueue(props: { items: AttentionItem[]; navigate: Navigate }): unknown {
  const known = new Set<string>(attentionGroups)
  const extra = props.items.filter((item) => !known.has(item.attention.kind))
  return h(
    'div',
    { className: 'attention-groups' },
    ...attentionGroups.map((kind) => {
      const items = props.items.filter((item) => item.attention.kind === kind)
      return h(
        'section',
        { className: 'attention-group', key: kind },
        SectionHeading({ title: kind.replaceAll('_', ' '), count: items.length, compact: true }),
        items.length === 0
          ? h('p', { className: 'empty-inline' }, 'No items')
          : h(
              'ul',
              { className: 'attention-list' },
              ...items.map((item) => h(
                'li',
                { key: `${item.project.id}:${item.work_unit.issue_number}` },
                InternalLink({
                  href: paths.workUnit(item.project.id, item.work_unit.issue_number),
                  navigate: props.navigate,
                  className: 'attention-link',
                  children: h(
                    React.Fragment,
                    null,
                    h('span', { className: 'attention-link__identity' }, `${item.project.owner}/${item.project.repository} #${item.work_unit.issue_number}`),
                    h('strong', null, item.work_unit.title),
                    h('small', null, item.attention.explanation),
                  ),
                }),
              )),
            ),
      )
    }),
    extra.length > 0
      ? h('section', { className: 'attention-group' }, SectionHeading({ title: 'Other', count: extra.length, compact: true }), h('p', { className: 'muted' }, 'Additional backend-classified items'))
      : null,
  )
}

function RepositoryCard(props: { project: Project; state?: ProjectState; navigate: Navigate }): unknown {
  const workUnits = props.state?.work_units ?? []
  return h(
    'article',
    { className: 'project-card' },
    h('div', { className: 'card-topline' }, StatusBadge({ value: props.project.sync_status }), h('span', { className: 'muted' }, formatDate(props.project.last_sync_completed_at))),
    h('div', { className: 'project-card__identity' }, h('span', { className: 'eyebrow' }, 'Repository'), h('h3', { className: 'breakable' }, `${props.project.owner}/${props.project.repository}`)),
    h(
      'dl',
      { className: 'metric-row' },
      h('div', null, h('dt', null, 'Active work units'), h('dd', null, String(workUnits.length))),
      h('div', null, h('dt', null, 'Attention'), h('dd', null, attentionSummary(workUnits))),
    ),
    props.project.sync_error ? h('p', { className: 'inline-alert inline-alert--danger' }, props.project.sync_error) : null,
    InternalLink({ href: paths.project(props.project.id), navigate: props.navigate, className: 'button button--primary', children: 'Open Project' }),
  )
}

export function WorkspaceContent(props: {
  bundle: WorkspaceBundle
  navigate: Navigate
  createPanel: unknown
  onRefresh: () => void
}): unknown {
  const stateByProject = new Map(props.bundle.state.projects.map((project) => [project.project.id, project]))
  return h(
    React.Fragment,
    null,
    PageHeader({
      eyebrow: 'Supervisor workspace',
      title: 'Workspace',
      summary: 'Repository state, exact-candidate evidence and attention across every configured Project.',
      actions: [h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRefresh, key: 'refresh' }, 'Refresh')],
    }),
    h('section', { className: 'health-strip', 'aria-label': 'Planner health' }, h('span', { className: 'health-strip__label' }, 'Planner runtime'), plannerSummary(props.bundle.planner, props.bundle.plannerError)),
    props.bundle.projects.length === 0
      ? h('div', { className: 'stack-lg' }, EmptyState({ title: 'No Projects yet', message: 'Add the first repository to begin read-only synchronization and supervision.' }), props.createPanel)
      : h(
          React.Fragment,
          null,
          SectionHeading({ title: 'Repositories', count: props.bundle.projects.length, copy: 'Configured supervision boundaries.' }),
          h(
            'div',
            { className: 'project-grid' },
            ...props.bundle.projects.map((project) => RepositoryCard({ project, state: stateByProject.get(project.id), navigate: props.navigate })),
          ),
          SectionHeading({ title: 'Global Attention Queue', count: props.bundle.state.attention.length, copy: 'Backend-classified work requiring inspection.' }),
          AttentionQueue({ items: props.bundle.state.attention, navigate: props.navigate }),
          h('details', { className: 'create-project-details' }, h('summary', null, 'Add another Project'), props.createPanel),
        ),
  )
}

function ciLabel(workUnit: WorkUnitState): string {
  return workUnit.ci.conclusion || workUnit.ci.status || 'unknown'
}

function workerResultInline(role: string, result?: ResultEvidence): unknown {
  if (!result) return h('span', { className: 'worker-inline worker-inline--empty' }, h('strong', null, role), h('span', { className: 'muted' }, 'No result'))
  const detail = result.verdict || result.decision || result.status
  return h('span', { className: 'worker-inline' }, h('strong', null, role), StatusBadge({ value: result.stale ? 'stale' : detail }), result.head ? h('code', null, shortSha(result.head)) : null)
}

function WorkUnitRow(props: { workUnit: WorkUnitState; navigate: Navigate; projectID: number }): unknown {
  const item = props.workUnit
  const candidate = item.candidate.current
  const needsAttention = !['normal', 'waiting', 'terminal'].includes(item.attention.kind)
  return h(
    'article',
    { className: `work-unit-card${needsAttention ? ' work-unit-card--attention' : ''}` },
    h(
      'div',
      { className: 'work-unit-card__head' },
      h('div', null, h('div', { className: 'badge-row' }, StatusBadge({ value: item.attention.kind }), StatusBadge({ value: item.lifecycle })), h('h3', null, `#${item.identity.issue_number} ${item.identity.title}`)),
      h('div', { className: 'work-unit-card__route' }, h('span', { className: 'eyebrow' }, 'Next route'), h('strong', null, item.route.action), item.route.target_role ? h('span', null, item.route.target_role) : null),
    ),
    h(
      'div',
      { className: 'evidence-grid evidence-grid--compact' },
      h('div', null, h('span', { className: 'label' }, 'Candidate'), candidate ? ExternalLink({ href: candidate.url, children: `PR #${candidate.number}` }) : h('span', null, 'None')),
      h('div', null, h('span', { className: 'label' }, 'Exact Head'), h('code', { title: item.current_head ?? '' }, shortSha(item.current_head))),
      h('div', null, h('span', { className: 'label' }, 'CI'), item.ci.details_url ? ExternalLink({ href: item.ci.details_url, children: ciLabel(item) }) : StatusBadge({ value: ciLabel(item) })),
      h('div', null, h('span', { className: 'label' }, 'Lane'), h('code', { className: 'breakable' }, item.route.lane_key ?? '—')),
    ),
    h('div', { className: 'worker-summary' }, workerResultInline('Lead', item.latest_results.lead), workerResultInline('Implementor', item.latest_results.implementor), workerResultInline('QA', item.latest_results.qa)),
    InternalLink({ href: paths.workUnit(props.projectID, item.identity.issue_number), navigate: props.navigate, className: 'button button--secondary', children: 'Inspect work unit' }),
  )
}

export function ProjectContent(props: {
  bundle: ProjectBundle
  navigate: Navigate
  onRefresh: () => void
  onSync: () => void
  syncing: boolean
  syncFeedback?: string
  automationPanel?: unknown
  onDelete: () => void
  deleting: boolean
}): unknown {
  const project = props.bundle.project
  const workUnits = sortWorkUnitsForDisplay(props.bundle.state.work_units)
  return h(
    React.Fragment,
    null,
    PageHeader({
      eyebrow: 'Project dashboard',
      title: `${project.owner}/${project.repository}`,
      summary: 'Read-only GitHub synchronization plus backend-derived workflow state.',
      actions: [
        h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRefresh, key: 'refresh' }, 'Refresh'),
        h('button', { type: 'button', className: 'button button--primary', onClick: props.onSync, disabled: props.syncing, key: 'sync' }, props.syncing ? 'Syncing…' : 'Sync now'),
      ],
    }),
    h(
      'section',
      { className: 'overview-grid' },
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Sync health'), StatusBadge({ value: project.sync_status }), h('p', { className: 'muted' }, `Last completed ${formatDate(project.last_sync_completed_at)}`), project.sync_error ? h('p', { className: 'inline-alert inline-alert--danger' }, project.sync_error) : null),
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Workflow mode'), h('strong', null, project.workflow_mode), h('p', { className: 'muted' }, project.polling_enabled ? `Polling every ${project.poll_interval_seconds}s` : 'Polling disabled')),
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Open work units'), h('strong', { className: 'metric-number' }, String(workUnits.length)), h('p', { className: 'muted' }, `${props.bundle.state.attention.length} attention items`)),
    ),
    props.automationPanel ?? null,
    props.syncFeedback ? h('p', { className: 'inline-alert', role: 'status' }, props.syncFeedback) : null,
    SectionHeading({ title: 'Work units', count: workUnits.length, copy: 'Sorted by operational attention.' }),
    workUnits.length === 0
      ? EmptyState({ title: 'No open work units', message: 'Run a sync after the repository has open Issues to populate this Project.' })
      : h('div', { className: 'stack-md' }, ...workUnits.map((item) => WorkUnitRow({ workUnit: item, navigate: props.navigate, projectID: project.id }))),
    h('section', { className: 'danger-zone' }, h('div', null, h('h2', null, 'Danger zone'), h('p', { className: 'muted' }, 'Deleting a Project removes its isolated persisted snapshots and planning audit data.')), h('button', { type: 'button', className: 'button button--danger', onClick: props.onDelete, disabled: props.deleting }, props.deleting ? 'Deleting…' : 'Delete Project')),
  )
}
