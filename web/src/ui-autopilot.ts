import { AutopilotBreaker, AutopilotStatus } from './autopilot-domain.js'
import { formatDate, PageHeader, SectionHeading, shortSha, StatusBadge } from './ui-shared.js'

const h = React.createElement

function metric(label: string, value: number): unknown {
  return h('article', { className: 'autopilot-metric' }, h('span', { className: 'label' }, label), h('strong', null, String(value)))
}

function ControlButton(props: { label: string; disabled: boolean; onClick: () => void; primary?: boolean }): unknown {
  return h('button', { type: 'button', className: `button ${props.primary ? 'button--primary' : 'button--secondary'}`, disabled: props.disabled, onClick: props.onClick }, props.label)
}

function BreakerCard(props: { breaker: AutopilotBreaker; busy: boolean; onAcknowledge: () => void; onResolve: () => void }): unknown {
  const breaker = props.breaker
  return h('article', { className: `autopilot-breaker autopilot-breaker--${breaker.status}` },
    h('div', { className: 'autopilot-breaker__head' },
      h('div', null, StatusBadge({ value: breaker.status }), h('strong', null, breaker.code)),
      h('code', null, breaker.scope_kind === 'lane' ? breaker.lane_key : `project:${breaker.project_id}`),
    ),
    h('p', null, breaker.reason),
    h('p', { className: 'muted' }, breaker.recovery_requirements),
    breaker.expected_head ? h('p', null, h('span', { className: 'label' }, 'Expected Head '), h('code', { title: breaker.expected_head }, shortSha(breaker.expected_head))) : null,
    breaker.evidence ? h('details', null, h('summary', null, 'Evidence'), h('pre', null, breaker.evidence)) : null,
    h('div', { className: 'autopilot-breaker__actions' },
      breaker.status === 'open' ? h('button', { type: 'button', className: 'button button--secondary', disabled: props.busy, onClick: props.onAcknowledge }, 'Acknowledge') : null,
      breaker.status !== 'resolved' ? h('button', { type: 'button', className: 'button button--primary', disabled: props.busy, onClick: props.onResolve }, 'Resolve') : null,
      h('span', { className: 'muted' }, `${breaker.occurrence_count} occurrence(s) · ${formatDate(breaker.updated_at)}`),
    ),
  )
}

function QueueTable(status: AutopilotStatus): unknown {
  if (status.queue.length === 0) return h('p', { className: 'empty-inline' }, 'No active or waiting Intents.')
  const rows = status.queue.map((item) => h('tr', { key: item.intent.intent_id },
    h('td', null, String(item.intent.priority)),
    h('td', null, item.intent.action_type),
    h('td', null, item.intent.issue_number ? `#${item.intent.issue_number} ${item.intent.role ?? ''}` : item.intent.role ?? 'Project'),
    h('td', null, h('code', null, item.intent.lane_key ?? '—')),
    h('td', null, StatusBadge({ value: item.intent.status }), h('small', null, item.waiting_reason ?? '')),
    h('td', null, h('code', { title: item.intent.expected_head ?? '' }, shortSha(item.intent.expected_head))),
  ))
  return h('div', { className: 'autopilot-table-wrap' }, h('table', { className: 'autopilot-table' },
    h('thead', null, h('tr', null, h('th', null, 'Priority'), h('th', null, 'Action'), h('th', null, 'Target'), h('th', null, 'Lane'), h('th', null, 'Status'), h('th', null, 'Exact Head'))),
    h('tbody', null, ...rows),
  ))
}

function ExecutionEvidence(status: AutopilotStatus): unknown {
  const leaseRows = status.active_leases.map((lease) => h('p', { key: lease.lease_id },
    StatusBadge({ value: lease.status }), ' ', h('code', null, lease.lane_key),
    h('small', null, ` lease ${lease.lease_id} · intent ${lease.intent_id} · claim ${lease.claim_id} · ${lease.lease_owner} · expires ${formatDate(lease.expires_at)}`),
  ))
  const provisionRows = status.provisioning.slice(0, 12).map((request) => h('p', { key: request.id },
    StatusBadge({ value: request.status }), ' ', h('code', null, request.lane_key),
    h('small', null, ` request ${request.id} · intent ${request.intent_id} · Issue #${request.issue_number} · ${request.role}`),
    request.expected_head ? h('code', { title: request.expected_head }, ` ${shortSha(request.expected_head)}`) : null,
    request.worker_id ? h('small', null, ` · worker ${request.worker_id} · tab ${request.tab_id ?? '—'}`) : null,
    request.bound_binding_id ? h('small', null, ` · binding ${request.bound_binding_id}@${request.bound_binding_version}`) : null,
  ))
  const commandRows = status.commands.slice(0, 12).map((command) => h('p', { key: command.materialization_id },
    StatusBadge({ value: command.delivery_status ?? command.workflow_status ?? command.status }), ' ', h('code', null, command.lane_key),
    h('small', null, ` materialization ${command.materialization_id} · intent ${command.intent_id} · lease ${command.lease_id}`),
    command.workflow_command_id ? h('small', null, ` · workflow ${command.workflow_command_id}`) : null,
    command.delivery_command_id ? h('small', null, ` · delivery ${command.delivery_command_id}`) : null,
    command.context_hash ? h('code', { title: command.context_hash }, ` context ${shortSha(command.context_hash)}`) : null,
    command.prompt_hash ? h('code', { title: command.prompt_hash }, ` prompt ${shortSha(command.prompt_hash)}`) : null,
  ))
  const mergeRows = status.merge_cycles.slice(0, 12).map((cycle) => h('p', { key: cycle.id },
    StatusBadge({ value: cycle.status }), ' ', h('small', null, `cycle ${cycle.id} · intent ${cycle.intent_id} · Issue #${cycle.issue_number} · PR #${cycle.pr_number} · approved `),
    h('code', { title: cycle.approved_head }, shortSha(cycle.approved_head)),
    cycle.observed_merge_commit ? h('small', null, ' · observed ') : null,
    cycle.observed_merge_commit ? h('code', { title: cycle.observed_merge_commit }, shortSha(cycle.observed_merge_commit)) : null,
  ))
  return h('div', { className: 'autopilot-evidence-grid' },
    h('article', null, h('h3', null, `Active leases · ${status.active_leases.length}`), ...leaseRows),
    h('article', null, h('h3', null, `Provisioning · ${status.provisioning.length}`), ...provisionRows),
    h('article', null, h('h3', null, `Commands · ${status.commands.length}`), ...commandRows),
    h('article', null, h('h3', null, `Merge read-back · ${status.merge_cycles.length}`), ...mergeRows),
  )
}

function Warnings(status: AutopilotStatus): unknown {
  if (status.warnings.length === 0) return null
  return h('section', { className: 'autopilot-warnings' },
    SectionHeading({ title: 'Exact-identity warnings', count: status.warnings.length }),
    ...status.warnings.map((warning, index) => h('article', { key: `${warning.code}:${warning.intent_id ?? index}` },
      StatusBadge({ value: 'warning', label: warning.code }),
      h('p', null, warning.message),
      warning.expected_head ? h('code', null, `${shortSha(warning.expected_head)} → ${shortSha(warning.observed_head)}`) : null,
    )),
  )
}

export function AutopilotContent(props: {
  status: AutopilotStatus
  busy: boolean
  feedback?: string
  onRefresh: () => void
  onControl: (action: 'enable' | 'pause' | 'resume' | 'stop') => void
  onAcknowledge: (breakerID: string) => void
  onResolve: (breakerID: string) => void
  breakerForm: unknown
}): unknown {
  const value = props.status
  const activeBreakers = value.circuit_breakers.filter((breaker) => breaker.status !== 'resolved')
  const breakerContent = activeBreakers.length === 0
    ? h('p', { className: 'empty-inline' }, 'No active circuit breakers.')
    : h('div', { className: 'stack-md' }, ...activeBreakers.map((breaker) => h(BreakerCard, { key: breaker.id, breaker, busy: props.busy, onAcknowledge: () => props.onAcknowledge(breaker.id), onResolve: () => props.onResolve(breaker.id) })))

  return h(React.Fragment, null,
    PageHeader({ eyebrow: 'Continuous delivery operations', title: `Autopilot · ${value.repository}`, summary: value.next_action, actions: [h('button', { type: 'button', className: 'button button--secondary', onClick: props.onRefresh, disabled: props.busy, key: 'refresh' }, 'Refresh')] }),
    h('section', { className: 'autopilot-status-strip' },
      h('div', null, h('span', { className: 'label' }, 'Project'), h('strong', null, String(value.project_id))),
      h('div', null, h('span', { className: 'label' }, 'Wave'), h('code', null, value.active_wave?.wave_id ?? '—')),
      h('div', null, h('span', { className: 'label' }, 'Autonomy'), StatusBadge({ value: value.profile.autonomy_state })),
      h('div', null, h('span', { className: 'label' }, 'GitHub sync'), StatusBadge({ value: value.sync_status })),
      h('div', null, h('span', { className: 'label' }, 'Operator revision'), h('strong', null, String(value.control.revision))),
      h('div', null, h('span', { className: 'label' }, 'Lead lane'), StatusBadge({ value: value.lead_busy ? 'in_progress' : 'ready' })),
    ),
    value.sync_error ? h('p', { className: 'inline-alert inline-alert--danger', role: 'alert' }, value.sync_error) : null,
    props.feedback ? h('p', { className: 'inline-alert', role: 'status' }, props.feedback) : null,
    h('section', { className: 'autopilot-controls', 'aria-label': 'Autopilot controls' },
      ControlButton({ label: 'Enable', primary: true, disabled: props.busy || value.profile.autonomy_state === 'enabled', onClick: () => props.onControl('enable') }),
      ControlButton({ label: 'Pause', disabled: props.busy || value.profile.autonomy_state !== 'enabled', onClick: () => props.onControl('pause') }),
      ControlButton({ label: 'Resume', disabled: props.busy || value.profile.autonomy_state !== 'paused', onClick: () => props.onControl('resume') }),
      ControlButton({ label: 'Stop', disabled: props.busy || value.profile.autonomy_state === 'stopped', onClick: () => props.onControl('stop') }),
    ),
    h('section', { className: 'autopilot-metrics' },
      metric('Pending Intents', value.counts.pending_intents), metric('Claimed', value.counts.claimed_intents), metric('Active leases', value.counts.active_leases),
      metric('Provisioning', value.counts.pending_provisioning), metric('Managed sessions', value.counts.managed_sessions), metric('Active commands', value.counts.active_commands),
      metric('Circuit breakers', value.counts.active_circuit_breakers), metric('Ambiguous records', value.counts.ambiguous_records),
    ),
    SectionHeading({ title: 'Circuit breakers', count: activeBreakers.length, copy: 'Acknowledged breakers continue blocking their exact project or lane until resolved.' }),
    breakerContent,
    props.breakerForm,
    SectionHeading({ title: 'Intent queue', count: value.queue.length, copy: 'Priority-ordered durable work with exact waiting reasons.' }),
    QueueTable(value),
    SectionHeading({ title: 'Execution evidence', copy: 'Correlated lease, provisioning, binding, command and merge identities remain explicit.' }),
    ExecutionEvidence(value),
    Warnings(value),
  )
}
