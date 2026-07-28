import assert from 'node:assert/strict'
import test from 'node:test'
import { WorkerLoopApiClient } from '../dist/assets/workerloop-api.js'

function response(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })
}

const profile = {
  project_id: 1,
  resource_version: 'cddm-dashboard-resources/v1.0',
  methodology_version: 'cddm-minimal/v2.0',
  result_protocol: 'cddm-worker-result/v1',
  delivery_mode: 'reviewed',
  qa_session_mode: 'manual_fresh_binding',
  chat_creation_mode: 'automatic',
  chatgpt_project_url: 'https://chatgpt.com/g/g-p-repository/project',
  auto_merge: false,
  updated_at: '2026-07-28T00:00:00Z',
}

test('execution profile parser preserves durable chat creation mode and ChatGPT project URL', async () => {
  const client = new WorkerLoopApiClient(async () => response(profile))
  const value = await client.profile(1)
  assert.equal(value.chat_creation_mode, 'automatic')
  assert.equal(value.chatgpt_project_url, 'https://chatgpt.com/g/g-p-repository/project')
})

test('execution profile parser rejects a missing ChatGPT project scope field', async () => {
  const { chatgpt_project_url: _, ...incomplete } = profile
  const client = new WorkerLoopApiClient(async () => response(incomplete))
  await assert.rejects(() => client.profile(1), /Malformed backend response/)
})

test('worker-loop client keeps delivery and execution status separate', async () => {
  const client = new WorkerLoopApiClient(async () => response({
    project_id: 1,
    issue_number: 140,
    profile,
    active_workflow_command: {
      command_id: 'cmd-1', project_id: 1, issue_number: 140, role: 'qa', action: 'dispatch',
      resource_version: profile.resource_version, context_hash: 'context', expected_head: 'a'.repeat(40),
      status: 'awaiting_result', created_at: '2026-07-28T00:00:00Z',
    },
    delivery: {
      command_id: 'delivery-1', status: 'delivered', binding_id: 'binding-1', binding_version: 1,
      worker_id: 'worker-1', target_role: 'qa', lane_key: 'nordcoder/misak-website#140:qa',
    },
    delivery_status: 'delivered',
    execution_status: 'awaiting_result',
    validation_status: 'not_observed',
    role_bindings: [{ role: 'qa', lane_key: 'nordcoder/misak-website#140:qa' }],
    next_action: 'await_github_worker_result',
  }))
  const value = await client.execution(1, 140)
  assert.equal(value.delivery_status, 'delivered')
  assert.equal(value.execution_status, 'awaiting_result')
  assert.equal(value.next_action, 'await_github_worker_result')
})

test('pilot readiness parser rejects malformed checks', async () => {
  const client = new WorkerLoopApiClient(async () => response({ project_id: 1, issue_number: 140, ready: true, status: 'pilot_ready', resource_digest: 'digest', profile, checks: {}, protocol_warnings: [] }))
  await assert.rejects(() => client.readiness(1, 140), /Malformed backend response/)
})
