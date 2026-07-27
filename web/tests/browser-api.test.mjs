import test from 'node:test'
import assert from 'node:assert/strict'
import { BrowserApiClient } from '../dist/assets/browser-api.js'
import { BackendResponseError } from '../dist/assets/api.js'

const json = (body, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })
const target = { kind: 'chatgpt_conversation', origin: 'https://chatgpt.com', path: '/c/one' }
const binding = { binding_id: 'binding-1', binding_version: 2, project_id: 1, lane_key: 'lane-1', worker_id: 'worker-1', target, enabled: true, readiness: 'ready', worker_session_id: 'session-1', presence_token: 'secret-token', last_seen: '2026-07-27T12:00:00Z', updated_at: '2026-07-27T12:00:00Z' }
const command = { id: 'command-1', project_id: 1, issue_number: 20, plan_id: 3, plan_hash: 'plan-hash', context_hash: 'context-hash', prompt_hash: 'prompt-hash', action: 'dispatch', target_role: 'implementor', lane_key: 'lane-1', expected_head: 'abc', binding_id: 'binding-1', binding_version: 2, worker_id: 'worker-1', worker_session_id: 'session-1', target_kind: 'chatgpt_conversation', target_ref: 'https://chatgpt.com/c/one', status: 'pending', created_at: '2026-07-27T12:00:00Z', expires_at: '2026-07-27T12:05:00Z' }

test('browser API parses live target projections and binding absence', async () => {
  const client = new BrowserApiClient(async (input) => {
    if (String(input) === '/api/browser/workers') return json({ workers: [{ worker_id: 'worker-1', capabilities: ['exact_prompt_send'], state: 'live', target }] })
    return json({ lane_key: 'lane-1', binding: null })
  })
  assert.equal((await client.workers())[0].target.path, '/c/one')
  assert.deepEqual(await client.browserBinding(1, 20), { lane_key: 'lane-1', binding: null })
})

test('binding and confirmation requests echo CAS identities without prompt or target replacement authority', async () => {
  const calls = []
  const client = new BrowserApiClient(async (input, init = {}) => {
    calls.push({ path: String(input), method: init.method, body: init.body ? JSON.parse(init.body) : undefined })
    if (init.method === 'PUT') return json(binding)
    if (init.method === 'POST') return json(command, 201)
    throw new Error('unexpected request')
  })
  await client.bind(1, 20, { expected_lane_key: 'lane-1', expected_binding_version: 1, worker_id: 'worker-1', target })
  await client.confirm(1, 20, { plan_id: 3, idempotency_key: 'intent-1', expected_plan_hash: 'plan-hash', expected_context_hash: 'context-hash', expected_head: 'abc', expected_lane_key: 'lane-1', expected_binding_id: 'binding-1', expected_binding_version: 2, expected_presence_token: 'secret-token' })
  assert.deepEqual(Object.keys(calls[1].body).sort(), ['expected_binding_id','expected_binding_version','expected_context_hash','expected_head','expected_lane_key','expected_plan_hash','expected_presence_token','idempotency_key','plan_id'].sort())
  assert.equal('prompt' in calls[1].body, false)
  assert.equal('target' in calls[1].body, false)
})

test('malformed browser projection fails visibly', async () => {
  const client = new BrowserApiClient(async () => json({ workers: [{ worker_id: 'worker-1', capabilities: [], state: 'live', target: { ...target, path: 42 } }] }))
  await assert.rejects(client.workers(), (error) => error instanceof BackendResponseError && /target\.path/.test(error.message))
})
