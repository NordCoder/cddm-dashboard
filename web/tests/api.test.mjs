import test from 'node:test'
import assert from 'node:assert/strict'
import { ApiClient, ApiError, BackendResponseError } from '../dist/assets/api.js'
import { project, projectState, workspaceState } from './fixtures.mjs'

const json = (body, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })

test('typed API accepts valid project and workspace responses', async () => {
  const calls = []
  const client = new ApiClient(async (input) => {
    calls.push(String(input))
    if (String(input) === '/api/projects') return json({ projects: [project()] })
    if (String(input) === '/api/workspace/state') return json(workspaceState())
    throw new Error(`unexpected ${input}`)
  })

  const projects = await client.projects()
  const workspace = await client.workspaceState()

  assert.equal(projects[0].repository, 'alpha')
  assert.equal(workspace.projects[0].work_units[0].identity.issue_number, 14)
  assert.deepEqual(calls, ['/api/projects', '/api/workspace/state'])
})

test('typed API turns unexpected response shapes into a controlled error', async () => {
  const client = new ApiClient(async () => json({ projects: [{ ...project(), id: 'wrong-type' }] }))
  await assert.rejects(client.projects(), (error) => {
    assert.ok(error instanceof BackendResponseError)
    assert.match(error.message, /Malformed backend response at \$\.projects\[0\]\.id/)
    return true
  })
})

test('typed API rejects malformed JSON without silent corruption', async () => {
  const client = new ApiClient(async () => new Response('{not-json', { status: 200 }))
  await assert.rejects(client.projects(), (error) => {
    assert.ok(error instanceof BackendResponseError)
    assert.match(error.message, /malformed JSON/)
    return true
  })
})

test('typed API surfaces backend errors and network unavailability', async () => {
  const failing = new ApiClient(async () => json({ error: 'repository sync unavailable' }, 503))
  await assert.rejects(failing.projects(), (error) => {
    assert.ok(error instanceof ApiError)
    assert.equal(error.status, 503)
    assert.equal(error.message, 'repository sync unavailable')
    return true
  })

  const offline = new ApiClient(async () => { throw new Error('connection refused') })
  await assert.rejects(offline.workspaceState(), (error) => {
    assert.ok(error instanceof ApiError)
    assert.equal(error.status, 0)
    assert.match(error.message, /connection refused/)
    return true
  })
})

test('project-scoped API requests never reuse another project identity', async () => {
  const calls = []
  const client = new ApiClient(async (input) => {
    const path = String(input)
    calls.push(path)
    if (path === '/api/projects/1/state') return json(projectState(1, 'alpha'))
    if (path === '/api/projects/2/state') return json(projectState(2, 'beta', []))
    throw new Error(`unexpected ${path}`)
  })

  const alpha = await client.projectState(1)
  const beta = await client.projectState(2)

  assert.equal(alpha.project.repository, 'alpha')
  assert.equal(beta.project.repository, 'beta')
  assert.equal(beta.work_units.length, 0)
  assert.deepEqual(calls, ['/api/projects/1/state', '/api/projects/2/state'])
})
