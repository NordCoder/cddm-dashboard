import { parseRoute, RouteState } from './router.js'
import { Navigate } from './ui-shared.js'

export function useRoute(): [RouteState, Navigate] {
  const [route, setRoute] = React.useState<RouteState>(() => parseRoute(globalThis.location.pathname))

  React.useEffect(() => {
    const onPopState = () => setRoute(parseRoute(globalThis.location.pathname))
    globalThis.addEventListener('popstate', onPopState)
    return () => globalThis.removeEventListener('popstate', onPopState)
  }, [])

  const navigate = React.useCallback<Navigate>((path) => {
    if (path === globalThis.location.pathname) return
    globalThis.history.pushState({}, '', path)
    setRoute(parseRoute(path))
    globalThis.scrollTo({ top: 0, behavior: 'auto' })
  }, [])

  return [route, navigate]
}

export function routeLabel(route: RouteState): string {
  switch (route.kind) {
    case 'workspace': return 'Workspace'
    case 'settings': return 'System health'
    case 'project': return `Project ${route.projectID}`
    case 'work-unit': return `Project ${route.projectID} · Issue #${route.issueNumber}`
    case 'plans': return `Project ${route.projectID} · #${route.issueNumber} · Plans`
    case 'not-found': return 'Not found'
  }
}
