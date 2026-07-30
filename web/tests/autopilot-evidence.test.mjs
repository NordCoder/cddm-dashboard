import assert from 'node:assert/strict'
import test from 'node:test'
import { buildAutopilotEvidenceRows } from '../dist/assets/autopilot-evidence.js'

const timestamp = '2026-07-30T07:00:00Z'

function intent(id, issue, role, lane) {
  return {
    intent_id: id, project_id: 9, source_result_comment_id: 1, source_command_id: 'source', action_id: id,
    action_type: 'dispatch', repository: 'NordCoder/cddm-dashboard', issue_number: issue, role,
    priority: issue, lane_key: lane, status: 'claimed', created_at: timestamp, updated_at: timestamp,
  }
}

test('builds correlated rows for claimed and standalone provisioned Intents without commands', () => {
  const claimed = intent('intent-claimed', 101, 'implementor', 'project:9:issue:101:implementor')
  const provisioned = intent('intent-provisioned', 102, 'qa', 'project:9:issue:102:qa:head')
  const status = {
    intents: [claimed, provisioned],
    leases: [
      { lease_id: 'lease-claimed', project_id: 9, lane_key: claimed.lane_key, intent_id: claimed.intent_id, claim_id: 'claim-claimed', lease_owner: 'dashboard-autopilot', status: 'active', acquired_at: timestamp, expires_at: timestamp },
      { lease_id: 'lease-provisioned', project_id: 9, lane_key: provisioned.lane_key, intent_id: provisioned.intent_id, claim_id: 'claim-provisioned', lease_owner: 'dashboard-autopilot', status: 'active', acquired_at: timestamp, expires_at: timestamp },
    ],
    provisioning: [{
      id: 'provision-standalone', project_id: 9, intent_id: provisioned.intent_id, lease_id: 'lease-provisioned', lane_key: provisioned.lane_key,
      issue_number: 102, role: 'qa', status: 'provisioned', worker_id: 'worker-qa', worker_session_id: 'session-qa', tab_id: 42,
      observed_chatgpt_url: 'https://chatgpt.com/c/qa', bound_binding_id: 'binding-qa', bound_binding_version: 1,
      created_at: timestamp, updated_at: timestamp,
    }],
    commands: [], results: [], merge_cycles: [],
  }

  const rows = buildAutopilotEvidenceRows(status)
  assert.equal(rows.length, 2)
  assert.equal(rows[0].lease.claim_id, 'claim-claimed')
  assert.equal(rows[0].provisioning, undefined)
  assert.equal(rows[0].command, undefined)
  assert.equal(rows[1].lease.lease_id, 'lease-provisioned')
  assert.equal(rows[1].provisioning.worker_session_id, 'session-qa')
  assert.equal(rows[1].command, undefined)
})
