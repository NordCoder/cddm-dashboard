import test from 'node:test'
import assert from 'node:assert/strict'
import { BrowserApiClient } from '../dist/assets/browser-api.js'
import { ApiError } from '../dist/assets/api.js'

const input = {
  plan_id: 1, idempotency_key: 'intent', expected_plan_hash: 'plan', expected_context_hash: 'context',
  expected_head: 'head', expected_lane_key: 'lane', expected_binding_id: 'binding', expected_binding_version: 1,
  expected_presence_token: 'presence',
}

function command() {
  return {
    id: 'command', project_id: 1, issue_number: 2, plan_id: 1, plan_hash: 'plan', context_hash: 'context',
    prompt_hash: 'hash', action: 'dispatch', target_role: 'worker', lane_key: 'lane', expected_head: 'head',
    binding_id: 'binding', binding_version: 1, worker_id: 'worker', worker_session_id: 'session',
    target_kind: 'chatgpt_conversation', target_ref: 'https://chatgpt.com/c/one', status: 'pending',
    created_at: '2026-01-01T00:00:00Z', expires_at: '2026-01-01T00:05:00Z',
  }
}

test('confirmation 5xx is exposed as transport-ambiguous so the same idempotency key is retained', async () => {
  const client = new BrowserApiClient(async () => new Response(JSON.stringify({ error: 'proxy unavailable' }), { status: 503 }))
  await assert.rejects(() => client.confirm(1, 2, input), (error) => error instanceof ApiError && error.status === 0)
})

test('confirmation malformed success response is transport-ambiguous', async () => {
  const client = new BrowserApiClient(async () => new Response('{', { status: 201 }))
  await assert.rejects(() => client.confirm(1, 2, input), (error) => error instanceof ApiError && error.status === 0)
})

test('confirmation body-read failure is transport-ambiguous', async () => {
  const client = new BrowserApiClient(async () => ({ ok: true, status: 201, async text() { throw new Error('stream reset') } }))
  await assert.rejects(() => client.confirm(1, 2, input), (error) => error instanceof ApiError && error.status === 0)
})

test('definitive conflict remains a conflict and forces a fresh review', async () => {
  const client = new BrowserApiClient(async () => new Response(JSON.stringify({ error: 'conflict' }), { status: 409 }))
  await assert.rejects(() => client.confirm(1, 2, input), (error) => error instanceof ApiError && error.status === 409)
})

test('valid confirmation response still parses normally', async () => {
  const client = new BrowserApiClient(async () => new Response(JSON.stringify(command()), { status: 201 }))
  assert.equal((await client.confirm(1, 2, input)).id, 'command')
})

test('malformed definitive conflict remains a conflict rather than becoming a duplicate-intent retry', async () => {
  const client = new BrowserApiClient(async () => new Response('{', { status: 409 }))
  await assert.rejects(() => client.confirm(1, 2, input), (error) => error instanceof ApiError && error.status === 409)
})
