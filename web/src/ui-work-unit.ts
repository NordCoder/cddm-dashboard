import { ResultEvidence, Warning, WorkUnitState } from './domain.js'
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
  StatusBadge,
} from './ui-shared.js'

const h = React.createElement

function ciLabel(workUnit: WorkUnitState): string {
  return workUnit.ci.conclusion || workUnit.ci.status || 'unknown'
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
    h(
      'dl',
      { className: 'details-list' },
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
    h('div', { className: 'plan-launcher__copy' }, h('span', { className: 'eyebrow' }, 'Planner'), h('h2', null, 'Prompt planning'), h('p', { className: 'muted' }, 'Generate a fresh backend plan. Provider/model credentials never enter the browser.')),
    h(
      'div',
      { className: 'segmented', role: 'group', 'aria-label': 'Planning mode' },
      h('button', { type: 'button', className: props.mode === 'opencode' ? 'segmented__active' : '', onClick: () => props.onMode('opencode'), disabled: props.generating }, 'OpenCode'),
      h('button', { type: 'button', className: props.mode === 'fallback' ? 'segmented__active' : '', onClick: () => props.onMode('fallback'), disabled: props.generating }, 'Fallback'),
    ),
    h('button', { type: 'button', className: 'button button--primary', onClick: props.onGenerate, disabled: props.generating, 'aria-busy': props.generating ? 'true' : 'false' }, props.generating ? 'Generating plan…' : 'Generate plan'),
    props.error ? h('p', { className: 'inline-alert inline-alert--danger', role: 'alert' }, props.error) : null,
  )
}

function evidenceCard(label: string, content: unknown, detail?: unknown): unknown {
  return h('article', { className: 'detail-card' }, h('span', { className: 'label' }, label), content, detail ?? null)
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
    h(
      'section',
      { className: 'hero-status' },
      h('div', { className: 'hero-status__identity' }, h('span', { className: 'eyebrow' }, 'Current authority'), h('div', { className: 'badge-row' }, StatusBadge({ value: item.lifecycle }), StatusBadge({ value: item.attention.kind }))),
      h('div', { className: 'hero-status__route' }, h('span', { className: 'label' }, 'Next route'), h('strong', null, item.route.action), item.route.target_role ? h('span', null, `Target: ${item.route.target_role}`) : null, item.route.lane_key ? h('code', { className: 'breakable' }, item.route.lane_key) : null),
    ),
    h(
      'div',
      { className: 'evidence-grid' },
      evidenceCard('GitHub Issue', ExternalLink({ href: item.identity.url, children: `#${item.identity.issue_number}` }), h('p', { className: 'breakable' }, item.identity.title)),
      evidenceCard('Candidate PR', candidate ? ExternalLink({ href: candidate.url, children: `#${candidate.number} ${candidate.title}` }) : h('strong', null, 'No current Candidate'), candidate ? h('p', { className: 'muted' }, `${candidate.draft ? 'Draft' : 'Ready'} · ${candidate.mergeable_state || 'mergeability unknown'}`) : null),
      evidenceCard('Current exact Head', h('code', { className: 'sha-full' }, item.current_head ?? '—'), item.route.expected_head && item.route.expected_head !== item.current_head ? h('p', { className: 'inline-alert inline-alert--warning' }, `Route expected ${item.route.expected_head}`) : null),
      evidenceCard('Exact-Head CI', StatusBadge({ value: ciLabel(item) }), h(React.Fragment, null, h('p', { className: 'muted' }, item.ci.head_sha ? `Head ${shortSha(item.ci.head_sha)} · ${item.ci.source}` : item.ci.source || 'No CI evidence'), item.ci.details_url ? ExternalLink({ href: item.ci.details_url, children: 'Open CI evidence' }) : null)),
    ),
    item.active_blocker ? h('section', { className: 'callout callout--danger' }, h('strong', null, 'Active blocker'), h('p', null, `${item.active_blocker.role} reported ${item.active_blocker.status} at ${formatDate(item.active_blocker.created_at)}.`), item.active_blocker.head ? h('code', { className: 'breakable' }, item.active_blocker.head) : null) : null,
    SectionHeading({ title: 'Latest worker results', copy: 'Current and stale terminal evidence by role.' }),
    h('div', { className: 'results-grid' }, ResultCard({ role: 'Lead', result: item.latest_results.lead }), ResultCard({ role: 'Implementor handoff', result: item.latest_results.implementor }), ResultCard({ role: 'QA verdict', result: item.latest_results.qa })),
    h(
      'div',
      { className: 'two-column' },
      h('section', { className: 'panel-section' }, SectionHeading({ title: 'Route guards', compact: true }), item.route.guards.length ? h('ul', { className: 'plain-list' }, ...item.route.guards.map((guard, index) => h('li', { key: index }, guard))) : h('p', { className: 'muted' }, 'No route guards reported.'), h('p', { className: 'muted' }, item.route.reason)),
      h('section', { className: 'panel-section' }, SectionHeading({ title: 'Warnings', compact: true }), WarningList({ warnings: [...item.warnings, ...item.route.warnings] })),
    ),
    props.launcher,
    SectionHeading({ title: 'Operational evidence', count: evidence.length, copy: 'Meaningful source comments retained by the backend.' }),
    evidence.length === 0
      ? EmptyState({ title: 'No meaningful comment evidence', message: 'The backend has no parsed operational evidence for this work unit yet.' })
      : h(
          'div',
          { className: 'evidence-timeline' },
          ...evidence.map((entry) => h(
            'article',
            { className: 'evidence-entry', key: entry.comment_id },
            h('div', { className: 'card-topline' }, h('strong', null, entry.heading || entry.level), h('span', { className: 'muted' }, `${entry.author} · ${formatDate(entry.created_at)}`)),
            h('p', { className: 'evidence-markdown' }, entry.markdown),
            ExternalLink({ href: entry.url, children: 'Open source comment' }),
          )),
        ),
  )
}
