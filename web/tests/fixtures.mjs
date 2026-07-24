export const now = '2026-07-24T18:00:00Z'
export const head = '0123456789abcdef0123456789abcdef01234567'
export const otherHead = 'fedcba9876543210fedcba9876543210fedcba98'
export const contextHash = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'

export function project(id = 1, repository = 'alpha') {
  return {
    id,
    owner: 'NordCoder',
    repository,
    workflow_mode: 'pull_request',
    polling_enabled: true,
    poll_interval_seconds: 300,
    sync_status: 'healthy',
    last_sync_started_at: now,
    last_sync_completed_at: now,
    created_at: now,
    updated_at: now,
  }
}

export function projectIdentity(id = 1, repository = 'alpha') {
  return { id, owner: 'NordCoder', repository, workflow_mode: 'pull_request' }
}

export function ci(overrides = {}) {
  return {
    head_sha: head,
    status: 'completed',
    conclusion: 'success',
    source: 'check_runs',
    details_url: 'https://github.com/NordCoder/alpha/actions/runs/1',
    updated_at: now,
    ...overrides,
  }
}

export function candidate(overrides = {}) {
  return {
    github_id: 9001,
    number: 7,
    title: 'Stage candidate',
    draft: true,
    mergeable_state: 'clean',
    base_ref: 'main',
    head_ref: 'stage/work',
    head_sha: head,
    url: 'https://github.com/NordCoder/alpha/pull/7',
    ci: ci(),
    ...overrides,
  }
}

export function result(role, overrides = {}) {
  return {
    project_id: 1,
    issue_number: 14,
    comment_id: role === 'lead' ? 101 : role === 'implementor' ? 102 : 103,
    role,
    status: 'completed',
    head,
    level: 'authoritative',
    stale: false,
    effective: true,
    created_at: now,
    warnings: [],
    ...overrides,
  }
}

export function attention(kind = 'normal', explanation = 'No action required') {
  return { kind, code: `${kind}_fixture`, explanation }
}

export function route(overrides = {}) {
  return {
    action: 'dispatch_worker',
    target_role: 'implementor',
    lane_key: 'project:1:issue:14:implementor',
    reason_code: 'implementation_required',
    reason: 'Implementation is the next backend-authorized action.',
    expected_head: head,
    guards: ['Use exact current Head'],
    warnings: [],
    ...overrides,
  }
}

export function workUnit(overrides = {}) {
  const issueNumber = overrides.identity?.issue_number ?? 14
  const repository = overrides.identity?.repository ?? 'alpha'
  const projectID = overrides.identity?.project_id ?? 1
  const identity = {
    project_id: projectID,
    owner: 'NordCoder',
    repository,
    issue_github_id: 1400 + issueNumber,
    issue_number: issueNumber,
    title: `Issue ${issueNumber} dashboard work`,
    url: `https://github.com/NordCoder/${repository}/issues/${issueNumber}`,
    ...(overrides.identity ?? {}),
  }
  return {
    identity,
    lifecycle: 'implementation',
    candidate: { current: candidate(), alternatives: [], ambiguous: false },
    current_head: head,
    ci: ci(),
    parsed_comments: [
      {
        project_id: projectID,
        issue_number: issueNumber,
        comment_id: 777,
        author: 'NordCoder',
        url: `${identity.url}#issuecomment-777`,
        created_at: now,
        updated_at: now,
        level: 'authoritative',
        heading: 'Implementor Handoff',
        markdown: 'Candidate evidence is available for human review.',
        meaningful: true,
        transition_safe: true,
        warnings: [],
      },
    ],
    latest_results: {
      lead: result('lead', { project_id: projectID, issue_number: issueNumber, decision: 'dispatch' }),
      implementor: result('implementor', { project_id: projectID, issue_number: issueNumber }),
      qa: result('qa', { project_id: projectID, issue_number: issueNumber, verdict: 'approved' }),
    },
    warnings: [],
    last_meaningful_activity: now,
    attention: attention('normal'),
    route: route({ lane_key: `project:${projectID}:issue:${issueNumber}:implementor` }),
    ...overrides,
    identity,
  }
}

export function attentionItem(unit, kind = unit.attention.kind) {
  return {
    project: projectIdentity(unit.identity.project_id, unit.identity.repository),
    work_unit: unit.identity,
    attention: attention(kind, `${kind} needs review`),
    route: unit.route,
  }
}

export function projectState(id = 1, repository = 'alpha', units = [workUnit()]) {
  return {
    project: projectIdentity(id, repository),
    work_units: units,
    attention: units.filter((unit) => !['normal', 'waiting', 'terminal'].includes(unit.attention.kind)).map((unit) => attentionItem(unit)),
  }
}

export function workspaceState(states = [projectState()]) {
  return {
    generated_at: now,
    projects: states,
    attention: states.flatMap((state) => state.attention),
  }
}

export function promptContext(overrides = {}) {
  return {
    v: 1,
    repository: { project_id: 1, owner: 'NordCoder', repository: 'alpha', workflow_mode: 'pull_request' },
    issue: {
      github_id: 1414,
      number: 14,
      title: 'Issue 14 dashboard work',
      body: 'Stage 5 contract',
      url: 'https://github.com/NordCoder/alpha/issues/14',
      lifecycle: 'implementation',
      attention: attention('normal'),
    },
    candidate: { current: candidate(), alternatives: [], ambiguous: false },
    current_head: head,
    ci: ci(),
    latest_worker_results: {},
    route: route(),
    expected_event: 'worker_result',
    warnings: [],
    evidence: [],
    context_hash: contextHash,
    ...overrides,
  }
}

export function generation(status = 'approved', overrides = {}) {
  const hasPlan = !['planner_error'].includes(status)
  const sourceKind = status === 'fallback' ? 'template_fallback' : 'opencode'
  const plan = hasPlan
    ? {
        v: 1,
        action: 'dispatch_worker',
        target_role: 'implementor',
        lane_key: 'project:1:issue:14:implementor',
        summary: 'Continue the implementation safely.',
        reason: 'Backend state authorizes the Implementor lane.',
        risk: 'medium',
        requires_owner: false,
        expected_head: head,
        expected_event: 'worker_result',
        guards: ['Use exact Head', 'Keep PR draft'],
        prohibited_actions: ['Do not merge'],
        prompt: 'Generated policy-approved prompt body',
        confidence: 0.93,
        source: {
          kind: sourceKind,
          runtime: status === 'fallback' ? 'template' : 'opencode',
          mode: status === 'fallback' ? 'fallback' : 'opencode',
          context_hash: contextHash,
        },
      }
    : undefined
  return {
    status,
    context: promptContext(),
    ...(plan ? { plan } : {}),
    policy_decision: {
      status: status === 'rejected' ? 'rejected' : status === 'planner_error' ? 'error' : 'approved',
      violations: status === 'rejected' ? [{ code: 'policy_mismatch', field: 'action', message: 'Action does not match canonical route' }] : [],
      context_hash: contextHash,
      plan_hash: plan ? 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' : undefined,
      decided_at: now,
    },
    plan_id: 42,
    created_at: now,
    ...overrides,
  }
}

export function clone(value) {
  return structuredClone(value)
}
