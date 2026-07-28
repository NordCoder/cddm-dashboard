import assert from 'node:assert/strict'
import test from 'node:test'
import {
  bootstrapPrompt,
  bootstrapRequestID,
  chatCreationWorker,
  createWorkerChat,
  projectChatCandidates,
  routedCreationRole,
} from '../dist/assets/chat-bootstrap.js'

function workUnit(role = 'implementor', action = 'dispatch', issueNumber = 140) {
  return {
    identity: {
      project_id: 1, owner: 'NordCoder', repository: 'misak-website', issue_number: issueNumber,
    },
    route: { action, target_role: role, reason_code: 'test_route' },
  }
}

test('role bootstrap prompts attach only the expected reusable Library resources', () => {
  assert.match(bootstrapPrompt('lead', workUnit('lead')), /^@01-workflow\.md\n@cddm-minimal-issue-sizing-standard\.md/)
  assert.match(bootstrapPrompt('implementor', workUnit()), /^@02-implementor-trigger\.md\n@gpt-gh-connector-guidelines\.md/)
  assert.match(bootstrapPrompt('qa', workUnit('qa')), /^@03-qa-trigger\.md\n@gpt-gh-connector-guidelines\.md/)
  assert.match(bootstrapPrompt('qa', workUnit('qa')), /not a Dashboard Workflow Command/)
})

test('automatic routing creates only missing Implementor or fresh QA lanes', () => {
  const bindings = [
    { role: 'lead', lane_key: 'repo#140:lead' },
    { role: 'implementor', lane_key: 'repo#140:implementor' },
    { role: 'qa', lane_key: 'repo#140:qa' },
  ]
  assert.equal(routedCreationRole(workUnit('implementor'), bindings), 'implementor')
  assert.equal(routedCreationRole(workUnit('qa'), bindings), 'qa')
  assert.equal(routedCreationRole(workUnit('lead'), bindings), undefined)
  assert.equal(routedCreationRole(workUnit('qa', 'hold'), bindings), undefined)
  bindings[2].binding = { enabled: true, readiness: 'ready' }
  assert.equal(routedCreationRole(workUnit('qa'), bindings), undefined)
})

test('Project supervisor considers every routed Implementor and QA Work Unit in stable order', () => {
  const candidates = projectChatCandidates({
    work_units: [
      workUnit('qa', 'dispatch', 151),
      workUnit('lead', 'dispatch', 149),
      workUnit('implementor', 'dispatch', 150),
      workUnit('qa', 'hold', 148),
    ],
  })
  assert.deepEqual(candidates.map((item) => [item.identity.issue_number, item.route.target_role]), [[150, 'implementor'], [151, 'qa']])
})

test('chat creation capability is discovered independently from an active target', () => {
  const worker = chatCreationWorker([
    { worker_id: 'old', capabilities: ['exact_prompt_send'], state: 'live' },
    { worker_id: 'creator', capabilities: ['chatgpt_conversation_create'], state: 'live' },
  ])
  assert.equal(worker.worker_id, 'creator')
})

test('bootstrap request identity rotates with binding generation, lane or ChatGPT project scope', () => {
  const projectURL = 'https://chatgpt.com/g/g-p-repository/project'
  const first = bootstrapRequestID(1, 140, 'qa', 'repo#140:qa', 1, projectURL)
  const duplicate = bootstrapRequestID(1, 140, 'qa', 'repo#140:qa', 1, projectURL)
  const nextCycle = bootstrapRequestID(1, 140, 'qa', 'repo#140:qa', 2, projectURL)
  const otherLane = bootstrapRequestID(1, 141, 'qa', 'repo#141:qa', 1, projectURL)
  const otherProject = bootstrapRequestID(1, 140, 'qa', 'repo#140:qa', 1, 'https://chatgpt.com/g/g-p-other/project')
  assert.equal(duplicate, first)
  assert.match(first, /^chat-p1-i140-qa-v1-[0-9a-f]{8}$/)
  assert.notEqual(nextCycle, first)
  assert.notEqual(otherLane, first)
  assert.notEqual(otherProject, first)
})

test('Dashboard sends the configured ChatGPT project in a bounded external bootstrap request', async () => {
  let sent
  const chromeApi = {
    runtime: {
      sendMessage(extensionID, message, callback) {
        sent = { extensionID, message }
        callback({ ok: true, binding: { binding_id: 'binding-1' }, target: { kind: 'chatgpt_conversation', origin: 'https://chatgpt.com', path: '/c/fresh' } })
      },
    },
  }
  const result = await createWorkerChat({
    projectID: 1,
    issueNumber: 140,
    role: 'implementor',
    roleBinding: { role: 'implementor', lane_key: 'nordcoder/misak-website#140:implementor' },
    workUnit: workUnit(),
    chatGPTProjectURL: 'https://chatgpt.com/g/g-p-repository/project',
    chromeApi,
  })
  assert.equal(result.ok, true)
  assert.equal(sent.extensionID, 'biakfbpkfdpniphmoafgldedkbnjfibp')
  assert.equal(sent.message.expected_lane_key, 'nordcoder/misak-website#140:implementor')
  assert.equal(sent.message.chatgpt_project_url, 'https://chatgpt.com/g/g-p-repository/project')
  assert.match(sent.message.request_id, /^chat-p1-i140-implementor-v0-[0-9a-f]{8}$/)
  assert.match(sent.message.bootstrap_prompt, /@02-implementor-trigger\.md/)
  assert.equal(sent.message.command_id, undefined)
})
