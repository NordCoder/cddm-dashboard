import { ApiClient, ApiError, BackendResponseError } from './api.js'
import { ErrorState, LoadingState } from './ui-shared.js'

export const api = new ApiClient()
const DEFAULT_REFRESH_INTERVAL_MS = 60_000

export type ResourceState<T> =
  | { kind: 'loading' }
  | { kind: 'ready'; data: T }
  | { kind: 'error'; message: string }

export type Resource<T> = {
  state: ResourceState<T>
  refresh: () => void
}

export function errorMessage(error: unknown): string {
  if (error instanceof BackendResponseError) return error.message
  if (error instanceof ApiError) {
    return error.status === 0 ? `Backend unavailable: ${error.message}` : `Backend error (${error.status}): ${error.message}`
  }
  return error instanceof Error ? error.message : 'Unknown dashboard error'
}

export function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}

export function useResource<T>(
  key: string,
  loader: (signal: AbortSignal) => Promise<T>,
  refreshInterval = DEFAULT_REFRESH_INTERVAL_MS,
): Resource<T> {
  const [state, setState] = React.useState<ResourceState<T>>({ kind: 'loading' })
  const [revision, setRevision] = React.useState(0)

  React.useEffect(() => {
    const controller = new AbortController()
    let active = true
    setState({ kind: 'loading' })

    void loader(controller.signal)
      .then((data) => {
        if (active) setState({ kind: 'ready', data })
      })
      .catch((error: unknown) => {
        if (active && !isAbort(error)) setState({ kind: 'error', message: errorMessage(error) })
      })

    const timer = refreshInterval > 0
      ? globalThis.setInterval(() => setRevision((current) => current + 1), refreshInterval)
      : undefined

    return () => {
      active = false
      controller.abort()
      if (timer !== undefined) globalThis.clearInterval(timer)
    }
  }, [key, revision])

  return {
    state,
    refresh: () => setRevision((current) => current + 1),
  }
}

export function resourceContent<T>(resource: Resource<T>, render: (data: T) => unknown, loadingLabel?: string): unknown {
  if (resource.state.kind === 'loading') return LoadingState({ label: loadingLabel })
  if (resource.state.kind === 'error') return ErrorState({ message: resource.state.message, onRetry: resource.refresh })
  return render(resource.state.data)
}
