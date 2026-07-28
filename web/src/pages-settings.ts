import { HealthResponse, PlannerHealth } from './domain.js'
import { SettingsContent } from './ui.js'
import { api, resourceContent, useResource } from './app-runtime.js'

type SettingsBundle = { backend: HealthResponse; planner: PlannerHealth }

export function SettingsPage(): unknown {
  const resource = useResource<SettingsBundle>('settings', async (signal) => {
    const [backend, planner] = await Promise.all([api.health(signal), api.plannerHealth(signal)])
    return { backend, planner }
  })
  return resourceContent(resource, (bundle) => SettingsContent({
    backend: bundle.backend,
    planner: bundle.planner,
    onRefresh: resource.refresh,
  }), 'Checking runtime health…')
}
