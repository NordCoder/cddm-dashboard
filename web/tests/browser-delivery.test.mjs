import test from 'node:test'
import assert from 'node:assert/strict'
import { buildConfirmationInput, deliveryEligibility } from '../dist/assets/browser-delivery.js'
import { contextHash, generation, head, workUnit } from './fixtures.mjs'

function readyPlan() {
  const result = generation('approved', { plan_id: 7 })
  result.plan.action = 'dispatch'
  result.plan.lane_key = 'project:1:issue:14:implementor'
  result.plan.expected_head = head
  result.context.current_head = head
  result.context.route.action = 'dispatch'
  result.context.route.lane_key = result.plan.lane_key
  result.policy_decision.status = 'approved'
  result.policy_decision.plan_hash = 'plan-hash'
  return result
}
function readyWorkUnit() {
  const unit = workUnit()
  unit.route.action = 'dispatch'
  unit.route.lane_key = readyPlan().plan.lane_key
  unit.current_head = head
  unit.attention.kind = 'normal'
  return unit
}
function readyBinding() {
  return { binding_id: 'binding-1', binding_version: 4, project_id: 1, lane_key: readyPlan().plan.lane_key, worker_id: 'worker', target: { kind: 'chatgpt_conversation', origin: 'https://chatgpt.com', path: '/c/one' }, enabled: true, readiness: 'ready', worker_session_id: 'session', presence_token: 'presence', last_seen: '2026-07-27T12:00:00Z', updated_at: '2026-07-27T12:00:00Z' }
}

test('delivery eligibility requires current dispatch plan and ready binding', () => {
  const result = readyPlan()
  const unit = readyWorkUnit()
  const binding = readyBinding()
  assert.equal(deliveryEligibility(unit, result, binding).ready, true)
  assert.equal(deliveryEligibility(unit, result, { ...binding, readiness: 'stale' }).ready, false)
  assert.equal(deliveryEligibility({ ...unit, route: { ...unit.route, action: 'qa' } }, result, binding).ready, false)
  assert.equal(deliveryEligibility(unit, { ...result, status: 'rejected' }, binding).ready, false)
})

test('confirmation payload contains only backend CAS identities', () => {
  const input = buildConfirmationInput(readyPlan(), readyBinding(), 'intent-1')
  assert.deepEqual(input, {
    plan_id: 7,
    idempotency_key: 'intent-1',
    expected_plan_hash: 'plan-hash',
    expected_context_hash: contextHash,
    expected_head: head,
    expected_lane_key: 'project:1:issue:14:implementor',
    expected_binding_id: 'binding-1',
    expected_binding_version: 4,
    expected_presence_token: 'presence',
  })
  assert.equal('prompt' in input, false)
  assert.equal('target' in input, false)
})
