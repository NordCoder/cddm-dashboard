import { BrowserWorker } from './browser-api.js'
import { WorkUnitState } from './domain.js'
import { PilotReadiness, WorkUnitExecution } from './workerloop-api.js'
import { SectionHeading, StatusBadge } from './ui-shared.js'

const h = React.createElement

function evidenceCard(label: string, content: unknown, detail?: unknown): unknown {
  return h('article', { className: 'detail-card' }, h('span', { className: 'label' }, label), content, detail ?? null)
}

export function WorkerLoopPanel(props: {
  workUnit: WorkUnitState
  execution: WorkUnitExecution
  readiness: PilotReadiness
  workers: BrowserWorker[]
  busy: boolean
  feedback?: string
  onBindRole: (role: string, worker: BrowserWorker) => void
  onDisableRole: (role: string) => void
  onDeliveryMode: (mode: 'reviewed' | 'auto') => void
}): unknown {
  const command = props.execution.active_workflow_command
  const result = props.execution.worker_result
  const liveWorkers = props.workers.filter((worker) => worker.state === 'live' && worker.target)
  const qa = props.execution.role_bindings.find((item) => item.role === 'qa')
  const freshQARequired = props.workUnit.route.target_role === 'qa' && (!qa?.binding || qa.binding.readiness !== 'ready')

  const roleCards = props.execution.role_bindings.map((item) => {
    const selectID = `role-binding-${item.role}`
    return h(
      'article',
      { className: 'detail-card', key: item.role },
      h('div', { className: 'card-topline' }, h('strong', null, item.role), StatusBadge({ value: item.binding?.readiness ?? 'unbound' })),
      h('code', { className: 'breakable' }, item.lane_key),
      item.binding ? h('p', { className: 'muted' }, `${item.binding.worker_id} · binding v${item.binding.binding_version}`) : h('p', { className: 'muted' }, 'No ChatGPT conversation is bound.'),
      h('label', { className: 'label', htmlFor: selectID }, 'Live ChatGPT target'),
      h('select', { id: selectID, className: 'delivery-select', disabled: props.busy || liveWorkers.length === 0 },
        liveWorkers.length === 0 ? h('option', { value: '' }, 'No live targets') : null,
        ...liveWorkers.map((worker, index) => h('option', { value: String(index), key: worker.worker_id }, `${worker.worker_id} · ${worker.target?.path ?? ''}`)),
      ),
      h('div', { className: 'delivery-actions' },
        h('button', {
          type: 'button', className: 'button button--secondary', disabled: props.busy || liveWorkers.length === 0,
          onClick: () => {
            const element = document.getElementById(selectID) as HTMLSelectElement | null
            const worker = liveWorkers[Number(element?.value ?? 0)]
            if (worker) props.onBindRole(item.role, worker)
          },
        }, item.binding ? 'Rebind' : 'Bind'),
        item.binding?.enabled ? h('button', { type: 'button', className: 'button button--secondary', disabled: props.busy, onClick: () => props.onDisableRole(item.role) }, 'Disable') : null,
      ),
    )
  })

  return h(
    React.Fragment,
    null,
    SectionHeading({ title: 'Worker-loop execution', copy: 'Browser delivery and worker completion are intentionally separate states.' }),
    freshQARequired ? h('section', { className: 'callout callout--warning' }, h('strong', null, 'Fresh QA chat required'), h('p', null, 'Bind a new QA conversation. The previous QA binding is retired after an accepted terminal QA result.')) : null,
    props.feedback ? h('p', { className: props.feedback.includes('error') ? 'inline-alert inline-alert--danger' : 'inline-alert' }, props.feedback) : null,
    h('div', { className: 'evidence-grid' },
      evidenceCard('Active workflow command', command ? h('code', { className: 'breakable' }, command.command_id) : h('strong', null, 'No active command'), command ? h('p', { className: 'muted' }, `${command.role} · ${command.resource_version}`) : null),
      evidenceCard('Delivery status', StatusBadge({ value: props.execution.delivery_status }), props.execution.delivery ? h('p', { className: 'muted' }, `${props.execution.delivery.worker_id} · binding v${props.execution.delivery.binding_version}`) : null),
      evidenceCard('Execution status', StatusBadge({ value: props.execution.execution_status }), h('p', { className: 'muted' }, props.execution.next_action)),
      evidenceCard('Worker result', result ? StatusBadge({ value: result.result }) : h('strong', null, 'Not observed'), result ? h('p', { className: 'muted' }, `Validation: ${result.validation_status}`) : h('p', { className: 'muted' }, `Validation: ${props.execution.validation_status}`)),
      evidenceCard('QA-reviewed Head', h('code', { className: 'breakable' }, props.workUnit.qa_reviewed_head ?? '—')),
      evidenceCard('Candidate identity', h('code', { className: 'breakable' }, props.workUnit.current_head ?? '—'), h('p', { className: 'muted' }, props.workUnit.candidate.current ? `PR #${props.workUnit.candidate.current.number}` : 'No current Candidate')),
    ),
    h('section', { className: 'panel-section' },
      SectionHeading({ title: 'Execution profile', compact: true }),
      h('dl', { className: 'details-list' },
        h('div', null, h('dt', null, 'Resources'), h('dd', null, props.execution.profile.resource_version)),
        h('div', null, h('dt', null, 'Methodology'), h('dd', null, props.execution.profile.methodology_version)),
        h('div', null, h('dt', null, 'Result protocol'), h('dd', null, props.execution.profile.result_protocol)),
        h('div', null, h('dt', null, 'QA mode'), h('dd', null, props.execution.profile.qa_session_mode)),
        h('div', null, h('dt', null, 'Auto merge'), h('dd', null, props.execution.profile.auto_merge ? 'enabled' : 'disabled')),
      ),
      h('div', { className: 'segmented', role: 'group', 'aria-label': 'Delivery mode' },
        h('button', { type: 'button', className: props.execution.profile.delivery_mode === 'reviewed' ? 'segmented__active' : '', disabled: props.busy, onClick: () => props.onDeliveryMode('reviewed') }, 'Reviewed'),
        h('button', { type: 'button', className: props.execution.profile.delivery_mode === 'auto' ? 'segmented__active' : '', disabled: props.busy, onClick: () => props.onDeliveryMode('auto') }, 'Auto-send'),
      ),
    ),
    SectionHeading({ title: 'Role-to-chat bindings', copy: 'Lead, Implementor and QA use distinct logical lanes.' }),
    h('div', { className: 'evidence-grid' }, ...roleCards),
    SectionHeading({ title: 'Pilot Readiness', copy: 'Diagnostic only. This check does not create or deliver commands.' }),
    h('section', { className: props.readiness.ready ? 'callout' : 'callout callout--warning' },
      h('div', { className: 'card-topline' }, h('strong', null, props.readiness.ready ? 'PILOT READY' : 'Pilot prerequisites incomplete'), StatusBadge({ value: props.readiness.status })),
      h('ul', { className: 'plain-list' }, ...props.readiness.checks.map((check) => h('li', { key: check.code }, StatusBadge({ value: check.ready ? 'ready' : 'blocked' }), ' ', h('strong', null, check.code), ` — ${check.status}`, check.detail ? h('span', { className: 'muted' }, ` · ${check.detail}`) : null))),
    ),
  )
}
