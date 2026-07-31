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
    lead_busy: false, next_action: 'Observe current durable work.', generated_at: timestamp,
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
