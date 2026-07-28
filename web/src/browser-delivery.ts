import { ApiClient, ApiError } from './api.js'
import { BrowserApiClient, BrowserBinding, BrowserWorker } from './browser-api.js'
import {
  buildConfirmationInput,
  createIdempotencyKey,
  deliveryEligibility,
  deliveryRouteIdentity,
  DeliveryRouteIdentity,
  shortIdentity,
  targetLabel,
} from './browser-delivery-model.js'
import {
  DeliveryInspectorActions,
  DeliveryInspectorState,
  DeliveryInspectorView,
  DeliverySnapshot,
  FrozenConfirmation,
} from './browser-delivery-view.js'

export { buildConfirmationInput, deliveryEligibility, deliveryRouteIdentity } from './browser-delivery-model.js'

const REFRESH_INTERVAL_MS = 5_000

function errorText(error: unknown): string {
  if (error instanceof ApiError) return error.status === 0 ? `Backend unavailable: ${error.message}` : `Backend error (${error.status}): ${error.message}`
  return error instanceof Error ? error.message : 'Unknown browser-delivery error'
}

class BrowserDeliveryController {
  private readonly core = new ApiClient()
  private readonly browser = new BrowserApiClient()
  private readonly view: DeliveryInspectorView
  private readonly identity: DeliveryRouteIdentity
  private snapshot: DeliverySnapshot | null = null
  private confirmation: FrozenConfirmation | null = null
  private feedback = ''
  private submitting = false
  private mutating = false
  private loading = false
  private timer: number | undefined
  private loadGeneration = 0
  private loadAbort: AbortController | null = null
  private stopped = false

  constructor(identity: DeliveryRouteIdentity) {
    this.identity = identity
    this.view = new DeliveryInspectorView(() => this.refresh())
  }

  start(): void {
    this.stopped = false
    void this.load(false)
    this.timer = globalThis.setInterval(() => {
      if (!this.confirmation && !this.busy()) void this.load(true)
    }, REFRESH_INTERVAL_MS)
  }

  stop(): void {
    this.stopped = true
    this.loadGeneration += 1
    this.loadAbort?.abort()
    this.loadAbort = null
    if (this.timer !== undefined) globalThis.clearInterval(this.timer)
    this.view.destroy()
  }

  private busy(): boolean {
    return this.submitting || this.mutating
  }

  private state(): DeliveryInspectorState {
    return {
      snapshot: this.snapshot,
      confirmation: this.confirmation,
      feedback: this.feedback,
      busy: this.busy(),
      loading: this.loading,
    }
  }

  private actions(): DeliveryInspectorActions {
    return {
      refresh: () => this.refresh(),
      bind: (worker) => void this.bind(worker),
      disableBinding: () => void this.disableBinding(),
      review: () => this.openConfirmation(),
      confirm: () => void this.confirm(),
      cancelConfirmation: () => {
        if (this.busy()) return
        this.confirmation = null
        this.feedback = ''
        this.render()
      },
    }
  }

  private render(): void {
    if (this.stopped) return
    this.view.render(this.state(), this.actions())
  }

  private refresh(): void {
    if (this.busy()) return
    if (this.confirmation) {
      this.confirmation = null
      this.feedback = 'Confirmation cancelled by refresh. Review the current state again.'
      void this.load(true)
      return
    }
    void this.load(false)
  }

  private async load(silent: boolean): Promise<void> {
    const generation = ++this.loadGeneration
    this.loadAbort?.abort()
    const controller = new AbortController()
    this.loadAbort = controller
    this.loading = !silent && this.snapshot === null
    if (!silent) this.render()

    try {
      const { projectID, issueNumber } = this.identity
      const [workUnit, plan, workers, deliveries] = await Promise.all([
        this.core.workUnitState(projectID, issueNumber, controller.signal),
        this.core.latestPlan(projectID, issueNumber, controller.signal),
        this.browser.workers(controller.signal),
        this.browser.deliveries(projectID, issueNumber, controller.signal),
      ])
      let binding: BrowserBinding | null = null
      if (workUnit.route.lane_key) binding = (await this.browser.browserBinding(projectID, issueNumber, controller.signal)).binding
      if (this.stopped || generation !== this.loadGeneration) return
      this.snapshot = { workUnit, plan, binding, workers, deliveries }
      if (!silent) this.feedback = ''
    } catch (error) {
      if (controller.signal.aborted || this.stopped || generation !== this.loadGeneration) return
      this.feedback = errorText(error)
    } finally {
      if (generation === this.loadGeneration) {
        this.loading = false
        this.loadAbort = null
        this.render()
      }
    }
  }

  private async mutate(action: () => Promise<void>, success: string): Promise<void> {
    if (this.busy()) return
    this.mutating = true
    this.confirmation = null
    this.feedback = ''
    this.render()
    try {
      await action()
      this.feedback = success
    } catch (error) {
      this.feedback = errorText(error)
    } finally {
      this.mutating = false
      await this.load(true)
    }
  }

  private async bind(worker: BrowserWorker): Promise<void> {
    const snapshot = this.snapshot
    if (!snapshot?.workUnit.route.lane_key || !worker.target) return
    const input = {
      expected_lane_key: snapshot.workUnit.route.lane_key,
      expected_binding_version: snapshot.binding?.binding_version,
      worker_id: worker.worker_id,
      target: worker.target,
    }
    await this.mutate(
      () => this.browser.bind(this.identity.projectID, this.identity.issueNumber, input).then(() => undefined),
      'Binding updated. Review the current state before delivery.',
    )
  }

  private async disableBinding(): Promise<void> {
    const binding = this.snapshot?.binding
    const lane = this.snapshot?.workUnit.route.lane_key
    if (!binding || !lane) return
    await this.mutate(
      () => this.browser.disableBinding(this.identity.projectID, this.identity.issueNumber, {
        expected_lane_key: lane,
        expected_binding_version: binding.binding_version,
      }).then(() => undefined),
      'Binding disabled.',
    )
  }

  private openConfirmation(): void {
    if (this.busy()) return
    const snapshot = this.snapshot
    if (!snapshot?.plan?.plan || !snapshot.binding || !deliveryEligibility(snapshot.workUnit, snapshot.plan, snapshot.binding).ready) return

    const textarea = document.querySelector('textarea[aria-label="Prompt text"]') as HTMLTextAreaElement | null
    if (textarea && textarea.value !== snapshot.plan.plan.prompt) {
      this.feedback = 'Browser delivery uses the immutable backend prompt. Reset local edits before confirming, or use Manual Copy for edited text.'
      this.render()
      return
    }

    let idempotencyKey: string
    try {
      idempotencyKey = createIdempotencyKey()
    } catch (error) {
      this.feedback = errorText(error)
      this.render()
      return
    }

    const input = buildConfirmationInput(snapshot.plan, snapshot.binding, idempotencyKey)
    this.confirmation = {
      input,
      planID: input.plan_id,
      summary: snapshot.plan.plan.summary || snapshot.plan.plan.action,
      expectedHead: input.expected_head,
      lane: input.expected_lane_key,
      target: targetLabel(snapshot.binding.target),
      bindingVersion: input.expected_binding_version,
      bindingReadiness: snapshot.binding.readiness,
      prompt: snapshot.plan.plan.prompt,
    }
    this.feedback = ''
    this.render()
  }

  private async confirm(): Promise<void> {
    const frozen = this.confirmation
    if (this.busy() || !frozen) return
    this.submitting = true
    this.feedback = ''
    this.render()
    let refresh = true

    try {
      const command = await this.browser.confirm(this.identity.projectID, this.identity.issueNumber, frozen.input)
      this.feedback = `Delivery ${shortIdentity(command.id)} created with status ${command.status}.`
      this.confirmation = null
    } catch (error) {
      if (error instanceof ApiError && error.status === 0) {
        this.feedback = 'Confirmation transport is unresolved. Retry this same frozen confirmation to reuse its idempotency key, or cancel and review current state again.'
        refresh = false
      } else {
        this.feedback = errorText(error)
        this.confirmation = null
      }
    } finally {
      this.submitting = false
      if (refresh) await this.load(true)
      else this.render()
    }
  }
}

let controller: BrowserDeliveryController | null = null
let activePath = ''

function syncRoute(): void {
  const path = globalThis.location?.pathname ?? ''
  if (path === activePath && controller) return
  activePath = path
  controller?.stop()
  controller = null

  const identity = deliveryRouteIdentity(path)
  if (!identity) return
  controller = new BrowserDeliveryController(identity)
  controller.start()
}

if (typeof document !== 'undefined') {
  const observer = new MutationObserver(() => {
    if (globalThis.location.pathname !== activePath) syncRoute()
  })
  observer.observe(document.body, { childList: true, subtree: true })
  globalThis.addEventListener('popstate', syncRoute)
  queueMicrotask(syncRoute)
}
