export type HealthResponse = {
  status: string
  database: string
}

export type Project = {
  id: number
  owner: string
  repository: string
  workflow_mode: string
  polling_enabled: boolean
  poll_interval_seconds: number
  sync_status: string
  sync_error?: string
  last_sync_started_at?: string
  last_sync_completed_at?: string
  created_at: string
  updated_at: string
}

export type ProjectIdentity = {
  id: number
  owner: string
  repository: string
  workflow_mode: string
}

export type WorkUnitIdentity = {
  project_id: number
  owner: string
  repository: string
  issue_github_id: number
  issue_number: number
  title: string
  url: string
}

export type Warning = {
  code: string
  message: string
  comment_id?: number
}

export type CISummary = {
  head_sha: string
  status: string
  conclusion: string
  source: string
  details_url?: string
  updated_at: string
}

export type Candidate = {
  github_id: number
  number: number
  title: string
  draft: boolean
  mergeable_state?: string
  base_ref: string
  head_ref: string
  head_sha: string
  url: string
  ci: CISummary
}

export type CandidateState = {
  current?: Candidate
  alternatives: Candidate[]
  ambiguous: boolean
}

export type ResultEvidence = {
  project_id: number
  issue_number: number
  comment_id: number
  role: string
  status: string
  head?: string
  verdict?: string
  decision?: string
  resume_role?: string
  resolves?: unknown
  escalate_to?: string
  level: string
  stale: boolean
  effective: boolean
  created_at: string
  warnings: Warning[]
  extensions?: Record<string, unknown>
}

export type LatestResults = {
  lead?: ResultEvidence
  implementor?: ResultEvidence
  qa?: ResultEvidence
}

export type Attention = {
  kind: string
  code: string
  explanation: string
}

export type Route = {
  action: string
  target_role?: string
  lane_key?: string
  reason_code: string
  reason: string
  expected_head?: string
  guards: string[]
  warnings: Warning[]
}

export type ParsedComment = {
  project_id: number
  issue_number: number
  comment_id: number
  author: string
  url: string
  created_at: string
  updated_at: string
  level: string
  heading?: string
  markdown: string
  meaningful: boolean
  transition_safe: boolean
  warnings: Warning[]
  hard_error?: { code: string; message: string }
}

export type WorkUnitState = {
  identity: WorkUnitIdentity
  lifecycle: string
  candidate: CandidateState
  current_head?: string
  ci: CISummary
  parsed_comments: ParsedComment[]
  latest_results: LatestResults
  active_blocker?: ResultEvidence
  qa_reviewed_head?: string
  qa_approved_head?: string
  warnings: Warning[]
  last_meaningful_activity: string
  attention: Attention
  route: Route
}

export type AttentionItem = {
  project: ProjectIdentity
  work_unit: WorkUnitIdentity
  attention: Attention
  route: Route
}

export type ProjectState = {
  project: ProjectIdentity
  work_units: WorkUnitState[]
  attention: AttentionItem[]
}

export type WorkspaceState = {
  generated_at: string
  projects: ProjectState[]
  attention: AttentionItem[]
}

export type RepositoryIdentity = {
  project_id: number
  owner: string
  repository: string
  workflow_mode: string
}

export type IssueIdentity = {
  github_id: number
  number: number
  title: string
  body: string
  url: string
  lifecycle: string
  attention: Attention
}

export type PromptContext = {
  v: number
  repository: RepositoryIdentity
  issue: IssueIdentity
  current_head: string
  route: Route
  expected_event: string
  warnings: Warning[]
  context_hash: string
}

export type SourceMetadata = {
  kind: string
  runtime: string
  provider?: string
  model?: string
  agent?: string
  mode: string
  context_hash: string
}

export type PromptPlan = {
  v: number
  action: string
  target_role: string
  lane_key: string
  summary: string
  reason: string
  risk: string
  requires_owner: boolean
  expected_head: string
  expected_event: string
  guards: string[]
  prohibited_actions: string[]
  prompt: string
  confidence: number
  source: SourceMetadata
  extensions?: Record<string, unknown>
}

export type Violation = {
  code: string
  field?: string
  message: string
}

export type PolicyDecision = {
  status: string
  violations: Violation[]
  context_hash: string
  plan_hash?: string
  decided_at: string
}

export type GenerationResult = {
  status: string
  context: PromptContext
  plan?: PromptPlan
  policy_decision: PolicyDecision
  plan_id?: number
  created_at: string
}

export type ContextSummary = {
  v: number
  context_hash: string
  repository: RepositoryIdentity
  issue: IssueIdentity
  current_head?: string
  route: Route
  expected_event: string
  evidence_count: number
  warning_count: number
}

export type PlannerHealth = {
  enabled: boolean
  status: string
  runtime: string
  endpoint?: string
  provider?: string
  model?: string
  agent?: string
  error?: string
}

export type ProjectSnapshot = {
  project: Project
}

export class ValidationError extends Error {
  constructor(path: string, expected: string) {
    super(`Malformed backend response at ${path}: expected ${expected}`)
    this.name = 'ValidationError'
  }
}

type RecordValue = Record<string, unknown>

function record(value: unknown, path: string): RecordValue {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new ValidationError(path, 'object')
  }
  return value as RecordValue
}

function stringValue(value: unknown, path: string): string {
  if (typeof value !== 'string') {
    throw new ValidationError(path, 'string')
  }
  return value
}

function numberValue(value: unknown, path: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new ValidationError(path, 'finite number')
  }
  return value
}

function booleanValue(value: unknown, path: string): boolean {
  if (typeof value !== 'boolean') {
    throw new ValidationError(path, 'boolean')
  }
  return value
}

function optionalString(value: unknown, path: string): string | undefined {
  if (value === undefined || value === null || value === '') {
    return undefined
  }
  return stringValue(value, path)
}

function optionalNumber(value: unknown, path: string): number | undefined {
  if (value === undefined || value === null) {
    return undefined
  }
  return numberValue(value, path)
}

function arrayValue<T>(value: unknown, path: string, parser: (item: unknown, path: string) => T): T[] {
  if (value === null || value === undefined) {
    return []
  }
  if (!Array.isArray(value)) {
    throw new ValidationError(path, 'array')
  }
  return value.map((item, index) => parser(item, `${path}[${index}]`))
}

function unknownObject(value: unknown, path: string): Record<string, unknown> | undefined {
  if (value === undefined || value === null) {
    return undefined
  }
  return record(value, path)
}

export function parseHealth(value: unknown): HealthResponse {
  const item = record(value, '$')
  return {
    status: stringValue(item.status, '$.status'),
    database: stringValue(item.database, '$.database'),
  }
}

export function parseProject(value: unknown, path = '$'): Project {
  const item = record(value, path)
  return {
    id: numberValue(item.id, `${path}.id`),
    owner: stringValue(item.owner, `${path}.owner`),
    repository: stringValue(item.repository, `${path}.repository`),
    workflow_mode: stringValue(item.workflow_mode, `${path}.workflow_mode`),
    polling_enabled: booleanValue(item.polling_enabled, `${path}.polling_enabled`),
    poll_interval_seconds: numberValue(item.poll_interval_seconds, `${path}.poll_interval_seconds`),
    sync_status: stringValue(item.sync_status, `${path}.sync_status`),
    sync_error: optionalString(item.sync_error, `${path}.sync_error`),
    last_sync_started_at: optionalString(item.last_sync_started_at, `${path}.last_sync_started_at`),
    last_sync_completed_at: optionalString(item.last_sync_completed_at, `${path}.last_sync_completed_at`),
    created_at: stringValue(item.created_at, `${path}.created_at`),
    updated_at: stringValue(item.updated_at, `${path}.updated_at`),
  }
}

export function parseProjects(value: unknown): Project[] {
  const root = record(value, '$')
  return arrayValue(root.projects, '$.projects', parseProject)
}

function parseProjectIdentity(value: unknown, path: string): ProjectIdentity {
  const item = record(value, path)
  return {
    id: numberValue(item.id, `${path}.id`),
    owner: stringValue(item.owner, `${path}.owner`),
    repository: stringValue(item.repository, `${path}.repository`),
    workflow_mode: stringValue(item.workflow_mode, `${path}.workflow_mode`),
  }
}

function parseWorkUnitIdentity(value: unknown, path: string): WorkUnitIdentity {
  const item = record(value, path)
  return {
    project_id: numberValue(item.project_id, `${path}.project_id`),
    owner: stringValue(item.owner, `${path}.owner`),
    repository: stringValue(item.repository, `${path}.repository`),
    issue_github_id: numberValue(item.issue_github_id, `${path}.issue_github_id`),
    issue_number: numberValue(item.issue_number, `${path}.issue_number`),
    title: stringValue(item.title, `${path}.title`),
    url: stringValue(item.url, `${path}.url`),
  }
}

function parseWarning(value: unknown, path: string): Warning {
  const item = record(value, path)
  return {
    code: stringValue(item.code, `${path}.code`),
    message: stringValue(item.message, `${path}.message`),
    comment_id: optionalNumber(item.comment_id, `${path}.comment_id`),
  }
}

function parseCI(value: unknown, path: string): CISummary {
  const item = record(value, path)
  return {
    head_sha: stringValue(item.head_sha, `${path}.head_sha`),
    status: stringValue(item.status, `${path}.status`),
    conclusion: stringValue(item.conclusion, `${path}.conclusion`),
    source: stringValue(item.source, `${path}.source`),
    details_url: optionalString(item.details_url, `${path}.details_url`),
    updated_at: stringValue(item.updated_at, `${path}.updated_at`),
  }
}

function parseCandidate(value: unknown, path: string): Candidate {
  const item = record(value, path)
  return {
    github_id: numberValue(item.github_id, `${path}.github_id`),
    number: numberValue(item.number, `${path}.number`),
    title: stringValue(item.title, `${path}.title`),
    draft: booleanValue(item.draft, `${path}.draft`),
    mergeable_state: optionalString(item.mergeable_state, `${path}.mergeable_state`),
    base_ref: stringValue(item.base_ref, `${path}.base_ref`),
    head_ref: stringValue(item.head_ref, `${path}.head_ref`),
    head_sha: stringValue(item.head_sha, `${path}.head_sha`),
    url: stringValue(item.url, `${path}.url`),
    ci: parseCI(item.ci, `${path}.ci`),
  }
}

function parseCandidateState(value: unknown, path: string): CandidateState {
  const item = record(value, path)
  return {
    current: item.current === undefined || item.current === null ? undefined : parseCandidate(item.current, `${path}.current`),
    alternatives: arrayValue(item.alternatives, `${path}.alternatives`, parseCandidate),
    ambiguous: booleanValue(item.ambiguous, `${path}.ambiguous`),
  }
}

function parseResultEvidence(value: unknown, path: string): ResultEvidence {
  const item = record(value, path)
  return {
    project_id: numberValue(item.project_id, `${path}.project_id`),
    issue_number: numberValue(item.issue_number, `${path}.issue_number`),
    comment_id: numberValue(item.comment_id, `${path}.comment_id`),
    role: stringValue(item.role, `${path}.role`),
    status: stringValue(item.status, `${path}.status`),
    head: optionalString(item.head, `${path}.head`),
    verdict: optionalString(item.verdict, `${path}.verdict`),
    decision: optionalString(item.decision, `${path}.decision`),
    resume_role: optionalString(item.resume_role, `${path}.resume_role`),
    resolves: item.resolves,
    escalate_to: optionalString(item.escalate_to, `${path}.escalate_to`),
    level: stringValue(item.level, `${path}.level`),
    stale: booleanValue(item.stale, `${path}.stale`),
    effective: booleanValue(item.effective, `${path}.effective`),
    created_at: stringValue(item.created_at, `${path}.created_at`),
    warnings: arrayValue(item.warnings, `${path}.warnings`, parseWarning),
    extensions: unknownObject(item.extensions, `${path}.extensions`),
  }
}

function parseLatestResults(value: unknown, path: string): LatestResults {
  const item = record(value, path)
  const optionalResult = (raw: unknown, child: string): ResultEvidence | undefined =>
    raw === undefined || raw === null ? undefined : parseResultEvidence(raw, `${path}.${child}`)
  return {
    lead: optionalResult(item.lead, 'lead'),
    implementor: optionalResult(item.implementor, 'implementor'),
    qa: optionalResult(item.qa, 'qa'),
  }
}

function parseAttention(value: unknown, path: string): Attention {
  const item = record(value, path)
  return {
    kind: stringValue(item.kind, `${path}.kind`),
    code: stringValue(item.code, `${path}.code`),
    explanation: stringValue(item.explanation, `${path}.explanation`),
  }
}

function parseRoute(value: unknown, path: string): Route {
  const item = record(value, path)
  return {
    action: stringValue(item.action, `${path}.action`),
    target_role: optionalString(item.target_role, `${path}.target_role`),
    lane_key: optionalString(item.lane_key, `${path}.lane_key`),
    reason_code: stringValue(item.reason_code, `${path}.reason_code`),
    reason: stringValue(item.reason, `${path}.reason`),
    expected_head: optionalString(item.expected_head, `${path}.expected_head`),
    guards: arrayValue(item.guards, `${path}.guards`, stringValue),
    warnings: arrayValue(item.warnings, `${path}.warnings`, parseWarning),
  }
}

function parseParsedComment(value: unknown, path: string): ParsedComment {
  const item = record(value, path)
  let hardError: ParsedComment['hard_error']
  if (item.hard_error !== undefined && item.hard_error !== null) {
    const error = record(item.hard_error, `${path}.hard_error`)
    hardError = {
      code: stringValue(error.code, `${path}.hard_error.code`),
      message: stringValue(error.message, `${path}.hard_error.message`),
    }
  }
  return {
    project_id: numberValue(item.project_id, `${path}.project_id`),
    issue_number: numberValue(item.issue_number, `${path}.issue_number`),
    comment_id: numberValue(item.comment_id, `${path}.comment_id`),
    author: stringValue(item.author, `${path}.author`),
    url: stringValue(item.url, `${path}.url`),
    created_at: stringValue(item.created_at, `${path}.created_at`),
    updated_at: stringValue(item.updated_at, `${path}.updated_at`),
    level: stringValue(item.level, `${path}.level`),
    heading: optionalString(item.heading, `${path}.heading`),
    markdown: stringValue(item.markdown, `${path}.markdown`),
    meaningful: booleanValue(item.meaningful, `${path}.meaningful`),
    transition_safe: booleanValue(item.transition_safe, `${path}.transition_safe`),
    warnings: arrayValue(item.warnings, `${path}.warnings`, parseWarning),
    hard_error: hardError,
  }
}

export function parseWorkUnitState(value: unknown, path = '$'): WorkUnitState {
  const item = record(value, path)
  return {
    identity: parseWorkUnitIdentity(item.identity, `${path}.identity`),
    lifecycle: stringValue(item.lifecycle, `${path}.lifecycle`),
    candidate: parseCandidateState(item.candidate, `${path}.candidate`),
    current_head: optionalString(item.current_head, `${path}.current_head`),
    ci: parseCI(item.ci, `${path}.ci`),
    parsed_comments: arrayValue(item.parsed_comments, `${path}.parsed_comments`, parseParsedComment),
    latest_results: parseLatestResults(item.latest_results, `${path}.latest_results`),
    active_blocker:
      item.active_blocker === undefined || item.active_blocker === null
        ? undefined
        : parseResultEvidence(item.active_blocker, `${path}.active_blocker`),
    qa_reviewed_head: optionalString(item.qa_reviewed_head, `${path}.qa_reviewed_head`),
    qa_approved_head: optionalString(item.qa_approved_head, `${path}.qa_approved_head`),
    warnings: arrayValue(item.warnings, `${path}.warnings`, parseWarning),
    last_meaningful_activity: stringValue(item.last_meaningful_activity, `${path}.last_meaningful_activity`),
    attention: parseAttention(item.attention, `${path}.attention`),
    route: parseRoute(item.route, `${path}.route`),
  }
}

function parseAttentionItem(value: unknown, path: string): AttentionItem {
  const item = record(value, path)
  return {
    project: parseProjectIdentity(item.project, `${path}.project`),
    work_unit: parseWorkUnitIdentity(item.work_unit, `${path}.work_unit`),
    attention: parseAttention(item.attention, `${path}.attention`),
    route: parseRoute(item.route, `${path}.route`),
  }
}

export function parseProjectState(value: unknown, path = '$'): ProjectState {
  const item = record(value, path)
  return {
    project: parseProjectIdentity(item.project, `${path}.project`),
    work_units: arrayValue(item.work_units, `${path}.work_units`, parseWorkUnitState),
    attention: arrayValue(item.attention, `${path}.attention`, parseAttentionItem),
  }
}

export function parseWorkspaceState(value: unknown): WorkspaceState {
  const item = record(value, '$')
  return {
    generated_at: stringValue(item.generated_at, '$.generated_at'),
    projects: arrayValue(item.projects, '$.projects', parseProjectState),
    attention: arrayValue(item.attention, '$.attention', parseAttentionItem),
  }
}

function parseRepositoryIdentity(value: unknown, path: string): RepositoryIdentity {
  const item = record(value, path)
  return {
    project_id: numberValue(item.project_id, `${path}.project_id`),
    owner: stringValue(item.owner, `${path}.owner`),
    repository: stringValue(item.repository, `${path}.repository`),
    workflow_mode: stringValue(item.workflow_mode, `${path}.workflow_mode`),
  }
}

function parseIssueIdentity(value: unknown, path: string): IssueIdentity {
  const item = record(value, path)
  return {
    github_id: numberValue(item.github_id, `${path}.github_id`),
    number: numberValue(item.number, `${path}.number`),
    title: stringValue(item.title, `${path}.title`),
    body: stringValue(item.body, `${path}.body`),
    url: stringValue(item.url, `${path}.url`),
    lifecycle: stringValue(item.lifecycle, `${path}.lifecycle`),
    attention: parseAttention(item.attention, `${path}.attention`),
  }
}

function parsePromptContext(value: unknown, path: string): PromptContext {
  const item = record(value, path)
  return {
    v: numberValue(item.v, `${path}.v`),
    repository: parseRepositoryIdentity(item.repository, `${path}.repository`),
    issue: parseIssueIdentity(item.issue, `${path}.issue`),
    current_head: stringValue(item.current_head, `${path}.current_head`),
    route: parseRoute(item.route, `${path}.route`),
    expected_event: stringValue(item.expected_event, `${path}.expected_event`),
    warnings: arrayValue(item.warnings, `${path}.warnings`, parseWarning),
    context_hash: stringValue(item.context_hash, `${path}.context_hash`),
  }
}

function parseSourceMetadata(value: unknown, path: string): SourceMetadata {
  const item = record(value, path)
  return {
    kind: stringValue(item.kind, `${path}.kind`),
    runtime: stringValue(item.runtime, `${path}.runtime`),
    provider: optionalString(item.provider, `${path}.provider`),
    model: optionalString(item.model, `${path}.model`),
    agent: optionalString(item.agent, `${path}.agent`),
    mode: stringValue(item.mode, `${path}.mode`),
    context_hash: stringValue(item.context_hash, `${path}.context_hash`),
  }
}

function parsePromptPlan(value: unknown, path: string): PromptPlan {
  const item = record(value, path)
  return {
    v: numberValue(item.v, `${path}.v`),
    action: stringValue(item.action, `${path}.action`),
    target_role: stringValue(item.target_role, `${path}.target_role`),
    lane_key: stringValue(item.lane_key, `${path}.lane_key`),
    summary: stringValue(item.summary, `${path}.summary`),
    reason: stringValue(item.reason, `${path}.reason`),
    risk: stringValue(item.risk, `${path}.risk`),
    requires_owner: booleanValue(item.requires_owner, `${path}.requires_owner`),
    expected_head: stringValue(item.expected_head, `${path}.expected_head`),
    expected_event: stringValue(item.expected_event, `${path}.expected_event`),
    guards: arrayValue(item.guards, `${path}.guards`, stringValue),
    prohibited_actions: arrayValue(item.prohibited_actions, `${path}.prohibited_actions`, stringValue),
    prompt: stringValue(item.prompt, `${path}.prompt`),
    confidence: numberValue(item.confidence, `${path}.confidence`),
    source: parseSourceMetadata(item.source, `${path}.source`),
    extensions: unknownObject(item.extensions, `${path}.extensions`),
  }
}

function parseViolation(value: unknown, path: string): Violation {
  const item = record(value, path)
  return {
    code: stringValue(item.code, `${path}.code`),
    field: optionalString(item.field, `${path}.field`),
    message: stringValue(item.message, `${path}.message`),
  }
}

function parsePolicyDecision(value: unknown, path: string): PolicyDecision {
  const item = record(value, path)
  return {
    status: stringValue(item.status, `${path}.status`),
    violations: arrayValue(item.violations, `${path}.violations`, parseViolation),
    context_hash: stringValue(item.context_hash, `${path}.context_hash`),
    plan_hash: optionalString(item.plan_hash, `${path}.plan_hash`),
    decided_at: stringValue(item.decided_at, `${path}.decided_at`),
  }
}

export function parseGenerationResult(value: unknown, path = '$'): GenerationResult {
  const item = record(value, path)
  return {
    status: stringValue(item.status, `${path}.status`),
    context: parsePromptContext(item.context, `${path}.context`),
    plan: item.plan === undefined || item.plan === null ? undefined : parsePromptPlan(item.plan, `${path}.plan`),
    policy_decision: parsePolicyDecision(item.policy_decision, `${path}.policy_decision`),
    plan_id: optionalNumber(item.plan_id, `${path}.plan_id`),
    created_at: stringValue(item.created_at, `${path}.created_at`),
  }
}

export function parsePlanHistory(value: unknown): GenerationResult[] {
  const item = record(value, '$')
  return arrayValue(item.plans, '$.plans', parseGenerationResult)
}

export function parseContextSummary(value: unknown): ContextSummary {
  const item = record(value, '$')
  return {
    v: numberValue(item.v, '$.v'),
    context_hash: stringValue(item.context_hash, '$.context_hash'),
    repository: parseRepositoryIdentity(item.repository, '$.repository'),
    issue: parseIssueIdentity(item.issue, '$.issue'),
    current_head: optionalString(item.current_head, '$.current_head'),
    route: parseRoute(item.route, '$.route'),
    expected_event: stringValue(item.expected_event, '$.expected_event'),
    evidence_count: numberValue(item.evidence_count, '$.evidence_count'),
    warning_count: numberValue(item.warning_count, '$.warning_count'),
  }
}

export function parsePlannerHealth(value: unknown): PlannerHealth {
  const item = record(value, '$')
  return {
    enabled: booleanValue(item.enabled, '$.enabled'),
    status: stringValue(item.status, '$.status'),
    runtime: stringValue(item.runtime, '$.runtime'),
    endpoint: optionalString(item.endpoint, '$.endpoint'),
    provider: optionalString(item.provider, '$.provider'),
    model: optionalString(item.model, '$.model'),
    agent: optionalString(item.agent, '$.agent'),
    error: optionalString(item.error, '$.error'),
  }
}

export function parseProjectSnapshot(value: unknown): ProjectSnapshot {
  const item = record(value, '$')
  return { project: parseProject(item.project, '$.project') }
}

export function assertObjectResponse(value: unknown): void {
  record(value, '$')
}
