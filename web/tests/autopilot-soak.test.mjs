import assert from 'node:assert/strict'
import test from 'node:test'
import { parseAutopilotStatus } from '../dist/assets/autopilot-domain.js'

const head = '241401d9f5c1fb2004eeb19ec612323f74b57199'
const mergeCommit = '3a8fdb761a881207dfc45bb3a6876fde76bc1538'
const timestamp = '2026-07-29T17:00:00Z'
const contextHash = 'a'.repeat(64)
const promptHash = 'b'.repeat(64)

function intent(overrides = {}) {
  return {
    intent_id: 'intent-impl', project_id: 9, source_result_comment_id: 7001, source_command_id: 'source-command-one', action_id: 'action-one',
    action_type: 'dispatch', repository: 'NordCoder/cddm-dashboard', issue_number: 102, role: 'implementor', pr_number: 151,
    expected_head: head, expected_previous_head: '1111111111111111111111111111111111111111', reason_code: 'qa_changes_required', decision_category: 'continue',
    wave_id: 'wave-soak', priority: 10, lane_key: 'project:9:issue:102:implementor', status: 'claimed', created_at: timestamp, updated_at: timestamp,
    ...overrides,
  }
}

function projection() {
  return {
    project_id: 9,
    repository: 'NordCoder/cddm-dashboard',
    sync_status: 'healthy',
    profile: {
      project_id: 9, resource_version: 'cddm-dashboard-resources/v2.0', methodology_version: 'cddm-minimal/v2.1', result_protocol: 'cddm-worker-result/v2',
      delivery_mode: 'chatgpt_project', qa_session_mode: 'fresh', auto_merge: false, autonomy_mode: 'continuous_dashboard_orchestration', autonomy_state: 'paused',
      control_issue_number: 90, max_active_work_units: 3, max_parallel_implementors: 1, max_parallel_qa: 1,
      chatgpt_project_url: 'https://chatgpt.com/g/g-project/repository/project', updated_at: timestamp,
    },
    control: { project_id: 9, revision: 12, last_action: 'pause', updated_at: timestamp },
    active_wave: { project_id: 9, wave_id: 'wave-soak', control_issue_number: 90, source_command_id: 'source-wave-command', status: 'active', issues: [101, 102, 103], created_at: timestamp, updated_at: timestamp },
    queue: [
      { intent: intent(), waiting_reason: 'active lane lease' },
      { intent: intent({ intent_id: 'intent-qa', source_result_comment_id: 7002, source_command_id: 'source-command-two', action_id: 'action-two', issue_number: 101, role: 'qa', pr_number: 150, priority: 20, lane_key: 'project:9:issue:101:qa:head', status: 'pending' }), waiting_reason: 'autonomy_paused' },
      { intent: intent({ intent_id: 'intent-merge', source_result_comment_id: 7003, source_command_id: 'source-command-three', action_id: 'action-three', action_type: 'merge_candidate', issue_number: 103, role: 'lead', pr_number: 152, priority: 30, lane_key: 'project:9:lead', status: 'pending' }), waiting_reason: 'autonomy_paused' },
    ],
    active_leases: [{ lease_id: 'lease-one', project_id: 9, lane_key: 'project:9:issue:102:implementor', intent_id: 'intent-impl', claim_id: 'claim-one', lease_owner: 'dashboard-autopilot', lease_token: 'lease-token-one', status: 'active', acquired_at: timestamp, expires_at: timestamp }],
    provisioning: [{ id: 'provision-one', intent_id: 'intent-impl', lane_key: 'project:9:issue:102:implementor', issue_number: 102, role: 'implementor', expected_head: head, status: 'provisioned', worker_id: 'worker-one', tab_id: 44, observed_chatgpt_url: 'https://chatgpt.com/g/g-project/repository/c/one', bound_binding_id: 'binding-one', bound_binding_version: 3, created_at: timestamp, updated_at: timestamp }],
    commands: [{ materialization_id: 'materialization-one', intent_id: 'intent-impl', lease_id: 'lease-one', lane_key: 'project:9:issue:102:implementor', status: 'materialized', workflow_command_id: 'cmd-one', workflow_status: 'awaiting_result', delivery_command_id: 'delivery-one', delivery_status: 'delivered', context_hash: contextHash, prompt_hash: promptHash, updated_at: timestamp }],
    circuit_breakers: [{ id: 'breaker-one', project_id: 9, scope_kind: 'lane', lane_key: 'project:9:issue:103:qa:head', code: 'missing_exact_head_ci', reason: 'CI is not conclusive', recovery_requirements: 'Obtain exact-Head CI.', expected_head: head, status: 'acknowledged', occurrence_count: 2, created_at: timestamp, updated_at: timestamp, acknowledged_at: timestamp }],
    warnings: [{ code: 'stale_candidate_head', intent_id: 'intent-qa', issue_number: 101, pr_number: 150, expected_head: head, observed_head: '', message: 'The synchronized PR Head does not match.' }],
    merge_cycles: [{ id: 'merge-cycle-one', intent_id: 'intent-merge', issue_number: 103, pr_number: 152, approved_head: head, observed_merge_commit: mergeCommit, status: 'observed', updated_at: timestamp }],
    counts: { pending_intents: 2, blocked_intents: 0, claimed_intents: 1, active_leases: 1, pending_provisioning: 0, managed_sessions: 1, active_commands: 1, active_circuit_breakers: 1, ambiguous_records: 0 },
    project_hold_reason: '', lead_busy: false, next_action: 'Resolve active circuit breakers before new automatic work.', generated_at: timestamp,
  }
}

test('parses and preserves the complete exact-identity Autopilot projection', () => {
  const parsed = parseAutopilotStatus(projection())
  assert.equal(parsed.profile.project_id, 9)
  assert.equal(parsed.profile.max_parallel_qa, 1)
  assert.equal(parsed.active_wave.source_command_id, 'source-wave-command')
  assert.equal(parsed.queue[0].intent.source_result_comment_id, 7001)
  assert.equal(parsed.queue[0].intent.expected_previous_head, '1111111111111111111111111111111111111111')
  assert.equal(parsed.active_leases[0].claim_id, 'claim-one')
  assert.equal(parsed.active_leases[0].lease_token, 'lease-token-one')
  assert.equal(parsed.provisioning[0].tab_id, 44)
  assert.equal(parsed.provisioning[0].bound_binding_version, 3)
  assert.equal(parsed.commands[0].lease_id, 'lease-one')
  assert.equal(parsed.commands[0].context_hash, contextHash)
  assert.equal(parsed.commands[0].prompt_hash, promptHash)
  assert.equal(parsed.merge_cycles[0].approved_head, head)
  assert.equal(parsed.merge_cycles[0].observed_merge_commit, mergeCommit)
})

test('requires every authoritative top-level collection', () => {
  for (const field of ['queue', 'active_leases', 'provisioning', 'commands', 'circuit_breakers', 'warnings', 'merge_cycles']) {
    const value = projection()
    delete value[field]
    assert.throws(() => parseAutopilotStatus(value), new RegExp(`\\$\\.${field}.*expected array`))
  }
})

test('rejects an Issue-scoped dispatch without Issue identity', () => {
  const value = projection()
  delete value.queue[0].intent.issue_number
  assert.throws(() => parseAutopilotStatus(value), /queue\[0\]\.intent\.issue_number/)
})

test('rejects incomplete binding identity', () => {
  const value = projection()
  delete value.provisioning[0].bound_binding_version
  assert.throws(() => parseAutopilotStatus(value), /bound_binding_version/)
})

test('rejects a command without its lease identity', () => {
  const value = projection()
  delete value.commands[0].lease_id
  assert.throws(() => parseAutopilotStatus(value), /commands\[0\]\.lease_id/)
})

test('rejects a nested lane identity that conflicts with the referenced Intent', () => {
  const value = projection()
  value.commands[0].lane_key = 'project:9:issue:999:implementor'
  assert.throws(() => parseAutopilotStatus(value), /commands\[0\]\.lane_key/)
})

test('rejects incomplete merge read-back identity', () => {
  const value = projection()
  delete value.merge_cycles[0].approved_head
  assert.throws(() => parseAutopilotStatus(value), /merge_cycles\[0\]\.approved_head/)
})

test('rejects mismatched project identity in nested authoritative data', () => {
  const value = projection()
  value.active_leases[0].project_id = 10
  assert.throws(() => parseAutopilotStatus(value), /active_leases\[0\]\.project_id/)
})

test('rejects incomplete provisioned worker and exact-tab identities', () => {
  for (const field of ['worker_id', 'tab_id']) {
    const value = projection()
    delete value.provisioning[0][field]
    assert.throws(() => parseAutopilotStatus(value), new RegExp(`provisioning\\[0\\]\\.${field}`))
  }
})

test('rejects workflow and delivery statuses without their command identities', () => {
  const missingWorkflow = projection()
  delete missingWorkflow.commands[0].workflow_command_id
  assert.throws(() => parseAutopilotStatus(missingWorkflow), /commands\[0\]\.workflow_command_id/)

  const missingDelivery = projection()
  delete missingDelivery.commands[0].delivery_command_id
  assert.throws(() => parseAutopilotStatus(missingDelivery), /commands\[0\]\.delivery_command_id/)
})

test('rejects a merge Intent without exact PR and Head identities', () => {
  const missingPR = projection()
  delete missingPR.queue[2].intent.pr_number
  assert.throws(() => parseAutopilotStatus(missingPR), /queue\[2\]\.intent\.pr_number/)

  const missingHead = projection()
  delete missingHead.queue[2].intent.expected_head
  assert.throws(() => parseAutopilotStatus(missingHead), /queue\[2\]\.intent\.expected_head/)
})

test('rejects an active lease that references an absent Intent', () => {
  const value = projection()
  value.active_leases[0].intent_id = 'intent-missing'
  assert.throws(() => parseAutopilotStatus(value), /active_leases\[0\]\.intent_id/)
})

test('rejects merge read-back identities that conflict with the merge Intent', () => {
  const value = projection()
  value.merge_cycles[0].pr_number = 999
  assert.throws(() => parseAutopilotStatus(value), /merge_cycles\[0\]\.pr_number/)
})
