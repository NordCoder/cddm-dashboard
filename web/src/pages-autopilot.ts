import { AutopilotAction, autopilotApi } from './autopilot-api.js'
import { AutopilotStatus } from './autopilot-domain.js'
import { errorMessage, resourceContent, useResource } from './app-runtime.js'
import { AutopilotContent } from './ui-autopilot.js'

const h = React.createElement
const breakerCodes = [
  'library_resolution_failure', 'chatgpt_project_scope_mismatch', 'ambiguous_worker_result',
  'stale_candidate_head', 'merge_readback_mismatch', 'github_synchronization_unhealthy',
  'missing_exact_head_ci', 'worker_session_conflict', 'uncertain_browser_send',
  'provisioning_conflict', 'repeated_bounded_failure',
]

function BreakerForm(props: { status: AutopilotStatus; busy: boolean; onSubmit: (code: string, scope: 'project' | 'lane', lane: string, reason: string, evidence: string) => void }): unknown {
  const [code, setCode] = React.useState(breakerCodes[0])
  const [scope, setScope] = React.useState<'project' | 'lane'>('project')
  const [lane, setLane] = React.useState('')
  const [reason, setReason] = React.useState('')
  const [evidence, setEvidence] = React.useState('')
  const submit = (event: { preventDefault(): void }) => {
    event.preventDefault()
    if (!reason.trim() || (scope === 'lane' && !lane.trim())) return
    props.onSubmit(code, scope, lane.trim(), reason.trim(), evidence.trim())
  }
  return h('details', { className: 'autopilot-breaker-form' }, h('summary', null, 'Trip a circuit breaker'), h('form', { onSubmit: submit },
    h('label', null, h('span', null, 'Failure code'), h('select', { value: code, onChange: (event: { currentTarget: HTMLSelectElement }) => setCode(event.currentTarget.value) }, ...breakerCodes.map((value) => h('option', { key: value, value }, value)))),
    h('label', null, h('span', null, 'Scope'), h('select', { value: scope, onChange: (event: { currentTarget: HTMLSelectElement }) => setScope(event.currentTarget.value as 'project' | 'lane') }, h('option', { value: 'project' }, 'Project'), h('option', { value: 'lane' }, 'Lane'))),
    scope === 'lane' ? h('label', null, h('span', null, 'Exact lane key'), h('input', { value: lane, onChange: (event: { currentTarget: HTMLInputElement }) => setLane(event.currentTarget.value), required: true, placeholder: 'project:1:issue:84:implementor' })) : null,
    h('label', null, h('span', null, 'Reason'), h('textarea', { value: reason, onChange: (event: { currentTarget: HTMLTextAreaElement }) => setReason(event.currentTarget.value), required: true, rows: 3 })),
    h('label', null, h('span', null, 'Bounded evidence'), h('textarea', { value: evidence, onChange: (event: { currentTarget: HTMLTextAreaElement }) => setEvidence(event.currentTarget.value), rows: 3 })),
    h('button', { type: 'submit', className: 'button button--danger', disabled: props.busy }, 'Trip breaker'),
  ))
}

export function AutopilotPage(props: { projectID: number }): unknown {
  const resource = useResource<AutopilotStatus>(`autopilot:${props.projectID}`, (signal) => autopilotApi.status(props.projectID, signal), 15_000)
  const [busy, setBusy] = React.useState(false)
  const [feedback, setFeedback] = React.useState('')

  const apply = (operation: (status: AutopilotStatus) => Promise<AutopilotStatus>, success: string) => {
    if (busy || resource.state.kind !== 'ready') return
    setBusy(true)
    setFeedback('')
    void operation(resource.state.data).then(() => { setFeedback(success); resource.refresh() }).catch((error: unknown) => setFeedback(errorMessage(error))).finally(() => setBusy(false))
  }
  const control = (action: AutopilotAction) => apply((status) => autopilotApi.control(props.projectID, action, status.control.revision), `${action} recorded at the next operator revision.`)
  const transition = (breakerID: string, action: 'acknowledge' | 'resolve') => apply((status) => autopilotApi.transitionBreaker(props.projectID, breakerID, action, status.control.revision), `Circuit breaker ${action}d.`)
  const trip = (code: string, scope: 'project' | 'lane', lane: string, reason: string, evidence: string) => apply((status) => autopilotApi.tripBreaker(props.projectID, code, { expected_revision: status.control.revision, scope_kind: scope, lane_key: scope === 'lane' ? lane : undefined, reason, evidence }), 'Circuit breaker recorded.')

  return resourceContent(resource, (status) => h(AutopilotContent, {
    status, busy, feedback: feedback || undefined, onRefresh: resource.refresh, onControl: control,
    onAcknowledge: (id: string) => transition(id, 'acknowledge'), onResolve: (id: string) => transition(id, 'resolve'),
    breakerForm: h(BreakerForm, { status, busy, onSubmit: trip }),
  }), 'Loading Autopilot operations…')
}
