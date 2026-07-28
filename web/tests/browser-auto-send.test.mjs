import test from 'node:test'
import assert from 'node:assert/strict'
import {
  automaticDeliveryIdentity,
  autoSendRetryDue,
  matchingDeliveryExists,
} from '../dist/assets/browser-auto-send.js'
import { contextHash, generation, head } from './fixtures.mjs'

function result() {
  const value = generation('approved', { plan_id: 7 })
  value.plan.action = 'dispatch'
  value.plan.lane_key = 'project:1:issue:14:implementor'
  value.plan.expected_head = head
  value.context.context_hash = contextHash
  value.policy_decision.status = 'approved'
  value.policy_decision.plan_hash = 'plan-hash'
  return value
}

function binding(overrides = {}) {
  return {
    binding_id: 'binding-1',
    binding_version: 4,
    project_id: 1,
    lane_key: 'project:1:issue:14:implementor',
    worker_id: 'worker-1',
    target: { kind: 'chatgpt_conversation', origin: 'https://chatgpt.com', path: '/c/one' },
    enabled: true,
    readiness: 'ready',
    presence_token: 'presence-1',
    updated_at: '2026-07-28T08:00:00Z',
    ...overrides,
  }
}

test('automatic delivery identity is exact-plan and exact-binding scoped', () => {
  const first = automaticDeliveryIdentity(1, 14, result(), binding())
  assert.equal(first, automaticDeliveryIdentity(1, 14, result(), binding()))
  assert.notEqual(first, automaticDeliveryIdentity(1, 14, result(), binding({ binding_version: 5 })))
  assert.notEqual(first, automaticDeliveryIdentity(1, 15, result(), binding()))
})

test('existing exact command suppresses automatic duplicate creation', () => {
  const current = result()
  const currentBinding = binding()
  const command = {
    plan_id: 7,
    plan_hash: 'plan-hash',
    expected_head: head,
    lane_key: current.plan.lane_key,
    binding_id: 'binding-1',
    binding_version: 4,
  }
  assert.equal(matchingDeliveryExists([command], current, currentBinding), true)
  assert.equal(matchingDeliveryExists([{ ...command, binding_version: 5 }], current, currentBinding), false)
  assert.equal(matchingDeliveryExists([{ ...command, plan_id: 8 }], current, currentBinding), false)
})

test('ambiguous automatic confirmation retries are throttled', () => {
  const record = { identity: 'identity', idempotencyKey: 'intent', status: 'pending', lastAttemptAt: 10_000 }
  assert.equal(autoSendRetryDue(record, 24_999), false)
  assert.equal(autoSendRetryDue(record, 25_000), true)
  assert.equal(autoSendRetryDue({ ...record, status: 'submitted' }, 100_000), false)
})
