import { ContextSummary, GenerationResult, WorkUnitState } from './domain.js'
import { paths } from './router.js'
import {
  badgeTone,
  EmptyState,
  formatDate,
  InternalLink,
  isDispatchReady,
  isPromptEdited,
  Navigate,
  PageHeader,
  PromptEditState,
  SectionHeading,
  shortHash,
  shortSha,
  StatusBadge,
} from './ui-shared.js'

const h = React.createElement

function metadataItem(label: string, value: unknown): unknown {
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
    h(
      'div',
      { className: 'plan-review__header' },
      h('div', null, h('p', { className: 'eyebrow' }, 'Prompt Plan'), h('div', { className: 'badge-row' }, StatusBadge({ value: result.status }), StatusBadge({ value: source }), edited ? StatusBadge({ value: 'warning', label: 'Edited locally' }) : null)),
      h('div', { className: 'plan-review__copy-meta' }, h('span', { className: 'label' }, 'Exact Head before copy'), h('code', { className: 'breakable' }, expectedHead || '—')),
    ),
    !ready ? h('div', { className: 'callout callout--warning' }, h('strong', null, 'Not dispatch-ready'), h('p', null, `Status ${result.status} is shown for review only. Copy is unavailable unless the backend result is approved or fallback and the current work unit does not require Owner action.`)) : null,
    h(
      'dl',
      { className: 'metadata-grid' },
      metadataItem('Status', result.status),
      metadataItem('Source', source),
      metadataItem('Action', plan?.action ?? result.context.route.action),
      metadataItem('Target role', plan?.target_role ?? result.context.route.target_role ?? '—'),
      metadataItem('Lane key', h('code', { className: 'breakable' }, plan?.lane_key ?? result.context.route.lane_key ?? '—')),
      metadataItem('Expected Head', h('code', { className: 'breakable' }, expectedHead || '—')),
      metadataItem('Context hash', h('code', { className: 'breakable' }, result.context.context_hash)),
      metadataItem('Risk', plan?.risk ?? '—'),
      metadataItem('Confidence', plan ? `${Math.round(plan.confidence * 100)}%` : '—'),
      metadataItem('Created', formatDate(result.created_at)),
    ),
    plan ? h('section', { className: 'plan-explanation' }, h('span', { className: 'eyebrow' }, 'Rationale'), h('h3', null, plan.summary || 'Plan rationale'), h('p', null, plan.reason)) : null,
    h(
      'div',
      { className: 'two-column' },
      h('section', { className: 'panel-section' }, SectionHeading({ title: 'Guards', compact: true }), plan?.guards.length ? h('ul', { className: 'plain-list' }, ...plan.guards.map((guard, index) => h('li', { key: index }, guard))) : h('p', { className: 'muted' }, 'No plan guards.')),
      h('section', { className: 'panel-section' }, SectionHeading({ title: 'Prohibited actions', compact: true }), plan?.prohibited_actions.length ? h('ul', { className: 'plain-list' }, ...plan.prohibited_actions.map((action, index) => h('li', { key: index }, action))) : h('p', { className: 'muted' }, 'No prohibited actions listed.')),
    ),
    result.policy_decision.violations.length > 0
      ? h('section', { className: 'callout callout--danger' }, h('strong', null, 'Policy violations'), h('ul', { className: 'plain-list' }, ...result.policy_decision.violations.map((violation, index) => h('li', { key: index }, `${violation.code}${violation.field ? ` (${violation.field})` : ''}: ${violation.message}`))))
      : null,
    h(
      'section',
      { className: 'prompt-editor' },
      h(
        'div',
        { className: 'prompt-editor__toolbar' },
        h('div', null, h('span', { className: 'eyebrow' }, 'Immutable plan output'), h('h2', null, 'Prompt review'), h('p', { className: 'muted' }, edited ? 'Edited locally — backend PromptPlan and PolicyDecision are unchanged.' : 'Generated prompt — no local edits.')),
        edited ? h('button', { type: 'button', className: 'button button--secondary', onClick: props.onReset }, 'Reset to generated prompt') : null,
      ),
      plan
        ? h('textarea', { value: props.edit.value, onChange: (event: { currentTarget: HTMLTextAreaElement }) => props.onEdit(event.currentTarget.value), rows: 18, spellCheck: false, 'aria-label': 'Prompt text' })
        : h('div', { className: 'state-panel' }, 'No prompt body is available for this generation.'),
      h(
        'div',
        { className: 'prompt-actions' },
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
        h(
          'dl',
          { className: 'history-meta' },
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
      summary: 'Review immutable backend generations, optionally edit only the local prompt copy, then copy manually or use confirmed browser delivery.',
      actions: [
        InternalLink({ href: paths.workUnit(item.identity.project_id, item.identity.issue_number), navigate: props.navigate, className: 'button button--secondary', children: 'Back to work unit' }),
        h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRefresh, key: 'refresh' }, 'Refresh'),
      ],
    }),
    props.context ? h('section', { className: 'health-strip' }, h('span', { className: 'health-strip__label' }, 'Current context'), h('code', null, shortHash(props.context.context_hash)), h('span', null, `Head ${shortSha(props.context.current_head)}`), StatusBadge({ value: props.context.issue.attention.kind })) : null,
    props.launcher,
    h(
      'div',
      { className: 'planning-layout' },
      h('div', { className: 'planning-layout__main' }, props.current && props.review ? props.review : EmptyState({ title: 'No Prompt Plan selected', message: 'Generate a plan or choose a historical generation.' })),
      h('aside', { className: 'planning-layout__history', 'aria-label': 'Prompt plan history' }, SectionHeading({ title: 'History', count: props.history.length, compact: true }), PlanHistory({ items: props.history, projectID: item.identity.project_id, issueNumber: item.identity.issue_number, navigate: props.navigate, selectedPlanID: props.selectedPlanID })),
    ),
  )
}
