import assert from 'node:assert/strict'
import test from 'node:test'
import { parseAutopilotStatus } from '../dist/assets/autopilot-domain.js'

const timestamp = '2026-07-29T00:00:00Z'
const base = {
  project_id: 1, repository: 'NordCoder/app', sync_status: 'healthy',
  profile: {
    project_id: 1, resource_version: 'cddm-dashboard-resources/v2.0', methodology_version: 'cddm-minimal/v2.1', result_protocol: 'cddm-worker-result/v2',
    delivery_mode: 'auto', qa_session_mode: 'manual_fresh_binding', auto_merge: false,
    autonomy_mode: 'continuous_dashboard_orchestration', autonomy_state: 'enabled', control_issue_number: 90,
    max_active_work_units: 3, max_parallel_implementors: 2, max_parallel_qa: 1, chatgpt_project_url: 'https://chatgpt.com/g/g-project/app', updated_at: timestamp,
  },
  control: { project_id: 1, revision: 4, last_action: 'resume', updated_at: timestamp },
  queue: [], active_leases: [], provisioning: [], commands: [], circuit_breakers: [], warnings: [], merge_cycles: [],
  counts: { pending_intents: 0, blocked_intents: 0, claimed_intents: 0, active_leases: 0, pending_provisioning: 0, managed_sessions: 0, active_commands: 0, active_circuit_breakers: 0, ambiguous_records: 0 },
  lead_busy: false, next_action: 'No automatic work is queued.', generated_at: timestamp,
}

test('parses Autopilot operations projection', () => {
  const parsed = parseAutopilotStatus(base)
  assert.equal(parsed.control.revision, 4)
  assert.equal(parsed.profile.autonomy_state, 'enabled')
  assert.deepEqual(parsed.merge_cycles, [])
})

test('accepts the exact disabled manual profile without inventing a Control Issue', () => {
  const manual = structuredClone(base)
  manual.profile = {
    ...manual.profile,
    resource_version: 'cddm-dashboard-resources/v1.0', methodology_version: 'cddm-minimal/v2.0', result_protocol: 'cddm-worker-result/v1',
    delivery_mode: 'reviewed', autonomy_mode: 'manual_owner_dispatch', autonomy_state: 'disabled', control_issue_number: 0, chatgpt_project_url: '',
  }
  const parsed = parseAutopilotStatus(manual)
  assert.equal(parsed.profile.control_issue_number, 0)
})

test('rejects malformed optimistic revision', () => {
  assert.throws(() => parseAutopilotStatus({ ...base, control: { ...base.control, revision: '4' } }), /expected finite number/)
})

test('rejects frontend projection that grants Dashboard merge authority', () => {
  assert.throws(() => parseAutopilotStatus({ ...base, profile: { ...base.profile, auto_merge: true } }), /profile\.auto_merge/)
})
