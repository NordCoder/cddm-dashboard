import {
  assertObjectResponse,
  ContextSummary,
  GenerationResult,
  HealthResponse,
  parseContextSummary,
  parseGenerationResult,
  parseHealth,
  parsePlanHistory,
  parsePlannerHealth,
  parseProject,
  parseProjects,
  parseProjectSnapshot,
  parseProjectState,
  parseWorkspaceState,
  parseWorkUnitState,
  PlannerHealth,
  Project,
  ProjectState,
  ValidationError,
  WorkspaceState,
  WorkUnitState,
} from './domain.js'

export type PlanningMode = 'opencode' | 'fallback'

export type CreateProjectInput = {
  owner: string
  repository: string
  workflow_mode?: string
  polling_enabled?: boolean
  poll_interval_seconds?: number
}

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export class BackendResponseError extends Error {
  constructor(message: string, cause?: unknown) {
    super(message, cause === undefined ? undefined : { cause })
    this.name = 'BackendResponseError'
  }
}

type Parser<T> = (value: unknown) => T
type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

type RequestOptions = {
  method?: string
  body?: unknown
  signal?: AbortSignal
  acceptStatuses?: number[]
}

async function readBody(response: Response): Promise<unknown> {
  const text = await response.text()
  if (text.trim() === '') {
    return undefined
  }
  try {
    return JSON.parse(text) as unknown
  } catch (error) {
    throw new BackendResponseError('Backend returned malformed JSON', error)
  }
}

function backendErrorMessage(body: unknown, fallback: string): string {
  if (typeof body === 'object' && body !== null && !Array.isArray(body)) {
    const value = (body as Record<string, unknown>).error
    if (typeof value === 'string' && value.trim() !== '') {
      return value
    }
  }
  return fallback
}

export class ApiClient {
  private readonly fetcher: FetchLike

  constructor(fetcher: FetchLike = globalThis.fetch.bind(globalThis)) {
    this.fetcher = fetcher
  }

  private async request<T>(path: string, parser: Parser<T>, options: RequestOptions = {}): Promise<T> {
    let response: Response
    try {
      response = await this.fetcher(path, {
        method: options.method ?? 'GET',
        signal: options.signal,
        headers: options.body === undefined ? undefined : { 'Content-Type': 'application/json' },
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
      })
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        throw error
      }
      throw new ApiError(0, error instanceof Error ? error.message : 'Backend is unavailable')
    }

    let body: unknown
    try {
      body = await readBody(response)
    } catch (error) {
      if (!response.ok && !(options.acceptStatuses ?? []).includes(response.status)) {
        throw new ApiError(response.status, `Backend returned HTTP ${response.status}`)
      }
      throw error
    }

    if (!response.ok && !(options.acceptStatuses ?? []).includes(response.status)) {
      throw new ApiError(response.status, backendErrorMessage(body, `Backend returned HTTP ${response.status}`))
    }

    try {
      return parser(body)
    } catch (error) {
      if (error instanceof ValidationError) {
        throw new BackendResponseError(error.message, error)
      }
      throw error
    }
  }

  async health(signal?: AbortSignal): Promise<HealthResponse> {
    return this.request('/api/health', parseHealth, { signal, acceptStatuses: [503] })
  }

  async plannerHealth(signal?: AbortSignal): Promise<PlannerHealth> {
    return this.request('/api/planner/health', parsePlannerHealth, { signal, acceptStatuses: [503] })
  }

  async projects(signal?: AbortSignal): Promise<Project[]> {
    return this.request('/api/projects', parseProjects, { signal })
  }

  async createProject(input: CreateProjectInput, signal?: AbortSignal): Promise<Project> {
    return this.request('/api/projects', (value) => parseProject(value), {
      method: 'POST',
      body: input,
      signal,
    })
  }

  async deleteProject(projectID: number, signal?: AbortSignal): Promise<void> {
    let response: Response
    try {
      response = await this.fetcher(`/api/projects/${projectID}`, { method: 'DELETE', signal })
    } catch (error) {
      throw new ApiError(0, error instanceof Error ? error.message : 'Backend is unavailable')
    }
    if (!response.ok) {
      const body = await readBody(response)
      throw new ApiError(response.status, backendErrorMessage(body, `Backend returned HTTP ${response.status}`))
    }
    if (response.status !== 204) {
      throw new BackendResponseError(`Malformed backend response: expected HTTP 204, received ${response.status}`)
    }
  }

  async syncProject(projectID: number, signal?: AbortSignal): Promise<void> {
    await this.request(`/api/projects/${projectID}/sync`, (value) => {
      assertObjectResponse(value)
      return undefined
    }, { method: 'POST', signal })
  }

  async projectMetadata(projectID: number, signal?: AbortSignal): Promise<Project> {
    const snapshot = await this.request(`/api/projects/${projectID}`, parseProjectSnapshot, { signal })
    return snapshot.project
  }

  async workspaceState(signal?: AbortSignal): Promise<WorkspaceState> {
    return this.request('/api/workspace/state', parseWorkspaceState, { signal })
  }

  async projectState(projectID: number, signal?: AbortSignal): Promise<ProjectState> {
    return this.request(`/api/projects/${projectID}/state`, parseProjectState, { signal })
  }

  async workUnitState(projectID: number, issueNumber: number, signal?: AbortSignal): Promise<WorkUnitState> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/state`, parseWorkUnitState, { signal })
  }

  async generatePlan(
    projectID: number,
    issueNumber: number,
    mode: PlanningMode,
    signal?: AbortSignal,
  ): Promise<GenerationResult> {
    return this.request(`/api/projects/${projectID}/work-units/${issueNumber}/plans`, parseGenerationResult, {
      method: 'POST',
      body: { mode },
      signal,
    })
  }

  async latestPlan(projectID: number, issueNumber: number, signal?: AbortSignal): Promise<GenerationResult | null> {
    try {
      return await this.request(
        `/api/projects/${projectID}/work-units/${issueNumber}/plans/latest`,
        parseGenerationResult,
        { signal },
      )
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) {
        return null
      }
      throw error
    }
  }

  async planHistory(
    projectID: number,
    issueNumber: number,
    limit = 20,
    signal?: AbortSignal,
  ): Promise<GenerationResult[]> {
    return this.request(
      `/api/projects/${projectID}/work-units/${issueNumber}/plans?limit=${limit}`,
      parsePlanHistory,
      { signal },
    )
  }

  async plan(projectID: number, issueNumber: number, planID: number, signal?: AbortSignal): Promise<GenerationResult> {
    return this.request(
      `/api/projects/${projectID}/work-units/${issueNumber}/plans/${planID}`,
      parseGenerationResult,
      { signal },
    )
  }

  async planningContext(projectID: number, issueNumber: number, signal?: AbortSignal): Promise<ContextSummary> {
    return this.request(
      `/api/projects/${projectID}/work-units/${issueNumber}/planning/context`,
      parseContextSummary,
      { signal },
    )
  }
}
