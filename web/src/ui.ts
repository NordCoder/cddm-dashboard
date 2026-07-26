import {
  AttentionItem,
  ContextSummary,
  GenerationResult,
  HealthResponse,
  PlannerHealth,
  Project,
  ProjectState,
  ResultEvidence,
  Warning,
  WorkUnitState,
  WorkspaceState,
} from './domain.js'
import { paths } from './router.js'

const h = React.createElement

export type Navigate = (path: string) => void

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

export type PromptEditState = {
  generated: string
  value: string
}

export type PromptEditAction =
  | { type: 'replace-generation'; generated: string }
  | { type: 'edit'; value: string }
  | { type: 'reset' }

export function reducePromptEdit(state: PromptEditState, action: PromptEditAction): PromptEditState {
  switch (action.type) {
    case 'replace-generation':
      return { generated: action.generated, value: action.generated }
    case 'edit':
      return { generated: state.generated, value: action.value }
    case 'reset':
      return { generated: state.generated, value: state.generated }
  }
}

export function isPromptEdited(state: PromptEditState): boolean {
  return state.value !== state.generated
}

export function shortSha(value?: string): string {
  if (!value) return '—'
  return value.length <= 12 ? value : `${value.slice(0, 12)}…`
}

export function shortHash(value?: string): string {
  if (!value) return '—'
  return value.length <= 16 ? value : `${value.slice(0, 16)}…`
}

export function formatDate(value?: string): string {
  if (!value) return 'Never'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const attentionOrder = new Map<string, number>([
  ['blocked', 0],
  ['owner_required', 1],
  ['ci_failed', 2],
  ['qa_invalidated', 3],
  ['ambiguous', 4],
  ['protocol_warning', 5],
  ['action_required', 6],
  ['waiting', 7],
  ['normal', 8],
  ['terminal', 9],
])

export function sortWorkUnitsForDisplay(workUnits: WorkUnitState[]): WorkUnitState[] {
  return [...workUnits].sort((left, right) => {
    const leftPriority = attentionOrder.get(left.attention.kind) ?? 50
    const rightPriority = attentionOrder.get(right.attention.kind) ?? 50
    return leftPriority - rightPriority || left.identity.issue_number - right.identity.issue_number
  })
}

export function isDispatchReady(result: GenerationResult | null | undefined): boolean {
  if (!result || !result.plan) return false
  return (result.status === 'approved' || result.status === 'fallback') && !result.plan.requires_owner
}

function badgeTone(value: string): string {
  if (['healthy', 'success', 'completed', 'approved', 'fallback', 'normal', 'terminal', 'clean'].includes(value)) {
    return 'positive'
  }
  if (['blocked', 'failed', 'failure', 'rejected', 'planner_error', 'owner_required', 'ci_failed'].includes(value)) {
    return 'danger'
  }
  if (['stale', 'qa_invalidated', 'ambiguous', 'protocol_warning', 'action_required', 'waiting', 'in_progress', 'warning'].includes(value)) {
    return 'warning'
  }
  return 'neutral'
}

function badgeSymbol(tone: string): string {
  if (tone === 'positive') return '✓'
  if (tone === 'danger') return '!'
  if (tone === 'warning') return '△'
  return '•'
}

export function StatusBadge(props: { value: string; label?: string }): unknown {
  const tone = badgeTone(props.value)
  return h(
    'span',
    { className: `status-badge status-badge--${tone}` },
    h('span', { className: 'status-badge__symbol', 'aria-hidden': 'true' }, badgeSymbol(tone)),
    props.label ?? props.value.replaceAll('_', ' '),
  )
}

export function InternalLink(props: { href: string; navigate: Navigate; className?: string; children: unknown }): unknown {
  return h(
    'a',
    {
      href: props.href,
      className: props.className,
      onClick: (event: {
        preventDefault(): void
        button?: number
        metaKey?: boolean
        ctrlKey?: boolean
        shiftKey?: boolean
        altKey?: boolean
      }) => {
        if ((event.button ?? 0) !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
        event.preventDefault()
        props.navigate(props.href)
      },
    },
    props.children,
  )
}

export function ExternalLink(props: { href?: string; children: unknown }): unknown {
  if (!props.href) return h('span', { className: 'muted' }, 'Unavailable')
  return h('a', { href: props.href, target: '_blank', rel: 'noreferrer', className: 'external-link' }, props.children, ' ↗')
}

export function PageHeader(props: { eyebrow: string; title: string; summary?: string; actions?: unknown[] }): unknown {
  return h(
    'header',
    { className: 'page-header' },
    h('div', { className: 'page-header__copy' },
      h('p', { className: 'eyebrow' }, props.eyebrow),
      h('h1', null, props.title),
      props.summary ? h('p', { className: 'page-summary' }, props.summary) : null,
    ),
    props.actions && props.actions.length > 0 ? h('div', { className: 'page-actions' }, ...props.actions) : null,
  )
}

export function LoadingState(props: { label?: string }): unknown {
  return h(
    'section',
    { className: 'state-panel', role: 'status', 'aria-live': 'polite' },
    h('div', { className: 'spinner', 'aria-hidden': 'true' }),
    h('strong', null, props.label ?? 'Loading dashboard…'),
    h('p', { className: 'muted' }, 'Reading canonical state from the backend.'),
  )
}

export function ErrorState(props: { title?: string; message: string; onRetry?: () => void }): unknown {
  return h(
    'section',
    { className: 'state-panel state-panel--error', role: 'alert' },
    h('strong', null, props.title ?? 'Backend unavailable'),
    h('p', null, props.message),
    props.onRetry ? h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRetry }, 'Retry') : null,
  )
}

export function EmptyState(props: { title: string; message: string; action?: unknown }): unknown {
  return h(
    'section',
    { className: 'state-panel' },
    h('strong', null, props.title),
    h('p', { className: 'muted' }, props.message),
    props.action ?? null,
  )
}

export function AppShell(props: { routeLabel: string; navigate: Navigate; children: unknown }): unknown {
  return h(
    'div',
    { className: 'app-shell' },
    h(
      'aside',
      { className: 'sidebar' },
      h('div', { className: 'brand' }, h('span', { className: 'brand__mark', 'aria-hidden': 'true' }, 'CD'), h('span', null, 'CDDM Dashboard')),
      h(
        'nav',
        { className: 'primary-nav', 'aria-label': 'Primary navigation' },
        InternalLink({ href: paths.workspace(), navigate: props.navigate, className: 'nav-link', children: 'Workspace' }),
        InternalLink({ href: paths.settings(), navigate: props.navigate, className: 'nav-link', children: 'Settings / Health' }),
      ),
      h('div', { className: 'sidebar__context' }, h('span', { className: 'eyebrow' }, 'Current view'), h('span', null, props.routeLabel)),
      h('p', { className: 'sidebar__boundary' }, 'Manual prompt delivery only · no ChatGPT DOM access'),
    ),
    h('main', { className: 'main-content', id: 'main-content' }, props.children),
  )
}

function attentionSummary(workUnits: WorkUnitState[]): string {
  const actionable = workUnits.filter((item) => !['normal', 'waiting', 'terminal'].includes(item.attention.kind)).length
  return actionable === 0 ? 'No actionable attention' : `${actionable} need${actionable === 1 ? 's' : ''} attention`
}

function plannerSummary(planner?: PlannerHealth, error?: string): unknown {
  if (error) return h('span', { className: 'inline-alert inline-alert--danger' }, `Planner unavailable: ${error}`)
  if (!planner) return h('span', { className: 'muted' }, 'Planner health pending')
  return h('span', { className: 'inline-status' }, StatusBadge({ value: planner.status }), ` ${planner.runtime}${planner.enabled ? '' : ' disabled'}`, planner.error ? ` · ${planner.error}` : '')
}

const requiredAttentionGroups = [
  'action_required',
  'ci_failed',
  'blocked',
  'owner_required',
  'qa_invalidated',
  'ambiguous',
  'protocol_warning',
] as const

function AttentionQueue(props: { items: AttentionItem[]; navigate: Navigate }): unknown {
  const known = new Set<string>(requiredAttentionGroups)
  const extra = props.items.filter((item) => !known.has(item.attention.kind))
  return h(
    'div',
    { className: 'attention-groups' },
    ...requiredAttentionGroups.map((kind) => {
      const items = props.items.filter((item) => item.attention.kind === kind)
      return h(
        'section',
        { className: 'attention-group', key: kind },
        h('div', { className: 'section-heading section-heading--compact' }, h('h3', null, kind.replaceAll('_', ' ')), h('span', { className: 'count-pill' }, String(items.length))),
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
                    h('strong', null, `${item.project.owner}/${item.project.repository} #${item.work_unit.issue_number}`),
                    h('span', null, item.work_unit.title),
                    h('small', null, item.attention.explanation),
                  ),
                }),
              )),
            ),
      )
    }),
    extra.length > 0
      ? h('section', { className: 'attention-group' }, h('h3', null, 'Other'), h('p', { className: 'muted' }, `${extra.length} additional backend-classified items`))
      : null,
  )
}

export function WorkspaceContent(props: {
  bundle: WorkspaceBundle
  navigate: Navigate
  createPanel: unknown
  onRefresh: () => void
}): unknown {
  const stateByProject = new Map(props.bundle.state.projects.map((project) => [project.project.id, project]))
  const actions = [h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRefresh, key: 'refresh' }, 'Refresh')]
  return h(
    React.Fragment,
    null,
    PageHeader({
      eyebrow: 'Supervisor workspace',
      title: 'Workspace',
      summary: 'Repository state, exact-candidate evidence and attention across every configured Project.',
      actions,
    }),
    h('section', { className: 'health-strip', 'aria-label': 'Planner health' }, h('strong', null, 'Planner'), plannerSummary(props.bundle.planner, props.bundle.plannerError)),
    props.bundle.projects.length === 0
      ? h('div', { className: 'stack-lg' }, EmptyState({ title: 'No Projects yet', message: 'Add the first repository to begin read-only synchronization and supervision.' }), props.createPanel)
      : h(
          React.Fragment,
          null,
          h('div', { className: 'section-heading' }, h('h2', null, 'Repositories'), h('span', { className: 'count-pill' }, String(props.bundle.projects.length))),
          h(
            'div',
            { className: 'project-grid' },
            ...props.bundle.projects.map((project) => {
              const state = stateByProject.get(project.id)
              const workUnits = state?.work_units ?? []
              return h(
                'article',
                { className: 'project-card', key: project.id },
                h('div', { className: 'card-topline' }, StatusBadge({ value: project.sync_status }), h('span', { className: 'muted' }, formatDate(project.last_sync_completed_at))),
                h('h3', { className: 'breakable' }, `${project.owner}/${project.repository}`),
                h('dl', { className: 'metric-row' },
                  h('div', null, h('dt', null, 'Active work units'), h('dd', null, String(workUnits.length))),
                  h('div', null, h('dt', null, 'Attention'), h('dd', null, attentionSummary(workUnits))),
                ),
                project.sync_error ? h('p', { className: 'inline-alert inline-alert--danger' }, project.sync_error) : null,
                InternalLink({ href: paths.project(project.id), navigate: props.navigate, className: 'button button--primary', children: 'Open Project' }),
              )
            }),
          ),
          h('div', { className: 'section-heading' }, h('h2', null, 'Global Attention Queue'), h('span', { className: 'count-pill' }, String(props.bundle.state.attention.length))),
          AttentionQueue({ items: props.bundle.state.attention, navigate: props.navigate }),
          h('details', { className: 'create-project-details' }, h('summary', null, 'Add another Project'), props.createPanel),
        ),
  )
}

function ciLabel(workUnit: WorkUnitState): string {
  return workUnit.ci.conclusion || workUnit.ci.status || 'unknown'
}

function workerResultInline(role: string, result?: ResultEvidence): unknown {
  if (!result) return h('span', { className: 'muted' }, `${role}: none`)
  const detail = result.verdict || result.decision || result.status
  return h('span', { className: 'worker-inline' }, h('strong', null, role), StatusBadge({ value: result.stale ? 'stale' : detail }), result.head ? h('code', null, shortSha(result.head)) : null)
}

function WorkUnitRow(props: { workUnit: WorkUnitState; navigate: Navigate; projectID: number }): unknown {
  const item = props.workUnit
  const candidate = item.candidate.current
  const attentionClass = ['normal', 'waiting', 'terminal'].includes(item.attention.kind) ? '' : ' work-unit-card--attention'
  return h(
    'article',
    { className: `work-unit-card${attentionClass}` },
    h('div', { className: 'work-unit-card__head' },
      h('div', null,
        h('div', { className: 'badge-row' }, StatusBadge({ value: item.attention.kind }), StatusBadge({ value: item.lifecycle })),
        h('h3', null, `#${item.identity.issue_number} ${item.identity.title}`),
      ),
      h('div', { className: 'work-unit-card__route' }, h('span', { className: 'eyebrow' }, 'Next route'), h('strong', null, item.route.action), item.route.target_role ? h('span', null, item.route.target_role) : null),
    ),
    h('div', { className: 'evidence-grid evidence-grid--compact' },
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
    h('section', { className: 'overview-grid' },
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Sync health'), StatusBadge({ value: project.sync_status }), h('p', { className: 'muted' }, `Last completed ${formatDate(project.last_sync_completed_at)}`), project.sync_error ? h('p', { className: 'inline-alert inline-alert--danger' }, project.sync_error) : null),
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Workflow mode'), h('strong', null, project.workflow_mode), h('p', { className: 'muted' }, project.polling_enabled ? `Polling every ${project.poll_interval_seconds}s` : 'Polling disabled')),
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Open work units'), h('strong', { className: 'metric-number' }, String(workUnits.length)), h('p', { className: 'muted' }, `${props.bundle.state.attention.length} attention items`)),
    ),
    props.syncFeedback ? h('p', { className: 'inline-alert', role: 'status' }, props.syncFeedback) : null,
    h('div', { className: 'section-heading' }, h('h2', null, 'Work units'), h('span', { className: 'count-pill' }, String(workUnits.length))),
    workUnits.length === 0
      ? EmptyState({ title: 'No open work units', message: 'Run a sync after the repository has open Issues to populate this Project.' })
      : h('div', { className: 'stack-md' }, ...workUnits.map((item) => WorkUnitRow({ workUnit: item, navigate: props.navigate, projectID: project.id }))),
    h('section', { className: 'danger-zone' }, h('div', null, h('h2', null, 'Danger zone'), h('p', { className: 'muted' }, 'Deleting a Project removes its isolated persisted snapshots and planning audit data.')), h('button', { type: 'button', className: 'button button--danger', onClick: props.onDelete, disabled: props.deleting }, props.deleting ? 'Deleting…' : 'Delete Project')),
  )
}

function WarningList(props: { warnings: Warning[] }): unknown {
  if (props.warnings.length === 0) return h('p', { className: 'muted' }, 'No protocol warnings.')
  return h('ul', { className: 'plain-list' }, ...props.warnings.map((warning, index) => h('li', { key: `${warning.code}:${index}` }, h('strong', null, warning.code), ' — ', warning.message)))
}

function ResultCard(props: { role: string; result?: ResultEvidence }): unknown {
  const result = props.result
  if (!result) return h('article', { className: 'result-card' }, h('h3', null, props.role), h('p', { className: 'muted' }, 'No terminal result recorded.'))
  const primary = result.verdict || result.decision || result.status
  return h(
    'article',
    { className: `result-card${result.stale ? ' result-card--stale' : ''}` },
    h('div', { className: 'card-topline' }, h('h3', null, props.role), StatusBadge({ value: result.stale ? 'stale' : primary })),
    result.stale ? h('p', { className: 'inline-alert inline-alert--warning' }, `Stale ${props.role} result — it is not current-Head authority.`) : null,
    h('dl', { className: 'details-list' },
      h('div', null, h('dt', null, 'Status'), h('dd', null, result.status)),
      result.head ? h('div', null, h('dt', null, 'Head'), h('dd', null, h('code', { className: 'breakable' }, result.head))) : null,
      h('div', null, h('dt', null, 'Recorded'), h('dd', null, formatDate(result.created_at))),
    ),
  )
}

export function PlanLauncher(props: {
  ownerRequired: boolean
  mode: 'opencode' | 'fallback'
  onMode: (mode: 'opencode' | 'fallback') => void
  onGenerate: () => void
  generating: boolean
  error?: string
}): unknown {
  if (props.ownerRequired) {
    return h('section', { className: 'callout callout--danger' }, h('strong', null, 'Owner decision required'), h('p', null, 'Worker plan generation is not presented as a normal dispatch while the backend marks this work unit owner_required.'))
  }
  return h(
    'section',
    { className: 'plan-launcher' },
    h('div', null, h('h2', null, 'Prompt planning'), h('p', { className: 'muted' }, 'Generate a fresh backend plan. Provider/model credentials never enter the browser.')),
    h('div', { className: 'segmented', role: 'group', 'aria-label': 'Planning mode' },
      h('button', { type: 'button', className: props.mode === 'opencode' ? 'segmented__active' : '', onClick: () => props.onMode('opencode'), disabled: props.generating }, 'OpenCode'),
      h('button', { type: 'button', className: props.mode === 'fallback' ? 'segmented__active' : '', onClick: () => props.onMode('fallback'), disabled: props.generating }, 'Fallback'),
    ),
    h('button', { type: 'button', className: 'button button--primary', onClick: props.onGenerate, disabled: props.generating, 'aria-busy': props.generating ? 'true' : 'false' }, props.generating ? 'Generating plan…' : 'Generate plan'),
    props.error ? h('p', { className: 'inline-alert inline-alert--danger', role: 'alert' }, props.error) : null,
  )
}

export function WorkUnitContent(props: {
  workUnit: WorkUnitState
  navigate: Navigate
  onRefresh: () => void
  launcher: unknown
}): unknown {
  const item = props.workUnit
  const candidate = item.candidate.current
  const ownerRequired = item.attention.kind === 'owner_required'
  const evidence = item.parsed_comments.filter((comment) => comment.meaningful).slice(-6).reverse()
  return h(
    React.Fragment,
    null,
    PageHeader({
      eyebrow: `${item.identity.owner}/${item.identity.repository} · Issue #${item.identity.issue_number}`,
      title: item.identity.title,
      summary: 'Authoritative work-unit projection. Routing, candidate validity and policy decisions remain backend-owned.',
      actions: [
        h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRefresh, key: 'refresh' }, 'Refresh'),
        InternalLink({ href: paths.plans(item.identity.project_id, item.identity.issue_number), navigate: props.navigate, className: 'button button--secondary', children: 'Plan history' }),
      ],
    }),
    ownerRequired ? h('section', { className: 'callout callout--danger' }, h('strong', null, 'Owner decision required'), h('p', null, 'This is not a ready worker dispatch. Review the backend route and blocker/evidence before taking manual action.')) : null,
    item.candidate.ambiguous ? h('section', { className: 'callout callout--warning' }, h('strong', null, 'Ambiguous Candidate'), h('p', null, `${item.candidate.alternatives.length} candidate alternatives are recorded. The backend has not selected a healthy single Candidate.`)) : null,
    item.attention.kind === 'qa_invalidated' ? h('section', { className: 'callout callout--warning' }, h('strong', null, 'Changed-Head / QA invalidation'), h('p', null, item.attention.explanation)) : null,
    h('section', { className: 'hero-status' },
      h('div', { className: 'badge-row' }, StatusBadge({ value: item.lifecycle }), StatusBadge({ value: item.attention.kind })),
      h('div', { className: 'hero-status__route' }, h('span', { className: 'label' }, 'Next route'), h('strong', null, item.route.action), item.route.target_role ? h('span', null, `Target: ${item.route.target_role}`) : null, item.route.lane_key ? h('code', { className: 'breakable' }, item.route.lane_key) : null),
    ),
    h('div', { className: 'evidence-grid' },
      h('article', { className: 'detail-card' }, h('span', { className: 'label' }, 'GitHub Issue'), ExternalLink({ href: item.identity.url, children: `#${item.identity.issue_number}` }), h('p', { className: 'breakable' }, item.identity.title)),
      h('article', { className: 'detail-card' }, h('span', { className: 'label' }, 'Candidate PR'), candidate ? ExternalLink({ href: candidate.url, children: `#${candidate.number} ${candidate.title}` }) : h('strong', null, 'No current Candidate'), candidate ? h('p', { className: 'muted' }, `${candidate.draft ? 'Draft' : 'Ready'} · ${candidate.mergeable_state || 'mergeability unknown'}`) : null),
      h('article', { className: 'detail-card' }, h('span', { className: 'label' }, 'Current exact Head'), h('code', { className: 'sha-full' }, item.current_head ?? '—'), item.route.expected_head && item.route.expected_head !== item.current_head ? h('p', { className: 'inline-alert inline-alert--warning' }, `Route expected ${item.route.expected_head}`) : null),
      h('article', { className: 'detail-card' }, h('span', { className: 'label' }, 'Exact-Head CI'), StatusBadge({ value: ciLabel(item) }), h('p', { className: 'muted' }, item.ci.head_sha ? `Head ${shortSha(item.ci.head_sha)} · ${item.ci.source}` : item.ci.source || 'No CI evidence'), item.ci.details_url ? ExternalLink({ href: item.ci.details_url, children: 'Open CI evidence' }) : null),
    ),
    item.active_blocker ? h('section', { className: 'callout callout--danger' }, h('strong', null, 'Active blocker'), h('p', null, `${item.active_blocker.role} reported ${item.active_blocker.status} at ${formatDate(item.active_blocker.created_at)}.`), item.active_blocker.head ? h('code', { className: 'breakable' }, item.active_blocker.head) : null) : null,
    h('div', { className: 'section-heading' }, h('h2', null, 'Latest worker results')),
    h('div', { className: 'results-grid' }, ResultCard({ role: 'Lead', result: item.latest_results.lead }), ResultCard({ role: 'Implementor handoff', result: item.latest_results.implementor }), ResultCard({ role: 'QA verdict', result: item.latest_results.qa })),
    h('div', { className: 'two-column' },
      h('section', { className: 'panel-section' }, h('h2', null, 'Route guards'), item.route.guards.length ? h('ul', { className: 'plain-list' }, ...item.route.guards.map((guard, index) => h('li', { key: index }, guard))) : h('p', { className: 'muted' }, 'No route guards reported.'), h('p', { className: 'muted' }, item.route.reason)),
      h('section', { className: 'panel-section' }, h('h2', null, 'Warnings'), WarningList({ warnings: [...item.warnings, ...item.route.warnings] })),
    ),
    props.launcher,
    h('div', { className: 'section-heading' }, h('h2', null, 'Operational evidence'), h('span', { className: 'count-pill' }, String(evidence.length))),
    evidence.length === 0
      ? EmptyState({ title: 'No meaningful comment evidence', message: 'The backend has no parsed operational evidence for this work unit yet.' })
      : h('div', { className: 'evidence-timeline' }, ...evidence.map((entry) => h('article', { className: 'evidence-entry', key: entry.comment_id }, h('div', { className: 'card-topline' }, h('strong', null, entry.heading || entry.level), h('span', { className: 'muted' }, `${entry.author} · ${formatDate(entry.created_at)}`)), h('p', { className: 'evidence-markdown' }, entry.markdown), ExternalLink({ href: entry.url, children: 'Open source comment' })))),
  )
}

function MetadataItem(label: string, value: unknown): unknown {
  return h('div', null, h('dt', null, label), h('dd', null, value))
}

export function PlanReview(props: {
  result: GenerationResult
  edit: PromptEditState
  onEdit: (value: string) => void
  onReset: () => void
  onCopy: () => void
  copyFeedback?: string
  ownerRequired?: boolean
}): unknown {
  const result = props.result
  const plan = result.plan
  const ready = isDispatchReady(result) && !props.ownerRequired
  const edited = isPromptEdited(props.edit)
  const source = plan?.source.kind ?? 'none'
  const expectedHead = plan?.expected_head || result.context.current_head
  return h(
    'section',
    { className: `plan-review plan-review--${badgeTone(result.status)}` },
    h('div', { className: 'plan-review__header' },
      h('div', null, h('p', { className: 'eyebrow' }, 'Prompt Plan'), h('div', { className: 'badge-row' }, StatusBadge({ value: result.status }), StatusBadge({ value: source }), edited ? StatusBadge({ value: 'warning', label: 'Edited locally' }) : null)),
      h('div', { className: 'plan-review__copy-meta' }, h('span', { className: 'label' }, 'Exact Head before copy'), h('code', { className: 'breakable' }, expectedHead || '—')),
    ),
    !ready ? h('div', { className: 'callout callout--warning' }, h('strong', null, 'Not dispatch-ready'), h('p', null, `Status ${result.status} is shown for review only. Copy is unavailable unless the backend result is approved or fallback and the current work unit does not require Owner action.`)) : null,
    h('dl', { className: 'metadata-grid' },
      MetadataItem('Status', result.status),
      MetadataItem('Source', source),
      MetadataItem('Action', plan?.action ?? result.context.route.action),
      MetadataItem('Target role', plan?.target_role ?? result.context.route.target_role ?? '—'),
      MetadataItem('Lane key', h('code', { className: 'breakable' }, plan?.lane_key ?? result.context.route.lane_key ?? '—')),
      MetadataItem('Expected Head', h('code', { className: 'breakable' }, expectedHead || '—')),
      MetadataItem('Context hash', h('code', { className: 'breakable' }, result.context.context_hash)),
      MetadataItem('Risk', plan?.risk ?? '—'),
      MetadataItem('Confidence', plan ? `${Math.round(plan.confidence * 100)}%` : '—'),
      MetadataItem('Created', formatDate(result.created_at)),
    ),
    plan ? h('section', { className: 'plan-explanation' }, h('h3', null, plan.summary || 'Plan rationale'), h('p', null, plan.reason)) : null,
    h('div', { className: 'two-column' },
      h('section', { className: 'panel-section' }, h('h3', null, 'Guards'), plan?.guards.length ? h('ul', { className: 'plain-list' }, ...plan.guards.map((guard, index) => h('li', { key: index }, guard))) : h('p', { className: 'muted' }, 'No plan guards.')),
      h('section', { className: 'panel-section' }, h('h3', null, 'Prohibited actions'), plan?.prohibited_actions.length ? h('ul', { className: 'plain-list' }, ...plan.prohibited_actions.map((action, index) => h('li', { key: index }, action))) : h('p', { className: 'muted' }, 'No prohibited actions listed.')),
    ),
    result.policy_decision.violations.length > 0
      ? h('section', { className: 'callout callout--danger' }, h('strong', null, 'Policy violations'), h('ul', { className: 'plain-list' }, ...result.policy_decision.violations.map((violation, index) => h('li', { key: index }, `${violation.code}${violation.field ? ` (${violation.field})` : ''}: ${violation.message}`))))
      : null,
    h('section', { className: 'prompt-editor' },
      h('div', { className: 'section-heading section-heading--compact' }, h('div', null, h('h2', null, 'Prompt review'), h('p', { className: 'muted' }, edited ? 'Edited locally — backend PromptPlan and PolicyDecision are unchanged.' : 'Generated prompt — no local edits.')), edited ? h('button', { type: 'button', className: 'button button--secondary', onClick: props.onReset }, 'Reset to generated prompt') : null),
      plan
        ? h('textarea', { value: props.edit.value, onChange: (event: { currentTarget: HTMLTextAreaElement }) => props.onEdit(event.currentTarget.value), rows: 18, spellCheck: false, 'aria-label': 'Prompt text' })
        : h('div', { className: 'state-panel' }, 'No prompt body is available for this generation.'),
      h('div', { className: 'prompt-actions' },
        h('button', { type: 'button', className: 'button button--primary', onClick: props.onCopy, disabled: !ready || !plan }, ready ? 'Copy prompt' : 'Copy unavailable'),
        props.copyFeedback ? h('span', { className: 'copy-feedback', role: 'status', 'aria-live': 'polite' }, props.copyFeedback) : null,
      ),
    ),
  )
}

export function PlanHistory(props: { items: GenerationResult[]; projectID: number; issueNumber: number; navigate: Navigate; selectedPlanID?: number }): unknown {
  if (props.items.length === 0) return EmptyState({ title: 'No plan history', message: 'Generate the first Prompt Plan from this work unit.' })
  return h(
    'div',
    { className: 'history-list' },
    ...props.items.map((item) => {
      const planID = item.plan_id
      const content = h(
        React.Fragment,
        null,
        h('div', { className: 'history-row__top' }, StatusBadge({ value: item.status }), h('span', { className: 'muted' }, formatDate(item.created_at))),
        h('strong', null, item.plan?.source.kind ?? 'no plan source'),
        h('dl', { className: 'history-meta' },
          h('div', null, h('dt', null, 'Expected Head'), h('dd', null, h('code', null, shortSha(item.plan?.expected_head || item.context.current_head)))),
          h('div', null, h('dt', null, 'Context'), h('dd', null, h('code', null, shortHash(item.context.context_hash)))),
        ),
      )
      if (!planID) return h('article', { className: 'history-row', key: item.created_at }, content)
      return InternalLink({
        href: paths.plan(props.projectID, props.issueNumber, planID),
        navigate: props.navigate,
        className: `history-row history-row--link${props.selectedPlanID === planID ? ' history-row--selected' : ''}`,
        children: content,
      })
    }),
  )
}

export function PlanningPageContent(props: {
  workUnit: WorkUnitState
  current: GenerationResult | null
  history: GenerationResult[]
  context?: ContextSummary
  navigate: Navigate
  onRefresh: () => void
  launcher: unknown
  review?: unknown
  selectedPlanID?: number
}): unknown {
  const item = props.workUnit
  return h(
    React.Fragment,
    null,
    PageHeader({
      eyebrow: `${item.identity.owner}/${item.identity.repository} · #${item.identity.issue_number}`,
      title: 'Prompt Plan preview & history',
      summary: 'Review immutable backend generations, optionally edit only the local prompt copy, then copy manually to ChatGPT.',
      actions: [
        InternalLink({ href: paths.workUnit(item.identity.project_id, item.identity.issue_number), navigate: props.navigate, className: 'button button--secondary', children: 'Back to work unit' }),
        h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRefresh, key: 'refresh' }, 'Refresh'),
      ],
    }),
    props.context ? h('section', { className: 'health-strip' }, h('strong', null, 'Current context'), h('code', null, shortHash(props.context.context_hash)), h('span', null, `Head ${shortSha(props.context.current_head)}`), StatusBadge({ value: props.context.issue.attention.kind })) : null,
    props.launcher,
    h('div', { className: 'planning-layout' },
      h('div', { className: 'planning-layout__main' }, props.current && props.review ? props.review : EmptyState({ title: 'No Prompt Plan selected', message: 'Generate a plan or choose a historical generation.' })),
      h('aside', { className: 'planning-layout__history', 'aria-label': 'Prompt plan history' }, h('div', { className: 'section-heading section-heading--compact' }, h('h2', null, 'History'), h('span', { className: 'count-pill' }, String(props.history.length))), PlanHistory({ items: props.history, projectID: item.identity.project_id, issueNumber: item.identity.issue_number, navigate: props.navigate, selectedPlanID: props.selectedPlanID })),
    ),
  )
}

export function SettingsContent(props: { backend: HealthResponse; planner: PlannerHealth; onRefresh: () => void }): unknown {
  return h(
    React.Fragment,
    null,
    PageHeader({
      eyebrow: 'Runtime summary',
      title: 'Settings / Health',
      summary: 'Operational health only. Credentials are process configuration and are never accepted or stored by this browser UI.',
      actions: [h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRefresh, key: 'refresh' }, 'Refresh')],
    }),
    h('div', { className: 'overview-grid' },
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Backend'), StatusBadge({ value: props.backend.status }), h('p', null, `Database: ${props.backend.database}`)),
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Planner runtime'), StatusBadge({ value: props.planner.status }), h('p', null, `${props.planner.runtime}${props.planner.enabled ? '' : ' · disabled'}`), props.planner.error ? h('p', { className: 'inline-alert inline-alert--danger' }, props.planner.error) : null),
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Configured planner identity'), h('strong', null, props.planner.agent || 'No agent'), h('p', { className: 'muted' }, [props.planner.provider, props.planner.model].filter(Boolean).join(' / ') || 'Provider/model not exposed or not configured')),
    ),
    h('section', { className: 'panel-section' }, h('h2', null, 'Stage 5 safety boundary'), h('ul', { className: 'plain-list' }, h('li', null, 'Review plan → optionally edit local copy → Copy prompt → manually paste/send in the intended ChatGPT chat.'), h('li', null, 'No Chrome Extension delivery exists in Stage 5.'), h('li', null, 'The dashboard does not read or interpret ChatGPT DOM/output.'), h('li', null, 'GitHub and OpenCode credentials remain backend/process configuration.'))),
  )
}
