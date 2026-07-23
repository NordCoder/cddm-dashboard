type StateUpdater<T> = (next: T | ((current: T) => T)) => void

type ReactGlobal = {
  StrictMode: unknown
  createElement(type: unknown, props?: unknown, ...children: unknown[]): unknown
  useEffect(effect: () => void | (() => void), dependencies: readonly unknown[]): void
  useState<T>(initial: T): [T, StateUpdater<T>]
}

type ReactDOMGlobal = {
  createRoot(container: Element): {
    render(node: unknown): void
  }
}

declare const React: ReactGlobal
declare const ReactDOM: ReactDOMGlobal

type ConnectionState =
  | { kind: 'checking'; message: string }
  | { kind: 'connected'; message: string }
  | { kind: 'error'; message: string }

type HealthResponse = {
  status: string
  database: string
}

function App(): unknown {
  const [connection, setConnection] = React.useState<ConnectionState>({
    kind: 'checking',
    message: 'Checking backend connection…',
  })

  React.useEffect(() => {
    const controller = new AbortController()

    async function checkHealth(): Promise<void> {
      try {
        const response = await fetch('/api/health', { signal: controller.signal })
        if (!response.ok) {
          throw new Error(`Backend returned HTTP ${response.status}`)
        }

        const health = (await response.json()) as HealthResponse
        if (health.status !== 'healthy') {
          throw new Error(`Backend status is ${health.status}`)
        }

        setConnection({
          kind: 'connected',
          message: `Backend connected · SQLite ${health.database}`,
        })
      } catch (error) {
        if (controller.signal.aborted) {
          return
        }
        const message = error instanceof Error ? error.message : 'Unknown connection error'
        setConnection({ kind: 'error', message })
      }
    }

    void checkHealth()
    return () => controller.abort()
  }, [])

  return React.createElement(
    'main',
    { className: 'shell' },
    React.createElement(
      'section',
      { className: 'panel', 'aria-labelledby': 'page-title' },
      React.createElement('p', { className: 'eyebrow' }, 'Application foundation'),
      React.createElement('h1', { id: 'page-title' }, 'CDDM Dashboard'),
      React.createElement(
        'p',
        { className: 'summary' },
        'The Stage 1 scaffold is running. Later roadmap stages can add GitHub synchronization, routing, planning, and extension capabilities on this foundation.',
      ),
      React.createElement(
        'div',
        {
          className: `status status--${connection.kind}`,
          role: 'status',
          'aria-live': 'polite',
        },
        React.createElement('span', { className: 'status__dot', 'aria-hidden': 'true' }),
        React.createElement('span', null, connection.message),
      ),
    ),
  )
}

const rootElement = document.getElementById('root')
if (rootElement === null) {
  throw new Error('Missing #root element')
}

ReactDOM.createRoot(rootElement).render(
  React.createElement(React.StrictMode, null, React.createElement(App)),
)
