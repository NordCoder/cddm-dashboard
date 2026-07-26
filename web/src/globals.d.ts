type StateUpdater<T> = (next: T | ((current: T) => T)) => void

type ReactGlobal = {
  StrictMode: unknown
  Fragment: unknown
  createElement(type: unknown, props?: unknown, ...children: unknown[]): unknown
  useEffect(effect: () => void | (() => void), dependencies: readonly unknown[]): void
  useMemo<T>(factory: () => T, dependencies: readonly unknown[]): T
  useRef<T>(initial: T): { current: T }
  useCallback<T extends (...args: never[]) => unknown>(callback: T, dependencies: readonly unknown[]): T
  useState<T>(initial: T | (() => T)): [T, StateUpdater<T>]
}

type ReactDOMGlobal = {
  createRoot(container: Element): {
    render(node: unknown): void
  }
}

declare var React: ReactGlobal
declare var ReactDOM: ReactDOMGlobal
