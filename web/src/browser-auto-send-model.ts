import { BrowserBinding, DeliveryCommand } from './browser-api.js'
import { GenerationResult } from './domain.js'

const ENABLED_PREFIX = 'cddm.browser-delivery.auto-send.enabled'
const RETRY_INTERVAL_MS = 15_000

export type AutoSendRoute = { projectID: number; issueNumber: number }
export type AutoSendStatus = 'pending' | 'submitted' | 'blocked'

export type AutoSendRecord = {
  identity: string
  idempotencyKey: string
  status: AutoSendStatus
  lastAttemptAt: number
  message?: string
}

export function automaticDeliveryRoute(path: string): AutoSendRoute | null {
  const match = path.match(/^\/projects\/(\d+)\/work-units\/(\d+)(?:\/plans)?\/?$/)
  if (!match) return null
  const projectID = Number(match[1])
  const issueNumber = Number(match[2])
  return Number.isSafeInteger(projectID) && projectID > 0 && Number.isSafeInteger(issueNumber) && issueNumber > 0
    ? { projectID, issueNumber }
    : null
}

export function autoSendPreferenceKey(route: AutoSendRoute): string {
  return `${ENABLED_PREFIX}:${route.projectID}:${route.issueNumber}`
}

export function readAutoSendEnabled(path: string, storage: Pick<Storage, 'getItem'> | undefined = globalThis.localStorage): boolean {
  const route = automaticDeliveryRoute(path)
  if (!route || !storage) return false
  try { return storage.getItem(autoSendPreferenceKey(route)) === 'true' } catch { return false }
}

export function writeAutoSendEnabled(path: string, enabled: boolean, storage: Pick<Storage, 'setItem'> | undefined = globalThis.localStorage): boolean {
  const route = automaticDeliveryRoute(path)
  if (!route || !storage) return false
  try {
    storage.setItem(autoSendPreferenceKey(route), String(enabled))
    return true
  } catch {
    return false
  }
}

function presenceFingerprint(value: string): string {
  let hash = 0x811c9dc5
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 0x01000193)
  }
  return (hash >>> 0).toString(16).padStart(8, '0')
}

export function automaticDeliveryIdentity(
  projectID: number,
  issueNumber: number,
  result: GenerationResult,
  binding: BrowserBinding,
): string {
  if (!result.plan || !result.plan_id || !result.policy_decision.plan_hash || !binding.presence_token) {
    throw new Error('automatic_delivery_identity_incomplete')
  }
  return JSON.stringify([
    projectID,
    issueNumber,
    result.plan_id,
    result.policy_decision.plan_hash,
    result.context.context_hash,
    result.plan.expected_head,
    result.plan.lane_key,
    binding.binding_id,
    binding.binding_version,
    presenceFingerprint(binding.presence_token),
  ])
}

export function matchingDeliveryExists(
  deliveries: DeliveryCommand[],
  result: GenerationResult,
  binding: BrowserBinding,
): boolean {
  if (!result.plan || !result.plan_id || !result.policy_decision.plan_hash) return false
  return deliveries.some((command) => (
    command.plan_id === result.plan_id
    && command.plan_hash === result.policy_decision.plan_hash
    && command.expected_head === result.plan!.expected_head
    && command.lane_key === result.plan!.lane_key
    && command.binding_id === binding.binding_id
    && command.binding_version === binding.binding_version
  ))
}

export function autoSendRetryDue(record: AutoSendRecord, now: number): boolean {
  return record.status === 'pending' && now - record.lastAttemptAt >= RETRY_INTERVAL_MS
}
