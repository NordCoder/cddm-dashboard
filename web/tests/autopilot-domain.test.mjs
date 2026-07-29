import assert from 'node:assert/strict'
import test from 'node:test'
import { parseAutopilotStatus } from '../dist/autopilot-domain.js'

const base = {
  project_id: 1, repository: 'NordCoder/app', sync_status: 'healthy',
  profile: { autonomy_mode: 'continuous_dashboard_orchestration', autonomy_state: 'enabled', control_issue_number: 90, resource_version: 'cddm-dashboard-resources/v2.0', methodology_version: 'cddm-minimal/v2.1', result_protocol: 'cddm-worker-result/v2' },
  control: { project_id: 1, revision: 4, last_action: 'resume', updated_at: '2026-07-29T00:00:00Z' },
  queue: [], active_leases: [], provisioning: [], commands: [], circuit_breakers: [], warnings: [],
  counts: { pending_intents: 0, blocked_intents: 0, claimed_intents: 0, active_leases: 0, pending_provisioning: 0, managed_sessions: 0, active_commands: 0, active_circuit_breakers: 0, ambiguous_records: 0 },
  lead_busy: false, next_action: 'No automatic work is queued.', generated_at: '2026-07-29T00:00:00Z',
}

test('parses Autopilot operations projection', () => {
  const parsed = parseAutopilotStatus(base)
  assert.equal(parsed.control.revision, 4)
  assert.equal(parsed.profile.autonomy_state, 'enabled')
})

test('rejects malformed optimistic revision', () => {
  assert.throws(() => parseAutopilotStatus({ ...base, control: { ...base.control, revision: '4' } }), /expected finite number/)
})
