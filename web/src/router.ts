export type RouteState =
  | { kind: 'workspace' }
  | { kind: 'project'; projectID: number }
  | { kind: 'work-unit'; projectID: number; issueNumber: number }
  | { kind: 'plans'; projectID: number; issueNumber: number; planID?: number }
  | { kind: 'settings' }
  | { kind: 'not-found'; path: string }

function positiveInteger(value: string | undefined): number | null {
  if (value === undefined || !/^\d+$/.test(value)) {
    return null
  }
  const parsed = Number.parseInt(value, 10)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null
}

export function parseRoute(pathname: string): RouteState {
  const path = pathname.replace(/\/+$/, '') || '/'
  if (path === '/') return { kind: 'workspace' }
  if (path === '/settings') return { kind: 'settings' }

  const parts = path.split('/').filter(Boolean)
  if (parts[0] !== 'projects') return { kind: 'not-found', path: pathname }
  const projectID = positiveInteger(parts[1])
  if (projectID === null) return { kind: 'not-found', path: pathname }
  if (parts.length === 2) return { kind: 'project', projectID }
  if (parts[2] !== 'work-units') return { kind: 'not-found', path: pathname }
  const issueNumber = positiveInteger(parts[3])
  if (issueNumber === null) return { kind: 'not-found', path: pathname }
  if (parts.length === 4) return { kind: 'work-unit', projectID, issueNumber }
  if (parts[4] !== 'plans') return { kind: 'not-found', path: pathname }
  if (parts.length === 5) return { kind: 'plans', projectID, issueNumber }
  const planID = positiveInteger(parts[5])
  if (parts.length === 6 && planID !== null) return { kind: 'plans', projectID, issueNumber, planID }
  return { kind: 'not-found', path: pathname }
}

export const paths = {
  workspace: (): string => '/',
  settings: (): string => '/settings',
  project: (projectID: number): string => `/projects/${projectID}`,
  workUnit: (projectID: number, issueNumber: number): string => `/projects/${projectID}/work-units/${issueNumber}`,
  plans: (projectID: number, issueNumber: number): string =>
    `/projects/${projectID}/work-units/${issueNumber}/plans`,
  plan: (projectID: number, issueNumber: number, planID: number): string =>
    `/projects/${projectID}/work-units/${issueNumber}/plans/${planID}`,
}
