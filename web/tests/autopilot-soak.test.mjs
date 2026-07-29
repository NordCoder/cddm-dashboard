import assert from 'node:assert/strict'
import test from 'node:test'
import { parseAutopilotStatus } from '../dist/assets/autopilot-domain.js'

const head = '241401d9f5c1fb2004eeb19ec612323f74b57199'
const timestamp = '2026-07-29T17:00:00Z'

function projection() {
  return {
    project_id: 9,
    repository: 'NordCoder/cddm-dashboard',
    sync_status: 'healthy',
    profile: {
      autonomy_mode: 'continuous_dashboard_orchestration', autonomy_state: 'paused', control_issue_number: 90,
      resource_version: 'cddm-dashboard-resources/v2.0', methodology_version: 'cddm-minimal/v2.1',
      result_protocol: 'cddm-worker-result/v2', chatgpt_project_url: 'https://chatgpt.com/g/g-project/repository/project',
    },
    control: { project_id: 9, revision: 12, last_action: 'pause', updated_at: timestamp },
    active_wave: { wave_id: 'wave-soak', status: 'active', issues: [101, 102, 103] },
    queue: [{
      intent: { intent_id: 'intent-qa', action_type: 'dispatch', issue_number: 101, role: 'qa', pr_number: 150, expected_head: head, priority: 20, lane_key: 'project:9:issue:101:qa:head', status: 'pending' },
      waiting_reason: 'autonomy_paused',
    }],
    active_leases: [{ lease_id: 'lease-one', lane_key: 'project:9:issue:102:implementor', intent_id: 'intent-impl', lease_owner: 'dashboard-autopilot', status: 'active', acquired_at: timestamp, expires_at: timestamp }],
    provisioning: [{ id: 'provision-one', intent_id: 'intent-impl', lane_key: 'project:9:issue:102:implementor', issue_number: 102, role: 'implementor', expected_head: head, status: 'provisioned', worker_id: 'worker-one', observed_chatgpt_url: 'https://chatgpt.com/g/g-project/repository/c/one', bound_binding_id: 'binding-one', updated_at: timestamp }],
    commands: [{ materialization_id: 'materialization-one', intent_id: 'intent-impl', lane_key: 'project:9:issue:102:implementor', status: 'materialized', workflow_command_id: 'cmd-one', workflow_status: 'awaiting_result', delivery_command_id: 'delivery-one', delivery_status: 'delivered', updated_at: timestamp }],
    circuit_breakers: [{ id: 'breaker-one', scope_kind: 'lane', lane_key: 'project:9:issue:103:qa:head', code: 'missing_exact_head_ci', reason: 'CI is not conclusive', recovery_requirements: 'Obtain exact-Head CI.', expected_head: head, status: 'acknowledged', occurrence_count: 2, updated_at: timestamp }],
    warnings: [{ code: 'stale_candidate_head', intent_id: 'intent-qa', issue_number: 101, pr_number: 150, expected_head: head, observed_head: '', message: 'The synchronized PR Head does not match.' }],
    counts: { pending_intents: 1, blocked_intents: 0, claimed_intents: 1, active_leases: 1, pending_provisioning: 0, managed_sessions: 1, active_commands: 1, active_circuit_breakers: 1, ambiguous_records: 0 },
    project_hold_reason: '', lead_busy: false, next_action: 'Resolve active circuit breakers before new automatic work.', generated_at: timestamp,
  }
}

test('parses exact identities across the full Autopilot soak projection', () => {
  const parsed = parseAutopilotStatus(projection())
  assert.equal(parsed.active_wave.issues.length, 3)
  assert.equal(parsed.queue[0].intent.expected_head, head)
  assert.equal(parsed.active_leases[0].lease_id, 'lease-one')
  assert.equal(parsed.provisioning[0].bound_binding_id, 'binding-one')
  assert.equal(parsed.commands[0].workflow_command_id, 'cmd-one')
  assert.equal(parsed.circuit_breakers[0].expected_head, head)
  assert.equal(parsed.warnings[0].expected_head, head)
})

test('fails closed on malformed nested lease identity', () => {
  const value = projection()
  value.active_leases[0].lease_id = 42
  assert.throws(() => parseAutopilotStatus(value), /expected string/)
})
