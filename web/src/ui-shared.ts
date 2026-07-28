import { GenerationResult, WorkUnitState } from './domain.js'
import { paths } from './router.js'

const h = React.createElement

export type Navigate = (path: string) => void

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
  if (!result?.plan) return false
  return (result.status === 'approved' || result.status === 'fallback') && !result.plan.requires_owner
}

export function badgeTone(value: string): string {
  if (['healthy', 'success', 'completed', 'approved', 'fallback', 'normal', 'terminal', 'clean', 'ready', 'delivered'].includes(value)) return 'positive'
  if (['blocked', 'failed', 'failure', 'rejected', 'planner_error', 'owner_required', 'ci_failed'].includes(value)) return 'danger'
  if (['stale', 'qa_invalidated', 'ambiguous', 'protocol_warning', 'action_required', 'waiting', 'in_progress', 'warning', 'uncertain'].includes(value)) return 'warning'
  return 'neutral'
}

function badgeSymbol(tone: string): string {
  if (tone === 'positive') return '●'
  if (tone === 'danger') return '■'
  if (tone === 'warning') return '▲'
  return '◆'
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

export function InternalLink(props: { href: string; navigate: Navigate; className?: string; children: unknown; current?: boolean }): unknown {
  return h(
    'a',
    {
      href: props.href,
      className: props.className,
      'aria-current': props.current ? 'page' : undefined,
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
    h(
      'div',
      { className: 'page-header__copy' },
      h('p', { className: 'eyebrow' }, props.eyebrow),
      h('h1', null, props.title),
      props.summary ? h('p', { className: 'page-summary' }, props.summary) : null,
    ),
    props.actions?.length ? h('div', { className: 'page-actions' }, ...props.actions) : null,
  )
}

export function SectionHeading(props: { title: string; count?: number; copy?: string; compact?: boolean }): unknown {
  return h(
    'div',
    { className: `section-heading${props.compact ? ' section-heading--compact' : ''}` },
    h('div', null, h(props.compact ? 'h3' : 'h2', null, props.title), props.copy ? h('p', { className: 'muted' }, props.copy) : null),
    props.count === undefined ? null : h('span', { className: 'count-pill' }, String(props.count)),
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
    { className: 'state-panel state-panel--empty' },
    h('strong', null, props.title),
    h('p', { className: 'muted' }, props.message),
    props.action ?? null,
  )
}

const navigation = [
  { href: paths.workspace(), label: 'Workspace', hint: 'Repositories and attention', section: 'workspace' },
  { href: paths.settings(), label: 'System health', hint: 'Runtime and trust boundary', section: 'settings' },
] as const

export function AppShell(props: { routeLabel: string; navigate: Navigate; children: unknown }): unknown {
  const pathname = globalThis.location?.pathname ?? '/'
  const activeSection = pathname.startsWith(paths.settings()) ? 'settings' : 'workspace'
  return h(
    'div',
    { className: 'app-shell' },
    h(
      'aside',
      { className: 'sidebar' },
      h(
        'div',
        { className: 'brand' },
        h('span', { className: 'brand__mark', 'aria-hidden': 'true' }, 'CD'),
        h('div', null, h('strong', null, 'CDDM'), h('span', null, 'Supervisor workspace')),
      ),
      h(
        'nav',
        { className: 'primary-nav', 'aria-label': 'Primary navigation' },
        ...navigation.map((item) => InternalLink({
          href: item.href,
          navigate: props.navigate,
          className: 'nav-link',
          current: item.section === activeSection,
          children: h('span', null, h('strong', null, item.label), h('small', null, item.hint)),
        })),
      ),
      h('div', { className: 'sidebar__context' }, h('span', { className: 'eyebrow' }, 'Current view'), h('strong', null, props.routeLabel)),
      h('p', { className: 'sidebar__boundary' }, 'Confirmed browser delivery · response content remains outside the system boundary'),
    ),
    h(
      'div',
      { className: 'workspace-frame' },
      h(
        'header',
        { className: 'workspace-topbar' },
        h('div', null, h('span', { className: 'workspace-topbar__label' }, 'Operational workspace'), h('strong', null, props.routeLabel)),
        h('div', { className: 'workspace-topbar__authority' }, h('span', { 'aria-hidden': 'true' }, '●'), ' Backend authoritative'),
      ),
      h('main', { className: 'main-content', id: 'main-content' }, props.children),
    ),
  )
}
