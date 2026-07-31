import assert from 'node:assert/strict'
import test from 'node:test'
import { parseAutopilotStatus } from '../dist/assets/autopilot-domain.js'

const timestamp = '2026-07-31T12:00:00Z'

function projection() {
  const intent = {
    intent_id: 'intent-one', project_id: 1, source_result_comment_id: 1, source_command_id: 'source-one', action_id: 'action-one',
    action_type: 'dispatch', repository: 'NordCoder/app', issue_number: 101, role: 'implementor', priority: 10,
    lane_key: 'project:1:issue:101:implementor', status: 'claimed', created_at: timestamp, updated_at: timestamp,
  }
  const lease = {
    lease_id: 'lease-one', project_id: 1, lane_key: intent.lane_key, intent_id: intent.intent_id,
    claim_id: 'claim-one', lease_owner: 'dashboard-autopilot', status: 'active', acquired_at: timestamp, expires_at: timestamp,
  }
  return {
    project_id: 1, repository: 'NordCoder/app', sync_status: 'healthy',
    profile: {
      project_id: 1, resource_version: 'cddm-dashboard-resources/v2.0', methodology_version: 'cddm-minimal/v2.1', result_protocol: 'cddm-worker-result/v2',
      delivery_mode: 'auto', qa_session_mode: 'manual_fresh_binding', auto_merge: false,
      autonomy_mode: 'continuous_dashboard_orchestration', autonomy_state: 'enabled', control_issue_number: 90,
      max_active_work_units: 3, max_parallel_implementors: 2, max_parallel_qa: 1, updated_at: timestamp,
    },
    control: { project_id: 1, revision: 1, last_action: 'enable', updated_at: timestamp },
    intents: [intent], queue: [{ intent: { ...intent }, waiting_reason: 'active lane lease' }],
    leases: [lease], active_leases: [{ ...lease }], provisioning: [], commands: [], results: [],
    circuit_breakers: [], warnings: [], merge_cycles: [],
    counts: {
      pending_intents: 0, blocked_intents: 0, claimed_intents: 1, active_leases: 1,
      pending_provisioning: 0, managed_sessions: 0, active_commands: 0,
      active_circuit_breakers: 0, ambiguous_records: 0,
    },
    lead_busy: false, next_action: 'Observe current durable work; do not replay or retarget active commands.', generated_at: timestamp,
  }
}

test('rejects an omitted authoritative active lease from active_leases', () => {
  const value = projection()
  value.active_leases = []
  assert.throws(() => parseAutopilotStatus(value), /active_leases.*complete active lease mirror/)
})

test('rejects duplicate active lease mirrors', () => {
  const value = projection()
  value.active_leases.push({ ...value.active_leases[0] })
  value.counts.active_leases = 2
  assert.throws(() => parseAutopilotStatus(value), /active_leases\[1\]\.lease_id.*unique active lease identity/)
})

test('rejects an active lease count that disagrees with the exact mirror', () => {
  const value = projection()
  value.counts.active_leases = 0
  assert.throws(() => parseAutopilotStatus(value), /counts\.active_leases/)
})

test('requires exact unique queue membership for every runnable Intent', () => {
  const missing = projection()
  missing.queue = []
  assert.throws(() => parseAutopilotStatus(missing), /queue.*exact queue membership/)

  const duplicate = projection()
  duplicate.queue.push(structuredClone(duplicate.queue[0]))
  assert.throws(() => parseAutopilotStatus(duplicate), /queue\[1\]\.intent\.intent_id.*unique queued Intent identity/)
})

test('rejects multiple provisioning records for one authoritative Intent', () => {
  const value = projection()
  value.provisioning = [
    {
      id: 'provision-one', project_id: 1, intent_id: 'intent-one', lease_id: 'lease-one', lane_key: 'project:1:issue:101:implementor',
      issue_number: 101, role: 'implementor', status: 'pending', created_at: timestamp, updated_at: timestamp,
    },
    {
      id: 'provision-two', project_id: 1, intent_id: 'intent-one', lease_id: 'lease-one', lane_key: 'project:1:issue:101:implementor',
      issue_number: 101, role: 'implementor', status: 'pending', created_at: timestamp, updated_at: timestamp,
    },
  ]
  assert.throws(() => parseAutopilotStatus(value), /provisioning\[1\]\.intent_id.*one provisioning record per Intent/)
})

test('rejects every derived metric that disagrees with authoritative arrays', () => {
  const replacements = {
    pending_intents: 1,
    blocked_intents: 1,
    claimed_intents: 0,
    pending_provisioning: 1,
    managed_sessions: 1,
    active_commands: 1,
    active_circuit_breakers: 1,
  }
  for (const [field, replacement] of Object.entries(replacements)) {
    const value = projection()
    value.counts[field] = replacement
    assert.throws(() => parseAutopilotStatus(value), new RegExp(`counts\\.${field}`))
  }
})

test('rejects an ambiguous count below the represented evidence', () => {
  const value = projection()
  const ambiguous = {
    ...value.intents[0],
    intent_id: 'intent-ambiguous', source_result_comment_id: 2, source_command_id: 'source-two', action_id: 'action-two',
    issue_number: 102, lane_key: 'project:1:issue:102:implementor', status: 'ambiguous',
  }
  value.intents.push(ambiguous)
  value.queue.push({ intent: { ...ambiguous }, waiting_reason: 'ambiguous evidence requires operator recovery' })
  value.counts.ambiguous_records = 0
  assert.throws(() => parseAutopilotStatus(value), /counts\.ambiguous_records/)
})

test('rejects lead_busy that disagrees with the active Lead lease', () => {
  const value = projection()
  value.intents[0].role = 'lead'
  value.intents[0].lane_key = 'project:1:lead'
  value.queue[0].intent.role = 'lead'
  value.queue[0].intent.lane_key = 'project:1:lead'
  value.leases[0].lane_key = 'project:1:lead'
  value.active_leases[0].lane_key = 'project:1:lead'
  value.lead_busy = false
  assert.throws(() => parseAutopilotStatus(value), /lead_busy/)
})

test('rejects a Project hold reason that is absent from blocked Project evidence', () => {
  const value = projection()
  const hold = {
    ...value.intents[0],
    intent_id: 'intent-hold', source_result_comment_id: 3, source_command_id: 'source-hold', action_id: 'action-hold',
    action_type: 'hold', status: 'blocked', reason_code: 'owner_required', priority: 1,
  }
  delete hold.issue_number
  delete hold.role
  delete hold.lane_key
  value.intents.unshift(hold)
  value.queue.unshift({ intent: { ...hold }, waiting_reason: 'owner_required' })
  value.counts.blocked_intents = 1
  assert.throws(() => parseAutopilotStatus(value), /project_hold_reason/)
})

test('accepts the first non-empty Project hold reason after a blank blocked record', () => {
  const value = projection()
  const blank = {
    ...value.intents[0], intent_id: 'intent-hold-blank', source_result_comment_id: 4,
    source_command_id: 'source-hold-blank', action_id: 'action-hold-blank', action_type: 'hold', status: 'blocked', priority: 1,
  }
  const owner = {
    ...value.intents[0], intent_id: 'intent-hold-owner', source_result_comment_id: 5,
    source_command_id: 'source-hold-owner', action_id: 'action-hold-owner', action_type: 'hold', status: 'blocked', reason_code: 'owner_required', priority: 2,
  }
  for (const item of [blank, owner]) {
    delete item.issue_number
    delete item.role
    delete item.lane_key
  }
  delete blank.reason_code
  value.intents.unshift(blank, owner)
  value.queue.unshift(
    { intent: { ...blank }, waiting_reason: 'blocked' },
    { intent: { ...owner }, waiting_reason: 'owner_required' },
  )
  value.counts.blocked_intents = 2
  value.project_hold_reason = 'owner_required'
  value.next_action = 'Resolve the Project hold or owner-required condition.'
  assert.doesNotThrow(() => parseAutopilotStatus(value))
})

test('rejects queue guidance that disagrees with control and lane state', () => {
  const value = projection()
  value.queue[0].waiting_reason = 'ready'
  assert.throws(() => parseAutopilotStatus(value), /queue\[0\]\.waiting_reason/)
})

test('rejects next_action that disagrees with authoritative current work', () => {
  const value = projection()
  value.next_action = 'No automatic work is queued. The persistent Lead may plan the next bounded Wave.'
  assert.throws(() => parseAutopilotStatus(value), /next_action/)
})

test('rejects two active leases occupying the same scheduler lane', () => {
  const value = projection()
  const secondIntent = {
    ...value.intents[0], intent_id: 'intent-two', source_result_comment_id: 6,
    source_command_id: 'source-two', action_id: 'action-two', issue_number: 102,
  }
  const secondLease = {
    ...value.leases[0], lease_id: 'lease-two', intent_id: secondIntent.intent_id, claim_id: 'claim-two',
  }
  value.intents.push(secondIntent)
  value.queue.push({ intent: { ...secondIntent }, waiting_reason: 'active lane lease' })
  value.leases.push(secondLease)
  value.active_leases.push({ ...secondLease })
  value.counts.claimed_intents = 2
  value.counts.active_leases = 2
  assert.throws(() => parseAutopilotStatus(value), /active_leases\[1\]\.lane_key/)
})

test('rejects duplicate Issue identities inside the active Wave', () => {
  const value = projection()
  value.active_wave = {
    project_id: 1, wave_id: 'wave-one', control_issue_number: 90, source_command_id: 'source-wave',
    status: 'active', issues: [101, 101], created_at: timestamp, updated_at: timestamp,
  }
  assert.throws(() => parseAutopilotStatus(value), /active_wave\.issues\[1\]/)
})
