import { ApiClient, ApiError } from './api.js'
import { BrowserApiClient, BrowserBinding, BrowserTarget, BrowserWorker, DeliveryCommand, DeliveryConfirmationInput } from './browser-api.js'
import { GenerationResult, WorkUnitState } from './domain.js'

export type DeliveryEligibility = { ready: boolean; reason: string }

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

function routeIdentity(path: string): { projectID: number; issueNumber: number } | null {
  const match = path.match(/^\/projects\/(\d+)\/work-units\/(\d+)(?:\/plans)?\/?$/)
  if (!match) return null
  const projectID = Number(match[1])
  const issueNumber = Number(match[2])
  return projectID > 0 && issueNumber > 0 ? { projectID, issueNumber } : null
}

function short(value: string, length = 12): string { return value.length <= length ? value : `${value.slice(0, length)}…` }
function targetLabel(target: BrowserTarget): string { return `${target.origin}${target.path}` }
function randomKey(): string {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID()
  const bytes = new Uint8Array(16)
  globalThis.crypto?.getRandomValues?.(bytes)
  return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('') || `${Date.now()}-${Math.random()}`
}
function errorText(error: unknown): string {
  if (error instanceof ApiError) return error.status === 0 ? `Backend unavailable: ${error.message}` : `Backend error (${error.status}): ${error.message}`
  return error instanceof Error ? error.message : 'Unknown browser-delivery error'
}
function el<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string, text?: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag)
  if (className) node.className = className
  if (text !== undefined) node.textContent = text
  return node
}
function line(label: string, value: string): HTMLElement {
  const row = el('div', 'cddm-browser-row')
  row.append(el('span', 'cddm-browser-label', label), el('span', 'cddm-browser-value', value))
  return row
}
function button(label: string, className = ''): HTMLButtonElement {
  const node = el('button', `cddm-browser-button ${className}`.trim(), label)
  node.type = 'button'
  return node
}

const style = `
#cddm-browser-delivery{position:fixed;right:16px;bottom:16px;z-index:10000;width:min(460px,calc(100vw - 32px));max-height:82vh;overflow:auto;background:#111827;color:#f8fafc;border:1px solid #334155;border-radius:14px;box-shadow:0 20px 60px rgba(0,0,0,.35);font:14px/1.45 system-ui,sans-serif}
#cddm-browser-delivery *{box-sizing:border-box}.cddm-browser-head{display:flex;justify-content:space-between;gap:12px;align-items:center;padding:14px 16px;border-bottom:1px solid #334155;position:sticky;top:0;background:#111827;z-index:1}.cddm-browser-head h2{font-size:16px;margin:0}.cddm-browser-body{padding:14px 16px;display:grid;gap:14px}.cddm-browser-section{display:grid;gap:8px;padding:12px;border:1px solid #334155;border-radius:10px;background:#0f172a}.cddm-browser-section h3{font-size:14px;margin:0}.cddm-browser-row{display:flex;justify-content:space-between;gap:12px}.cddm-browser-label{color:#94a3b8}.cddm-browser-value{overflow-wrap:anywhere;text-align:right}.cddm-browser-controls{display:flex;flex-wrap:wrap;gap:8px}.cddm-browser-button{border:1px solid #475569;border-radius:8px;padding:7px 10px;background:#1e293b;color:#f8fafc;cursor:pointer}.cddm-browser-button--primary{background:#2563eb;border-color:#2563eb}.cddm-browser-button--danger{background:#7f1d1d;border-color:#991b1b}.cddm-browser-button:disabled{opacity:.45;cursor:not-allowed}.cddm-browser-select{width:100%;padding:8px;border:1px solid #475569;border-radius:8px;background:#0b1220;color:#f8fafc}.cddm-browser-feedback{margin:0;padding:8px 10px;border-radius:8px;background:#172554;color:#dbeafe;overflow-wrap:anywhere}.cddm-browser-warning{background:#422006;color:#fef3c7}.cddm-browser-danger{background:#450a0a;color:#fee2e2}.cddm-browser-history{display:grid;gap:8px}.cddm-browser-command{padding:9px;border:1px solid #334155;border-radius:8px;display:grid;gap:4px}.cddm-browser-status{font-weight:700}.cddm-browser-prompt{white-space:pre-wrap;max-height:180px;overflow:auto;background:#020617;padding:9px;border-radius:8px}.cddm-browser-muted{color:#94a3b8;margin:0}@media(max-width:640px){#cddm-browser-delivery{right:8px;bottom:8px;width:calc(100vw - 16px);max-height:72vh}}
`

type Snapshot = {
  workUnit: WorkUnitState
  plan: GenerationResult | null
  binding: BrowserBinding | null
  workers: BrowserWorker[]
  deliveries: DeliveryCommand[]
}

type FrozenConfirmation = {
  input: DeliveryConfirmationInput
  planID: number
  summary: string
  expectedHead: string
  lane: string
  target: string
  bindingVersion: number
  bindingReadiness: string
  prompt: string
}

class BrowserDeliveryController {
  private readonly core = new ApiClient()
  private readonly browser = new BrowserApiClient()
  private root: HTMLElement
  private snapshot: Snapshot | null = null
  private identity: { projectID: number; issueNumber: number }
  private confirmation: FrozenConfirmation | null = null
  private submitting = false
  private feedback = ''
  private timer: number | undefined

  constructor(root: HTMLElement, identity: { projectID: number; issueNumber: number }) {
    this.root = root
    this.identity = identity
  }

  start(): void {
    void this.load()
    this.timer = globalThis.setInterval(() => { if (!this.confirmation && !this.submitting) void this.load(true) }, 5000)
  }

  stop(): void { if (this.timer !== undefined) globalThis.clearInterval(this.timer) }
  refresh(): void {
    if (this.confirmation) {
      this.confirmation = null
      this.feedback = 'Confirmation cancelled by refresh. Review the current state again.'
      void this.load(true)
      return
    }
    void this.load(false)
  }

  private async load(silent = false): Promise<void> {
    if (!silent) this.renderLoading()
    try {
      const { projectID, issueNumber } = this.identity
      const [workUnit, plan, workers, deliveries] = await Promise.all([
        this.core.workUnitState(projectID, issueNumber), this.core.latestPlan(projectID, issueNumber), this.browser.workers(), this.browser.deliveries(projectID, issueNumber),
      ])
      let binding: BrowserBinding | null = null
      if (workUnit.route.lane_key) binding = (await this.browser.browserBinding(projectID, issueNumber)).binding
      this.snapshot = { workUnit, plan, binding, workers, deliveries }
      this.feedback = silent ? this.feedback : ''
      this.render()
    } catch (error) {
      this.feedback = errorText(error)
      this.render()
    }
  }

  private renderLoading(): void {
    const body = this.body()
    body.replaceChildren(el('p', 'cddm-browser-muted', 'Loading browser delivery state…'))
  }

  private body(): HTMLElement {
    let body = this.root.querySelector('.cddm-browser-body') as HTMLElement | null
    if (!body) { body = el('div', 'cddm-browser-body'); this.root.append(body) }
    return body
  }

  private render(): void {
    const body = this.body()
    body.replaceChildren()
    if (this.feedback) body.append(el('p', 'cddm-browser-feedback', this.feedback))
    if (!this.snapshot) { body.append(el('p', 'cddm-browser-muted', 'Browser delivery state is unavailable.')); return }
    body.append(this.bindingSection(), this.deliverySection(), this.historySection())
  }

  private bindingSection(): HTMLElement {
    const section = el('section', 'cddm-browser-section')
    section.append(el('h3', '', 'Browser binding'))
    const { workUnit, binding, workers } = this.snapshot!
    section.append(line('Lane', workUnit.route.lane_key ?? 'No dispatch lane'))
    if (binding) {
      section.append(line('State', binding.readiness), line('Binding', `${short(binding.binding_id)} · v${binding.binding_version}`), line('Worker', short(binding.worker_id)), line('Target', targetLabel(binding.target)), line('Last seen', binding.last_seen ? new Date(binding.last_seen).toLocaleString() : 'Not live'))
    } else section.append(line('State', 'none'))

    const choices = workers.filter((worker) => worker.state === 'live' && worker.target)
    if (workUnit.route.action === 'dispatch' && workUnit.route.lane_key) {
      const select = el('select', 'cddm-browser-select') as HTMLSelectElement
      select.setAttribute('aria-label', 'Live browser target')
      if (choices.length === 0) select.append(new Option('No live ChatGPT targets', ''))
      choices.forEach((worker, index) => select.append(new Option(`${short(worker.worker_id)} · ${targetLabel(worker.target!)}`, String(index))))
      section.append(select)
      const controls = el('div', 'cddm-browser-controls')
      const bindButton = button(binding ? 'Rebind selected target' : 'Bind selected target', 'cddm-browser-button--primary')
      bindButton.disabled = choices.length === 0 || this.submitting
      bindButton.addEventListener('click', () => {
        const selected = choices[Number(select.value)]
        if (selected?.target) void this.bind(selected)
      })
      controls.append(bindButton)
      if (binding?.enabled) {
        const disable = button('Disable binding', 'cddm-browser-button--danger')
        disable.disabled = this.submitting
        disable.addEventListener('click', () => void this.disableBinding())
        controls.append(disable)
      }
      section.append(controls)
    } else section.append(el('p', 'cddm-browser-muted', 'Current backend route is not browser-dispatchable.'))
    return section
  }

  private async bind(worker: BrowserWorker): Promise<void> {
    const snapshot = this.snapshot
    if (!snapshot?.workUnit.route.lane_key || !worker.target) return
    this.feedback = ''
    try {
      await this.browser.bind(this.identity.projectID, this.identity.issueNumber, {
        expected_lane_key: snapshot.workUnit.route.lane_key,
        expected_binding_version: snapshot.binding?.binding_version,
        worker_id: worker.worker_id,
        target: worker.target,
      })
      this.feedback = 'Binding updated. Review the current state before delivery.'
    } catch (error) { this.feedback = errorText(error) }
    this.confirmation = null
    await this.load(true)
  }

  private async disableBinding(): Promise<void> {
    const binding = this.snapshot?.binding
    const lane = this.snapshot?.workUnit.route.lane_key
    if (!binding || !lane) return
    this.feedback = ''
    try {
      await this.browser.disableBinding(this.identity.projectID, this.identity.issueNumber, { expected_lane_key: lane, expected_binding_version: binding.binding_version })
      this.feedback = 'Binding disabled.'
    } catch (error) { this.feedback = errorText(error) }
    this.confirmation = null
    await this.load(true)
  }

  private deliverySection(): HTMLElement {
    const section = el('section', 'cddm-browser-section')
    section.append(el('h3', '', 'Confirmed delivery'))
    const snapshot = this.snapshot!
    const eligibility = deliveryEligibility(snapshot.workUnit, snapshot.plan, snapshot.binding)
    section.append(line('Eligibility', eligibility.ready ? 'ready' : 'unavailable'))
    if (!eligibility.ready && !this.confirmation) section.append(el('p', 'cddm-browser-feedback cddm-browser-warning', eligibility.reason))

    if (this.confirmation) {
      const frozen = this.confirmation
      section.append(line('Plan', `#${frozen.planID} · ${frozen.summary}`), line('Expected Head', frozen.expectedHead || '—'), line('Lane', frozen.lane), line('Target', frozen.target), line('Binding', `v${frozen.bindingVersion} · ${frozen.bindingReadiness}`))
      section.append(el('p', 'cddm-browser-feedback cddm-browser-warning', 'Final confirmation sends exactly this reviewed backend-generated prompt to the bound ChatGPT conversation. CDDM does not read the ChatGPT response.'))
      section.append(el('pre', 'cddm-browser-prompt', frozen.prompt))
      const controls = el('div', 'cddm-browser-controls')
      const confirm = button(this.submitting ? 'Sending confirmation…' : 'Confirm and send', 'cddm-browser-button--primary')
      confirm.disabled = this.submitting
      confirm.addEventListener('click', () => void this.confirm())
      const cancel = button('Cancel')
      cancel.disabled = this.submitting
      cancel.addEventListener('click', () => { this.confirmation = null; this.feedback = ''; this.render() })
      controls.append(confirm, cancel)
      section.append(controls)
      return section
    }

    const review = button('Review delivery', 'cddm-browser-button--primary')
    review.disabled = !eligibility.ready
    review.addEventListener('click', () => this.openConfirmation())
    section.append(review)
    return section
  }

  private openConfirmation(): void {
    const snapshot = this.snapshot
    if (!snapshot?.plan?.plan || !snapshot.binding || !deliveryEligibility(snapshot.workUnit, snapshot.plan, snapshot.binding).ready) return
    const textarea = document.querySelector('textarea[aria-label="Prompt text"]') as HTMLTextAreaElement | null
    if (textarea && textarea.value !== snapshot.plan.plan.prompt) {
      this.feedback = 'Browser delivery always uses the immutable backend prompt. Reset local prompt edits before confirming, or use Manual Copy for the edited text.'
      this.render()
      return
    }
    const key = randomKey()
    const input = buildConfirmationInput(snapshot.plan, snapshot.binding, key)
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
    if (this.submitting || !frozen) return
    this.submitting = true
    this.feedback = ''
    this.render()
    let refresh = true
    try {
      const command = await this.browser.confirm(this.identity.projectID, this.identity.issueNumber, frozen.input)
      this.feedback = `Delivery ${short(command.id)} created with status ${command.status}.`
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

  private historySection(): HTMLElement {
    const section = el('section', 'cddm-browser-section')
    section.append(el('h3', '', 'Delivery history'))
    const history = el('div', 'cddm-browser-history')
    const deliveries = [...(this.snapshot?.deliveries ?? [])].sort((a, b) => b.created_at.localeCompare(a.created_at))
    if (deliveries.length === 0) history.append(el('p', 'cddm-browser-muted', 'No delivery attempts for this work unit.'))
    deliveries.slice(0, 12).forEach((command) => {
      const card = el('article', 'cddm-browser-command')
      card.append(line('Command', short(command.id)), line('Status', command.status), line('Plan', `#${command.plan_id}`), line('Head', short(command.expected_head || '—')), line('Target', command.target_ref), line('Created', new Date(command.created_at).toLocaleString()))
      if (command.outcome_reason) card.append(el('p', command.status === 'uncertain' ? 'cddm-browser-feedback cddm-browser-warning' : 'cddm-browser-muted', command.outcome_reason))
      if (command.outcome_evidence) card.append(el('p', 'cddm-browser-muted', `Evidence: ${command.outcome_evidence}`))
      if (command.status === 'uncertain') card.append(el('p', 'cddm-browser-muted', 'Outcome is ambiguous. There is no automatic retry; a new attempt requires a fresh explicit confirmation.'))
      history.append(card)
    })
    section.append(history)
    return section
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
  document.getElementById('cddm-browser-delivery')?.remove()
  const identity = routeIdentity(path)
  if (!identity) return

  if (!document.getElementById('cddm-browser-style')) {
    const styleNode = el('style')
    styleNode.id = 'cddm-browser-style'
    styleNode.textContent = style
    document.head.append(styleNode)
  }
  const root = el('aside')
  root.id = 'cddm-browser-delivery'
  root.setAttribute('aria-label', 'Browser Delivery')
  const head = el('div', 'cddm-browser-head')
  head.append(el('h2', '', 'Browser Delivery'))
  const refresh = button('Refresh')
  head.append(refresh)
  root.append(head, el('div', 'cddm-browser-body'))
  document.body.append(root)
  controller = new BrowserDeliveryController(root, identity)
  refresh.addEventListener('click', () => controller?.refresh())
  controller.start()
}

if (typeof document !== 'undefined') {
  const observer = new MutationObserver(() => {
    const next = globalThis.location.pathname
    if (next !== activePath) syncRoute()
  })
  observer.observe(document.body, { childList: true, subtree: true })
  globalThis.addEventListener('popstate', syncRoute)
  queueMicrotask(syncRoute)
}
