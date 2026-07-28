import { ApiClient, ApiError } from './api.js'
import { BrowserApiClient, BrowserBinding, DeliveryCommand } from './browser-api.js'
import {
  buildConfirmationInput,
  createIdempotencyKey,
  deliveryEligibility,
  deliveryRouteIdentity,
} from './browser-delivery-model.js'
import { GenerationResult } from './domain.js'

const ENABLED_KEY = 'cddm.browser-delivery.auto-send.enabled'
const RECORD_PREFIX = 'cddm.browser-delivery.auto-send.record'
const POLL_INTERVAL_MS = 5_000
const RETRY_INTERVAL_MS = 15_000

type AutoSendStatus = 'pending' | 'submitted' | 'blocked'

export type AutoSendRecord = {
  identity: string
  idempotencyKey: string
  status: AutoSendStatus
  lastAttemptAt: number
  message?: string
}

const memoryRecords = new Map<string, AutoSendRecord>()

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
    binding.presence_token,
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

function readEnabled(): boolean {
  try { return globalThis.localStorage?.getItem(ENABLED_KEY) === 'true' } catch { return false }
}

function writeEnabled(enabled: boolean): void {
  try { globalThis.localStorage?.setItem(ENABLED_KEY, String(enabled)) } catch { /* local preference only */ }
}

function recordKey(projectID: number, issueNumber: number): string {
  return `${RECORD_PREFIX}:${projectID}:${issueNumber}`
}

function readRecord(key: string): AutoSendRecord | null {
  try {
    const raw = globalThis.localStorage?.getItem(key)
    if (!raw) return memoryRecords.get(key) ?? null
    const parsed = JSON.parse(raw) as Partial<AutoSendRecord>
    if (
      typeof parsed.identity !== 'string'
      || typeof parsed.idempotencyKey !== 'string'
      || (parsed.status !== 'pending' && parsed.status !== 'submitted' && parsed.status !== 'blocked')
      || typeof parsed.lastAttemptAt !== 'number'
    ) return null
    return parsed as AutoSendRecord
  } catch {
    return memoryRecords.get(key) ?? null
  }
}

function writeRecord(key: string, record: AutoSendRecord): void {
  memoryRecords.set(key, record)
  try { globalThis.localStorage?.setItem(key, JSON.stringify(record)) } catch { /* memory fallback remains */ }
}

function editedPrompt(result: GenerationResult): boolean {
  const textarea = document.querySelector('textarea[aria-label="Prompt text"]') as HTMLTextAreaElement | null
  return Boolean(textarea && result.plan && textarea.value !== result.plan.prompt)
}

function message(error: unknown): string {
  if (error instanceof ApiError) return error.status === 0 ? `Transport unresolved: ${error.message}` : `Backend rejected auto-send (${error.status}): ${error.message}`
  return error instanceof Error ? error.message : 'Unknown automatic delivery error'
}

class BrowserAutoSendController {
  private readonly core = new ApiClient()
  private readonly browser = new BrowserApiClient()
  private checkbox: HTMLInputElement | null = null
  private status: HTMLElement | null = null
  private running = false
  private stopped = false
  private timer: number | undefined
  private observer: MutationObserver | null = null

  start(): void {
    this.stopped = false
    this.ensureControls()
    this.observer = new MutationObserver(() => this.ensureControls())
    this.observer.observe(document.body, { childList: true, subtree: true })
    this.timer = globalThis.setInterval(() => void this.tick(), POLL_INTERVAL_MS)
    void this.tick()
  }

  stop(): void {
    this.stopped = true
    this.observer?.disconnect()
    if (this.timer !== undefined) globalThis.clearInterval(this.timer)
  }

  private ensureControls(): void {
    const inspector = document.getElementById('cddm-browser-delivery')
    if (!inspector || inspector.querySelector('.delivery-auto-send')) return
    const headerActions = inspector.querySelector('.delivery-inspector__header-actions')
    const identity = inspector.querySelector('.delivery-inspector__identity')
    if (!(headerActions instanceof HTMLElement) || !(identity instanceof HTMLElement)) return

    const label = document.createElement('label')
    label.className = 'delivery-auto-send'
    label.title = 'Automatically send each new backend-approved immutable prompt when this browser binding is ready.'
    const checkbox = document.createElement('input')
    checkbox.type = 'checkbox'
    checkbox.checked = readEnabled()
    checkbox.setAttribute('aria-label', 'Automatically send approved prompts without review')
    const copy = document.createElement('span')
    copy.textContent = 'Auto-send'
    label.append(checkbox, copy)
    headerActions.prepend(label)

    const status = document.createElement('span')
    status.className = 'delivery-auto-send-state'
    status.setAttribute('role', 'status')
    identity.append(status)

    checkbox.addEventListener('change', () => {
      writeEnabled(checkbox.checked)
      this.setStatus(checkbox.checked ? 'Enabled; waiting for a new approved plan.' : 'Off')
      if (checkbox.checked) void this.tick()
    })
    this.checkbox = checkbox
    this.status = status
    this.setStatus(checkbox.checked ? 'Enabled' : 'Off')
  }

  private setStatus(value: string): void {
    this.ensureControls()
    if (this.status) this.status.textContent = `Auto-send: ${value}`
  }

  private async tick(): Promise<void> {
    this.ensureControls()
    if (this.stopped || this.running || !readEnabled()) {
      if (!readEnabled()) this.setStatus('Off')
      return
    }
    const route = deliveryRouteIdentity(globalThis.location.pathname)
    if (!route) {
      this.setStatus('Waiting for a work unit')
      return
    }

    this.running = true
    try {
      const [workUnit, result, deliveries] = await Promise.all([
        this.core.workUnitState(route.projectID, route.issueNumber),
        this.core.latestPlan(route.projectID, route.issueNumber),
        this.browser.deliveries(route.projectID, route.issueNumber),
      ])
      if (!result || !workUnit.route.lane_key) {
        this.setStatus('No current dispatch plan')
        return
      }
      const binding = (await this.browser.browserBinding(route.projectID, route.issueNumber)).binding
      const eligibility = deliveryEligibility(workUnit, result, binding)
      if (!binding || !eligibility.ready) {
        this.setStatus(eligibility.reason)
        return
      }
      if (editedPrompt(result)) {
        this.setStatus('Paused because the visible prompt has local edits')
        return
      }

      const identity = automaticDeliveryIdentity(route.projectID, route.issueNumber, result, binding)
      const key = recordKey(route.projectID, route.issueNumber)
      if (matchingDeliveryExists(deliveries, result, binding)) {
        const existing = readRecord(key)
        writeRecord(key, {
          identity,
          idempotencyKey: existing?.identity === identity ? existing.idempotencyKey : createIdempotencyKey(),
          status: 'submitted',
          lastAttemptAt: Date.now(),
          message: 'A command already exists for this exact plan and binding.',
        })
        this.setStatus('Already dispatched for this exact plan')
        return
      }

      let record = readRecord(key)
      if (!record || record.identity !== identity) {
        record = { identity, idempotencyKey: createIdempotencyKey(), status: 'pending', lastAttemptAt: 0 }
        writeRecord(key, record)
      }
      if (record.status === 'submitted') {
        this.setStatus('Submitted for this exact plan')
        return
      }
      if (record.status === 'blocked') {
        this.setStatus(record.message ?? 'Blocked for this exact plan; a new plan or binding is required')
        return
      }
      const now = Date.now()
      if (record.lastAttemptAt > 0 && !autoSendRetryDue(record, now)) {
        this.setStatus('Confirmation pending; retaining the same idempotency key')
        return
      }

      record = { ...record, lastAttemptAt: now, message: undefined }
      writeRecord(key, record)
      this.setStatus('Confirming current approved plan…')
      try {
        const command = await this.browser.confirm(
          route.projectID,
          route.issueNumber,
          buildConfirmationInput(result, binding, record.idempotencyKey),
        )
        writeRecord(key, { ...record, status: 'submitted', message: `Command ${command.id} created.` })
        this.setStatus(`Command ${command.id.slice(0, 12)} created`)
      } catch (error) {
        if (error instanceof ApiError && error.status === 0) {
          writeRecord(key, { ...record, status: 'pending', message: message(error) })
          this.setStatus('Transport unresolved; retrying the same intent')
        } else {
          writeRecord(key, { ...record, status: 'blocked', message: message(error) })
          this.setStatus(message(error))
        }
      }
    } catch (error) {
      this.setStatus(message(error))
    } finally {
      this.running = false
    }
  }
}

if (typeof document !== 'undefined') {
  const controller = new BrowserAutoSendController()
  controller.start()
  globalThis.addEventListener('pagehide', () => controller.stop(), { once: true })
}
