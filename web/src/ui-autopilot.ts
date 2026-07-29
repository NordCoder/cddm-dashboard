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
    h('td', null, h('code', null, item.intent.intent_id), h('small', null, ` · ${item.intent.action_type}`)),
    h('td', null, h('code', null, item.intent.wave_id ?? '—')),
    h('td', null, item.intent.issue_number ? `#${item.intent.issue_number}` : 'Project', item.intent.pr_number ? h('small', null, ` · PR #${item.intent.pr_number}`) : null, h('small', null, ` · ${item.intent.role ?? 'project'}`)),
    h('td', null, h('code', null, item.intent.lane_key ?? '—')),
    h('td', null, StatusBadge({ value: item.intent.status }), h('small', null, item.waiting_reason ?? '')),
    h('td', null, item.intent.expected_head ? h('code', { title: item.intent.expected_head }, shortSha(item.intent.expected_head)) : '—'),
  ))
  return h('div', { className: 'autopilot-table-wrap' }, h('table', { className: 'autopilot-table' },
    h('thead', null, h('tr', null, h('th', null, 'Priority'), h('th', null, 'Intent / action'), h('th', null, 'Wave'), h('th', null, 'Issue / PR / role'), h('th', null, 'Lane'), h('th', null, 'Status'), h('th', null, 'Exact Head'))),
    h('tbody', null, ...rows),
  ))
}

function ExecutionEvidence(status: AutopilotStatus): unknown {
  if (status.commands.length === 0 && status.merge_cycles.length === 0) return h('p', { className: 'empty-inline' }, 'No correlated command or merge evidence.')
  const commandRows = status.commands.map((command) => {
    const intent = status.intents.find((value) => value.intent_id === command.intent_id)
    const provision = status.provisioning.find((value) => value.id === command.provision_request_id)
    const results = status.results.filter((value) => value.command_id === command.workflow_command_id)
    const resultEvidence = results.length === 0
      ? '—'
      : h('div', null, ...results.map((result) => h('p', { key: result.github_comment_id }, h('code', null, `comment:${result.github_comment_id}`), h('small', null, ` · ${result.result} / ${result.validation_status}`), result.validation_reason ? h('small', null, ` · ${result.validation_reason}`) : null)))
    return h('tr', { key: command.materialization_id },
      h('td', null, h('code', null, `project:${command.project_id}`), h('small', null, ` · wave ${intent?.wave_id ?? '—'}`)),
      h('td', null, `#${command.issue_number}`, intent?.pr_number ? h('small', null, ` · PR #${intent.pr_number}`) : null, command.expected_head ? h('code', { title: command.expected_head }, ` · ${shortSha(command.expected_head)}`) : null),
      h('td', null, h('code', null, command.intent_id), h('small', null, ` · ${command.role}`), h('code', null, ` · ${command.lane_key}`), h('small', null, ` · lease ${command.lease_id}`)),
      h('td', null, h('code', null, command.provision_request_id), h('small', null, ` · worker ${command.worker_id ?? '—'} / session ${command.worker_session_id ?? '—'} / tab ${command.tab_id ?? '—'}`), h('small', null, ` · binding ${command.binding_id ?? '—'}@${command.binding_version ?? '—'}`), provision?.observed_chatgpt_url ? h('code', { title: provision.observed_chatgpt_url }, ' · observed target') : null),
      h('td', null, h('code', null, command.materialization_id), h('small', null, ` · workflow ${command.workflow_command_id ?? '—'} (${command.workflow_status ?? '—'})`), h('small', null, ` · delivery ${command.delivery_command_id ?? '—'} (${command.delivery_status ?? '—'})`)),
      h('td', null, resultEvidence),
    )
  })
  const mergeRows = status.merge_cycles.map((cycle) => {
    const intent = status.intents.find((value) => value.intent_id === cycle.intent_id)
    return h('tr', { key: cycle.id },
      h('td', null, h('code', null, `project:${cycle.project_id}`), h('small', null, ` · wave ${intent?.wave_id ?? '—'}`)),
      h('td', null, `#${cycle.issue_number}`, h('small', null, ` · PR #${cycle.pr_number}`), h('code', { title: cycle.approved_head }, ` · ${shortSha(cycle.approved_head)}`)),
      h('td', null, h('code', null, cycle.intent_id), h('small', null, ` · ${intent?.role ?? 'lead'}`), h('code', null, ` · ${intent?.lane_key ?? '—'}`)),
      h('td', null, '—'),
      h('td', null, h('code', null, cycle.id), h('small', null, ` · ${cycle.status}`)),
      h('td', null, cycle.observed_merge_commit ? h('code', { title: cycle.observed_merge_commit }, `merge ${shortSha(cycle.observed_merge_commit)}`) : 'awaiting read-back'),
    )
  })
  return h('div', { className: 'autopilot-table-wrap' }, h('table', { className: 'autopilot-table' },
    h('thead', null, h('tr', null, h('th', null, 'Project / Wave'), h('th', null, 'Issue / PR / Head'), h('th', null, 'Intent / lane / lease'), h('th', null, 'Provision / worker / session / binding'), h('th', null, 'Materialization / commands'), h('th', null, 'Result / merge read-back'))),
    h('tbody', null, ...commandRows, ...mergeRows),
  ))
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
    SectionHeading({ title: 'Intent queue', count: value.queue.length, copy: 'Priority-ordered durable work with exact Intent, Wave, PR, Head and lane identities.' }),
    QueueTable(value),
    SectionHeading({ title: 'Correlated execution evidence', copy: 'Record one complete row before recovery, breaker resolution, retry decisions or cleanup. Lease tokens are intentionally excluded.' }),
    ExecutionEvidence(value),
    Warnings(value),
  )
}
