import test from 'node:test'
import assert from 'node:assert/strict'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import {
  attention,
  attentionItem,
  clone,
  generation,
  head,
  project,
  projectState,
  result,
  workUnit,
  workspaceState,
} from './fixtures.mjs'

globalThis.React = React
const ui = await import('../dist/assets/ui.js')
const router = await import('../dist/assets/router.js')

const render = (node) => renderToStaticMarkup(node)
const noop = () => {}
const launcher = React.createElement('div', null, 'launcher')

function workspaceBundle(projects, states) {
  return {
    projects,
    state: workspaceState(states),
    planner: { enabled: true, status: 'healthy', runtime: 'opencode', agent: 'prompt-planner' },
  }
}

test('Workspace renders repositories, grouped attention navigation and empty/unavailable states', () => {
  const blocked = workUnit({ attention: attention('blocked', 'Blocked by dependency') })
  const state = projectState(1, 'alpha', [blocked])
  const html = render(ui.WorkspaceContent({
    bundle: workspaceBundle([project()], [state]),
    navigate: noop,
    createPanel: React.createElement('div', null, 'create-panel'),
    onRefresh: noop,
  }))

  assert.match(html, /NordCoder\/alpha/)
  assert.match(html, /Global Attention Queue/)
  assert.match(html, /blocked/)
  assert.match(html, /href="\/projects\/1\/work-units\/14"/)
  assert.match(html, /Planner/)

  const empty = render(ui.WorkspaceContent({
    bundle: workspaceBundle([], []),
    navigate: noop,
    createPanel: React.createElement('form', null, 'Create first Project'),
    onRefresh: noop,
  }))
  assert.match(empty, /No Projects yet/)
  assert.match(empty, /Create first Project/)

  const unavailable = render(ui.ErrorState({ message: 'connection refused' }))
  assert.match(unavailable, /Backend unavailable/)
  assert.match(unavailable, /connection refused/)
})

test('Project prioritizes attention work units and shows Candidate, CI, and worker evidence', () => {
  const normal = workUnit({ identity: { project_id: 1, repository: 'alpha', issue_number: 10 }, attention: attention('normal') })
  const failed = workUnit({
    identity: { project_id: 1, repository: 'alpha', issue_number: 20 },
    attention: attention('ci_failed', 'Exact-Head CI failed'),
    ci: { ...normal.ci, conclusion: 'failure' },
  })
  const html = render(ui.ProjectContent({
    bundle: { project: project(), state: projectState(1, 'alpha', [normal, failed]) },
    navigate: noop,
    onRefresh: noop,
    onSync: noop,
    syncing: false,
    onDelete: noop,
    deleting: false,
  }))

  assert.ok(html.indexOf('#20') < html.indexOf('#10'), 'attention item must render before normal item')
  assert.match(html, /PR #7/)
  assert.match(html, /failure/)
  assert.match(html, /Lead/)
  assert.match(html, /Implementor/)
  assert.match(html, /QA/)
  assert.match(html, /Delete Project/)
})

test('Work Unit exposes healthy candidate and operational evidence', () => {
  const html = render(ui.WorkUnitContent({ workUnit: workUnit(), navigate: noop, onRefresh: noop, launcher }))
  assert.match(html, /Candidate PR/)
  assert.match(html, /Current exact Head/)
  assert.match(html, /Exact-Head CI/)
  assert.match(html, /Next route/)
  assert.match(html, /Route guards/)
  assert.match(html, /Operational evidence/)
  assert.match(html, /Open source comment/)
})

test('Work Unit explicitly renders stale handoff, stale QA, ambiguity, blocker, invalidation and owner-required states', () => {
  const special = workUnit({
    candidate: { current: undefined, alternatives: [workUnit().candidate.current, workUnit().candidate.current], ambiguous: true },
    attention: attention('qa_invalidated', 'Candidate Head changed after QA'),
    active_blocker: result('implementor', { status: 'blocked' }),
    latest_results: {
      lead: result('lead'),
      implementor: result('implementor', { stale: true }),
      qa: result('qa', { stale: true, verdict: 'approved' }),
    },
    warnings: [{ code: 'protocol_warning', message: 'Malformed historical terminal envelope' }],
  })
  const html = render(ui.WorkUnitContent({ workUnit: special, navigate: noop, onRefresh: noop, launcher }))
  assert.match(html, /Ambiguous Candidate/)
  assert.match(html, /Changed-Head \/ QA invalidation/)
  assert.match(html, /Active blocker/)
  assert.match(html, /Stale Implementor handoff result/)
  assert.match(html, /Stale QA verdict result/)
  assert.match(html, /Malformed historical terminal envelope/)

  const owner = clone(workUnit())
  owner.attention = attention('owner_required', 'Owner must decide scope')
  owner.route = { ...owner.route, action: 'owner_attention', target_role: undefined, lane_key: undefined }
  const ownerHtml = render(ui.WorkUnitContent({
    workUnit: owner,
    navigate: noop,
    onRefresh: noop,
    launcher: ui.PlanLauncher({ ownerRequired: true, mode: 'opencode', onMode: noop, onGenerate: noop, generating: false }),
  }))
  assert.match(ownerHtml, /Owner decision required/)
  assert.match(ownerHtml, /not a ready worker dispatch/i)
  assert.doesNotMatch(ownerHtml, />Generate plan</)
})

test('Prompt planning renders approved/fallback as copyable and invalid statuses as review-only', () => {
  for (const status of ['approved', 'fallback']) {
    const item = generation(status)
    const html = render(ui.PlanReview({
      result: item,
      edit: { generated: item.plan.prompt, value: item.plan.prompt },
      onEdit: noop,
      onReset: noop,
      onCopy: noop,
    }))
    assert.match(html, new RegExp(status))
    assert.match(html, /Copy prompt/)
    assert.doesNotMatch(html, /disabled=""[^>]*>Copy prompt/)
    assert.match(html, /Exact Head before copy/)
    assert.match(html, /Context hash/)
    assert.match(html, /Prohibited actions/)
  }

  for (const status of ['stale', 'rejected', 'planner_error']) {
    const item = generation(status)
    const generated = item.plan?.prompt ?? ''
    const html = render(ui.PlanReview({
      result: item,
      edit: { generated, value: generated },
      onEdit: noop,
      onReset: noop,
      onCopy: noop,
    }))
    assert.match(html, /Not dispatch-ready/)
    assert.match(html, /Copy unavailable/)
    assert.match(html, /disabled=""/)
  }
})

test('generation in-flight disables mode and duplicate generation controls', () => {
  const html = render(ui.PlanLauncher({
    ownerRequired: false,
    mode: 'opencode',
    onMode: noop,
    onGenerate: noop,
    generating: true,
  }))
  assert.match(html, /Generating plan…/)
  assert.ok((html.match(/disabled=""/g) ?? []).length >= 3)
  assert.match(html, /aria-busy="true"/)
})

test('owner-required context never presents an approved historical plan as copy-ready', () => {
  const item = generation('approved')
  const html = render(ui.PlanReview({
    result: item,
    edit: { generated: item.plan.prompt, value: item.plan.prompt },
    onEdit: noop,
    onReset: noop,
    onCopy: noop,
    ownerRequired: true,
  }))
  assert.match(html, /Not dispatch-ready/)
  assert.match(html, /Copy unavailable/)
})

test('local prompt editing and reset never mutate the backend generation', () => {
  const backend = generation('approved')
  const originalPrompt = backend.plan.prompt
  let state = { generated: originalPrompt, value: originalPrompt }

  state = ui.reducePromptEdit(state, { type: 'edit', value: `${originalPrompt}\nLocal note` })
  assert.equal(ui.isPromptEdited(state), true)
  assert.equal(backend.plan.prompt, originalPrompt)

  const editedHtml = render(ui.PlanReview({ result: backend, edit: state, onEdit: noop, onReset: noop, onCopy: noop }))
  assert.match(editedHtml, /Edited locally/)
  assert.match(editedHtml, /Reset to generated prompt/)

  state = ui.reducePromptEdit(state, { type: 'reset' })
  assert.equal(state.value, originalPrompt)
  assert.equal(ui.isPromptEdited(state), false)
  assert.equal(backend.plan.prompt, originalPrompt)
})

test('multi-repository fixtures retain independent routes and state identities', () => {
  const alphaUnit = workUnit({ identity: { project_id: 1, repository: 'alpha', issue_number: 11 } })
  const betaUnit = workUnit({ identity: { project_id: 2, repository: 'beta', issue_number: 22 } })
  const alpha = projectState(1, 'alpha', [alphaUnit])
  const beta = projectState(2, 'beta', [betaUnit])
  alpha.attention = [attentionItem(alphaUnit, 'blocked')]
  beta.attention = [attentionItem(betaUnit, 'owner_required')]

  const html = render(ui.WorkspaceContent({
    bundle: workspaceBundle([project(1, 'alpha'), project(2, 'beta')], [alpha, beta]),
    navigate: noop,
    createPanel: null,
    onRefresh: noop,
  }))

  assert.match(html, /NordCoder\/alpha/)
  assert.match(html, /NordCoder\/beta/)
  assert.match(html, /href="\/projects\/1\/work-units\/11"/)
  assert.match(html, /href="\/projects\/2\/work-units\/22"/)
  assert.notEqual(router.paths.project(1), router.paths.project(2))
})

test('deep-link parser preserves project/work-unit/plan context', () => {
  assert.deepEqual(router.parseRoute('/projects/4/work-units/81/plans/9'), {
    kind: 'plans', projectID: 4, issueNumber: 81, planID: 9,
  })
  assert.deepEqual(router.parseRoute('/projects/4/work-units/81'), {
    kind: 'work-unit', projectID: 4, issueNumber: 81,
  })
})
