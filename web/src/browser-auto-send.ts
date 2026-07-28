import { ApiClient, ApiError } from './api.js'
import { BrowserApiClient } from './browser-api.js'
import {
  automaticDeliveryIdentity,
  automaticDeliveryRoute,
  autoSendRetryDue,
  AutoSendRecord,
  matchingDeliveryExists,
  readAutoSendEnabled,
  writeAutoSendEnabled,
} from './browser-auto-send-model.js'
import {
  buildConfirmationInput,
  createIdempotencyKey,
  deliveryEligibility,
} from './browser-delivery-model.js'
import { GenerationResult } from './domain.js'

const RECORD_PREFIX = 'cddm.browser-delivery.auto-send.record'
const POLL_INTERVAL_MS = 5_000

const memoryRecords = new Map<string, AutoSendRecord>()

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

    const route = automaticDeliveryRoute(globalThis.location.pathname)
    const label = document.createElement('label')
    label.className = 'delivery-auto-send'
    label.title = route
      ? 'Automatically send each new backend-approved immutable prompt for this work unit when its browser binding is ready.'
      : 'Auto-send is unavailable while viewing a historical Prompt Plan.'
    const checkbox = document.createElement('input')
    checkbox.type = 'checkbox'
    checkbox.disabled = route === null
    checkbox.checked = route !== null && readAutoSendEnabled(globalThis.location.pathname)
    checkbox.setAttribute('aria-label', 'Automatically send approved prompts for this work unit without review')
    const copy = document.createElement('span')
    copy.textContent = 'Auto-send'
    label.append(checkbox, copy)
    headerActions.prepend(label)

    const status = document.createElement('span')
    status.className = 'delivery-auto-send-state'
    status.setAttribute('role', 'status')
    identity.append(status)

    checkbox.addEventListener('change', () => {
      if (!writeAutoSendEnabled(globalThis.location.pathname, checkbox.checked)) {
        checkbox.checked = false
        this.setStatus('Unavailable on this route')
        return
      }
      this.setStatus(checkbox.checked ? 'Enabled for this work unit; waiting for a new approved plan.' : 'Off')
      if (checkbox.checked) void this.tick()
    })
    this.checkbox = checkbox
    this.status = status
    this.setStatus(route ? (checkbox.checked ? 'Enabled for this work unit' : 'Off') : 'Unavailable on historical plan view')
  }

  private setStatus(value: string): void {
    this.ensureControls()
    if (this.status) this.status.textContent = `Auto-send: ${value}`
  }

  private async tick(): Promise<void> {
    this.ensureControls()
    const path = globalThis.location.pathname
    const route = automaticDeliveryRoute(path)
    if (this.stopped || this.running || !route || !readAutoSendEnabled(path)) {
      if (!route) this.setStatus('Unavailable on historical plan view')
      else if (!readAutoSendEnabled(path)) this.setStatus('Off')
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
