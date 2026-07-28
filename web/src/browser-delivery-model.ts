import { BrowserBinding, BrowserTarget, DeliveryConfirmationInput } from './browser-api.js'
import { GenerationResult, WorkUnitState } from './domain.js'

export type DeliveryRouteIdentity = { projectID: number; issueNumber: number }
export type DeliveryEligibility = { ready: boolean; reason: string }

export function deliveryRouteIdentity(path: string): DeliveryRouteIdentity | null {
  const match = path.match(/^\/projects\/(\d+)\/work-units\/(\d+)(?:\/plans(?:\/\d+)?)?\/?$/)
  if (!match) return null
  const projectID = Number(match[1])
  const issueNumber = Number(match[2])
  return Number.isSafeInteger(projectID) && projectID > 0 && Number.isSafeInteger(issueNumber) && issueNumber > 0
    ? { projectID, issueNumber }
    : null
}

export function deliveryEligibility(workUnit: WorkUnitState, result: GenerationResult | null, binding: BrowserBinding | null): DeliveryEligibility {
  if (!result?.plan) return { ready: false, reason: 'No current Prompt Plan.' }
  if (!result.plan_id) return { ready: false, reason: 'Current plan has no persistent plan identity.' }
  const policyReady = result.status === 'approved' || (result.status === 'fallback' && result.policy_decision.status === 'approved')
  if (!policyReady) return { ready: false, reason: `Plan status ${result.status} is not dispatchable.` }
  if (result.plan.requires_owner || workUnit.attention.kind === 'owner_required') return { ready: false, reason: 'Owner action is required before browser delivery.' }
  if (result.plan.action !== 'dispatch' || workUnit.route.action !== 'dispatch') return { ready: false, reason: 'The current backend route is not dispatch.' }
  if (!workUnit.route.lane_key || result.plan.lane_key !== workUnit.route.lane_key) return { ready: false, reason: 'Plan lane no longer matches the current backend route.' }
  if (result.plan.expected_head !== (workUnit.current_head ?? '')) return { ready: false, reason: 'Plan Head no longer matches the current work unit.' }
  if (!result.policy_decision.plan_hash) return { ready: false, reason: 'Current policy decision has no plan hash.' }
  if (!binding) return { ready: false, reason: 'No browser binding exists for the current lane.' }
  if (binding.readiness !== 'ready') return { ready: false, reason: `Browser binding is ${binding.readiness}.` }
  if (!binding.presence_token || !binding.binding_id || binding.binding_version <= 0) return { ready: false, reason: 'Browser binding has no current presence proof.' }
  if (binding.lane_key !== workUnit.route.lane_key) return { ready: false, reason: 'Browser binding lane no longer matches the route.' }
  return { ready: true, reason: 'Ready for explicit confirmation.' }
}

export function buildConfirmationInput(result: GenerationResult, binding: BrowserBinding, idempotencyKey: string): DeliveryConfirmationInput {
  if (!result.plan || !result.plan_id || !result.policy_decision.plan_hash || !binding.presence_token) throw new Error('delivery_identity_incomplete')
  return {
    plan_id: result.plan_id,
    idempotency_key: idempotencyKey,
    expected_plan_hash: result.policy_decision.plan_hash,
    expected_context_hash: result.context.context_hash,
    expected_head: result.plan.expected_head,
    expected_lane_key: result.plan.lane_key,
    expected_binding_id: binding.binding_id,
    expected_binding_version: binding.binding_version,
    expected_presence_token: binding.presence_token,
  }
}

export function targetLabel(target: BrowserTarget): string {
  return `${target.origin}${target.path}`
}

export function shortIdentity(value: string, length = 12): string {
  return value.length <= length ? value : `${value.slice(0, length)}…`
}

export function createIdempotencyKey(cryptoApi: Crypto = globalThis.crypto): string {
  if (cryptoApi?.randomUUID) return cryptoApi.randomUUID()
  if (!cryptoApi?.getRandomValues) throw new Error('secure_random_unavailable')
  const bytes = new Uint8Array(16)
  cryptoApi.getRandomValues(bytes)
  return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')
}
