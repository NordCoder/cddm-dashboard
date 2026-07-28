import { useRoute, routeLabel } from './app-routing.js'
import { PlanningPage, WorkUnitPage } from './pages-work-unit.js'
import { ProjectPage, WorkspacePage } from './pages-workspace.js'
import { SettingsPage } from './pages-settings.js'
import { paths } from './router.js'
import { AppShell, InternalLink } from './ui.js'

const h = React.createElement

function routePage(route: ReturnType<typeof useRoute>[0], navigate: ReturnType<typeof useRoute>[1]): unknown {
  switch (route.kind) {
    case 'workspace':
      return h(WorkspacePage, { navigate })
    case 'project':
      return h(ProjectPage, { projectID: route.projectID, navigate })
    case 'work-unit':
      return h(WorkUnitPage, { projectID: route.projectID, issueNumber: route.issueNumber, navigate })
    case 'plans':
      return h(PlanningPage, { projectID: route.projectID, issueNumber: route.issueNumber, planID: route.planID, navigate })
    case 'settings':
      return h(SettingsPage)
    case 'not-found':
      return h(
        React.Fragment,
        null,
        h('h1', null, 'Page not found'),
        h('p', { className: 'muted' }, `No dashboard route matches ${route.path}.`),
        InternalLink({ href: paths.workspace(), navigate, className: 'button button--primary', children: 'Back to Workspace' }),
      )
  }
}

export function App(): unknown {
  const [route, navigate] = useRoute()
  return AppShell({ routeLabel: routeLabel(route), navigate, children: routePage(route, navigate) })
}

function installFatalFallback(root: HTMLElement): void {
  const show = () => {
    const fallback = document.createElement('main')
    fallback.className = 'fatal-fallback'
    fallback.setAttribute('role', 'alert')
    const title = document.createElement('strong')
    title.textContent = 'Dashboard rendering failed'
    const copy = document.createElement('p')
    copy.textContent = 'Reload the page. If the problem persists, check the browser console and backend availability.'
    fallback.append(title, copy)
    root.replaceChildren(fallback)
  }
  globalThis.addEventListener('error', show)
  globalThis.addEventListener('unhandledrejection', show)
}

export function mountApp(root: HTMLElement): void {
  installFatalFallback(root)
  ReactDOM.createRoot(root).render(React.createElement(React.StrictMode, null, React.createElement(App)))
}

if (typeof document !== 'undefined') {
  const rootElement = document.getElementById('root')
  if (!(rootElement instanceof HTMLElement)) throw new Error('Missing #root element')
  mountApp(rootElement)
}
