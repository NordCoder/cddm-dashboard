import { HealthResponse, PlannerHealth } from './domain.js'
import { PageHeader, SectionHeading, StatusBadge } from './ui-shared.js'

const h = React.createElement

export function SettingsContent(props: { backend: HealthResponse; planner: PlannerHealth; onRefresh: () => void }): unknown {
  return h(
    React.Fragment,
    null,
    PageHeader({
      eyebrow: 'Runtime summary',
      title: 'System health',
      summary: 'Operational health and trust boundaries. Credentials remain process configuration and never enter the browser workspace.',
      actions: [h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRefresh, key: 'refresh' }, 'Refresh')],
    }),
    h(
      'div',
      { className: 'overview-grid' },
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Backend'), StatusBadge({ value: props.backend.status }), h('p', null, `Database: ${props.backend.database}`)),
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Planner runtime'), StatusBadge({ value: props.planner.status }), h('p', null, `${props.planner.runtime}${props.planner.enabled ? '' : ' · disabled'}`), props.planner.error ? h('p', { className: 'inline-alert inline-alert--danger' }, props.planner.error) : null),
      h('article', { className: 'overview-card' }, h('span', { className: 'label' }, 'Configured planner identity'), h('strong', null, props.planner.agent || 'No agent'), h('p', { className: 'muted' }, [props.planner.provider, props.planner.model].filter(Boolean).join(' / ') || 'Provider/model not exposed or not configured')),
    ),
    h(
      'section',
      { className: 'panel-section trust-boundary' },
      SectionHeading({ title: 'Browser delivery trust boundary', copy: 'Explicit delivery without response scraping.' }),
      h(
        'ol',
        { className: 'boundary-list' },
        h('li', null, h('strong', null, 'Review'), h('span', null, 'Inspect the immutable backend Prompt Plan, exact Head, lane, binding and target.')),
        h('li', null, h('strong', null, 'Confirm'), h('span', null, 'Create one idempotent delivery intent for the exact reviewed authority snapshot.')),
        h('li', null, h('strong', null, 'Execute'), h('span', null, 'The bundled extension sends only to the explicitly bound ChatGPT conversation.')),
        h('li', null, h('strong', null, 'Observe'), h('span', null, 'The dashboard records delivery lifecycle only; ChatGPT response content is never read or persisted.')),
      ),
      h('p', { className: 'muted' }, 'Manual Copy remains available independently of browser delivery. GitHub, planner and extension credentials stay outside frontend state.'),
    ),
  )
}
