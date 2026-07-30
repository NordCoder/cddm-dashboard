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
  const implementor = intent()
  const qa = intent({ intent_id: 'intent-qa', source_result_comment_id: 7002, source_command_id: 'source-command-two', action_id: 'action-two', issue_number: 101, role: 'qa', pr_number: 150, priority: 20, lane_key: 'project:9:issue:101:qa:head', status: 'pending' })
  const merge = intent({ intent_id: 'intent-merge', source_result_comment_id: 7003, source_command_id: 'source-command-three', action_id: 'action-three', action_type: 'merge_candidate', issue_number: 103, role: 'lead', pr_number: 152, priority: 30, lane_key: 'project:9:lead', status: 'pending' })
  const lease = { lease_id: 'lease-one', project_id: 9, lane_key: implementor.lane_key, intent_id: implementor.intent_id, claim_id: 'claim-one', lease_owner: 'dashboard-autopilot', status: 'active', acquired_at: timestamp, expires_at: timestamp }
  return {
    project_id: 9,
    repository: 'NordCoder/cddm-dashboard',
    sync_status: 'healthy',
    profile: {
      project_id: 9, resource_version: 'cddm-dashboard-resources/v2.0', methodology_version: 'cddm-minimal/v2.1', result_protocol: 'cddm-worker-result/v2',
      delivery_mode: 'auto', qa_session_mode: 'manual_fresh_binding', auto_merge: false, autonomy_mode: 'continuous_dashboard_orchestration', autonomy_state: 'paused',
      control_issue_number: 90, max_active_work_units: 3, max_parallel_implementors: 1, max_parallel_qa: 1,
      chatgpt_project_url: 'https://chatgpt.com/g/g-project/repository/project', updated_at: timestamp,
    },
    control: { project_id: 9, revision: 12, last_action: 'pause', updated_at: timestamp },
    active_wave: { project_id: 9, wave_id: 'wave-soak', control_issue_number: 90, source_command_id: 'source-wave-command', status: 'active', issues: [101, 102, 103], created_at: timestamp, updated_at: timestamp },
    intents: [implementor, qa, merge],
    queue: [
      { intent: { ...implementor }, waiting_reason: 'active lane lease' },
      { intent: { ...qa }, waiting_reason: 'autonomy_paused' },
      { intent: { ...merge }, waiting_reason: 'autonomy_paused' },
    ],
    leases: [lease],
    active_leases: [{ ...lease }],
    provisioning: [{
      id: 'provision-one', project_id: 9, intent_id: implementor.intent_id, lease_id: lease.lease_id, lane_key: implementor.lane_key,
      issue_number: 102, role: 'implementor', expected_head: head, status: 'provisioned', worker_id: 'worker-one', worker_session_id: 'session-one',
      tab_id: 44, observed_chatgpt_url: 'https://chatgpt.com/g/g-project/repository/c/one', bound_binding_id: 'binding-one', bound_binding_version: 3,
      created_at: timestamp, updated_at: timestamp,
    }],
    commands: [{
      project_id: 9, materialization_id: 'materialization-one', intent_id: implementor.intent_id, lease_id: lease.lease_id,
      provision_request_id: 'provision-one', lane_key: implementor.lane_key, issue_number: 102, role: 'implementor', expected_head: head,
      status: 'materialized', workflow_command_id: 'cmd-one', workflow_status: 'awaiting_result', delivery_command_id: 'delivery-one', delivery_status: 'delivered',
      worker_id: 'worker-one', worker_session_id: 'session-one', tab_id: 44, binding_id: 'binding-one', binding_version: 3,
      observed_chatgpt_url: 'https://chatgpt.com/g/g-project/repository/c/one', context_hash: contextHash, prompt_hash: promptHash, updated_at: timestamp,
    }],
    results: [{ project_id: 9, github_comment_id: 88001, issue_number: 102, command_id: 'cmd-one', role: 'implementor', result: 'candidate_ready', payload_hash: 'result-hash', validation_status: 'accepted', accepted_at: timestamp, observed_at: timestamp }],
    circuit_breakers: [{ id: 'breaker-one', project_id: 9, scope_kind: 'lane', lane_key: 'project:9:issue:103:qa:head', code: 'missing_exact_head_ci', reason: 'CI is not conclusive', recovery_requirements: 'Obtain exact-Head CI.', expected_head: head, status: 'acknowledged', occurrence_count: 2, created_at: timestamp, updated_at: timestamp, acknowledged_at: timestamp }],
    warnings: [{ code: 'stale_candidate_head', intent_id: 'intent-qa', issue_number: 101, pr_number: 150, expected_head: head, observed_head: '', message: 'The synchronized PR Head does not match.' }],
    merge_cycles: [{ id: 'merge-cycle-one', project_id: 9, intent_id: 'intent-merge', issue_number: 103, pr_number: 152, approved_head: head, observed_merge_commit: mergeCommit, status: 'observed', updated_at: timestamp }],
    counts: { pending_intents: 2, blocked_intents: 0, claimed_intents: 1, active_leases: 1, pending_provisioning: 0, managed_sessions: 1, active_commands: 1, active_circuit_breakers: 1, ambiguous_records: 0 },
    project_hold_reason: '', lead_busy: false, next_action: 'Resolve active circuit breakers before new automatic work.', generated_at: timestamp,
  }
}

test('parses and preserves the complete exact-identity Autopilot projection', () => {
  const parsed = parseAutopilotStatus(projection())
  assert.equal(parsed.profile.project_id, 9)
  assert.equal(parsed.active_wave.source_command_id, 'source-wave-command')
  assert.equal(parsed.intents[0].intent_id, 'intent-impl')
  assert.equal(parsed.leases[0].claim_id, 'claim-one')
  assert.equal(parsed.provisioning[0].lease_id, 'lease-one')
  assert.equal(parsed.provisioning[0].worker_session_id, 'session-one')
  assert.equal(parsed.commands[0].provision_request_id, 'provision-one')
  assert.equal(parsed.commands[0].binding_version, 3)
  assert.equal(parsed.results[0].github_comment_id, 88001)
  assert.equal(parsed.merge_cycles[0].observed_merge_commit, mergeCommit)
  assert.equal('lease_token' in parsed.leases[0], false)
})

test('requires every authoritative top-level collection', () => {
  for (const field of ['intents', 'queue', 'leases', 'active_leases', 'provisioning', 'commands', 'results', 'circuit_breakers', 'warnings', 'merge_cycles']) {
    const value = projection()
    delete value[field]
    assert.throws(() => parseAutopilotStatus(value), new RegExp(`\\$\\.${field}.*expected array`))
  }
})

test('rejects every conflicting active lease mirror field', () => {
  const mutations = {
    project_id: 10,
    intent_id: 'intent-qa',
    lane_key: 'project:9:issue:999:implementor',
    claim_id: 'claim-other',
    lease_owner: 'other-owner',
    acquired_at: '2026-07-29T18:00:00Z',
    expires_at: '2026-07-29T19:00:00Z',
    released_at: '2026-07-29T20:00:00Z',
  }
  for (const [field, replacement] of Object.entries(mutations)) {
    const value = projection()
    value.active_leases[0][field] = replacement
    assert.throws(() => parseAutopilotStatus(value), new RegExp(`active_leases\\[0\\]\\.${field}`))
  }
})

test('rejects provisioned request without durable managed session identity', () => {
  const value = projection()
  delete value.provisioning[0].worker_session_id
  assert.throws(() => parseAutopilotStatus(value), /provisioning\[0\]\.worker_session_id/)
})

test('rejects orphaned provisioning Intent and lease identities', () => {
  const missingIntent = projection()
  missingIntent.provisioning[0].intent_id = 'intent-missing'
  assert.throws(() => parseAutopilotStatus(missingIntent), /provisioning\[0\]\.intent_id/)

  const missingLease = projection()
  missingLease.provisioning[0].lease_id = 'lease-missing'
  assert.throws(() => parseAutopilotStatus(missingLease), /provisioning\[0\]\.lease_id/)
})

test('rejects orphaned command Intent, lease and provisioning identities', () => {
  const missingIntent = projection()
  missingIntent.commands[0].intent_id = 'intent-missing'
  assert.throws(() => parseAutopilotStatus(missingIntent), /commands\[0\]\.intent_id/)

  const missingLease = projection()
  missingLease.commands[0].lease_id = 'lease-missing'
  assert.throws(() => parseAutopilotStatus(missingLease), /commands\[0\]\.lease_id/)

  const missingProvision = projection()
  missingProvision.commands[0].provision_request_id = 'provision-missing'
  assert.throws(() => parseAutopilotStatus(missingProvision), /commands\[0\]\.provision_request_id/)
})

test('rejects orphaned merge-cycle and result command identities', () => {
  const missingMergeIntent = projection()
  missingMergeIntent.merge_cycles[0].intent_id = 'intent-missing'
  assert.throws(() => parseAutopilotStatus(missingMergeIntent), /merge_cycles\[0\]\.intent_id/)

  const missingResultCommand = projection()
  missingResultCommand.results[0].command_id = 'cmd-missing'
  assert.throws(() => parseAutopilotStatus(missingResultCommand), /results\[0\]\.command_id/)
})

test('rejects conflicting provisioning and command relationships', () => {
  const wrongLeaseIntent = projection()
  wrongLeaseIntent.leases[0].intent_id = 'intent-qa'
  assert.throws(() => parseAutopilotStatus(wrongLeaseIntent), /leases\[0\]\.lane_key|provisioning\[0\]\.lease_id/)

  const wrongSession = projection()
  wrongSession.commands[0].worker_session_id = 'session-other'
  assert.throws(() => parseAutopilotStatus(wrongSession), /commands\[0\]\.worker_session_id/)

  const wrongHead = projection()
  wrongHead.commands[0].expected_head = '2'.repeat(40)
  assert.throws(() => parseAutopilotStatus(wrongHead), /commands\[0\]\.expected_head/)
})

test('rejects incomplete command and managed-session identity', () => {
  for (const field of ['workflow_command_id', 'delivery_command_id', 'worker_id', 'worker_session_id', 'tab_id', 'binding_id']) {
    const value = projection()
    delete value.commands[0][field]
    assert.throws(() => parseAutopilotStatus(value), new RegExp(`commands\\[0\\]\\.${field}`))
  }
})

test('rejects an Issue-scoped action without exact Issue identity', () => {
  const value = projection()
  delete value.intents[0].issue_number
  assert.throws(() => parseAutopilotStatus(value), /intents\[0\]\.issue_number/)
})

test('rejects mismatched project and merge identities', () => {
  const wrongProject = projection()
  wrongProject.leases[0].project_id = 10
  assert.throws(() => parseAutopilotStatus(wrongProject), /leases\[0\]\.project_id/)

  const wrongMergePR = projection()
  wrongMergePR.merge_cycles[0].pr_number = 999
  assert.throws(() => parseAutopilotStatus(wrongMergePR), /merge_cycles\[0\]\.pr_number/)
})
