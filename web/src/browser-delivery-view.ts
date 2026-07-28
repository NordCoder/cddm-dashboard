import { BrowserBinding, BrowserWorker, DeliveryCommand, DeliveryConfirmationInput } from './browser-api.js'
import { GenerationResult, WorkUnitState } from './domain.js'
import { deliveryEligibility, shortIdentity, targetLabel } from './browser-delivery-model.js'

export type DeliverySnapshot = {
  workUnit: WorkUnitState
  plan: GenerationResult | null
  binding: BrowserBinding | null
  workers: BrowserWorker[]
  deliveries: DeliveryCommand[]
}

export type FrozenConfirmation = {
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

export type DeliveryInspectorState = {
  snapshot: DeliverySnapshot | null
  confirmation: FrozenConfirmation | null
  feedback: string
  busy: boolean
  loading: boolean
}

export type DeliveryInspectorActions = {
  refresh: () => void
  bind: (worker: BrowserWorker) => void
  disableBinding: () => void
  review: () => void
  confirm: () => void
  cancelConfirmation: () => void
}

const OPEN_KEY = 'cddm.browser-delivery.inspector-open'

function node<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string, text?: string): HTMLElementTagNameMap[K] {
  const element = document.createElement(tag)
  if (className) element.className = className
  if (text !== undefined) element.textContent = text
  return element
}

function actionButton(label: string, variant = 'secondary'): HTMLButtonElement {
  const element = node('button', `delivery-action delivery-action--${variant}`, label)
  element.type = 'button'
  return element
}

function field(label: string, value: string, code = false): HTMLElement {
  const row = node('div', 'delivery-field')
  row.append(node('dt', 'delivery-field__label', label), node(code ? 'code' : 'dd', 'delivery-field__value', value))
  return row
}

function sectionHeader(step: string, title: string, copy: string): HTMLElement {
  const header = node('header', 'delivery-section__header')
  header.append(node('span', 'delivery-section__step', step), node('div', '', undefined))
  const copyBox = header.lastElementChild as HTMLElement
  copyBox.append(node('h3', '', title), node('p', 'delivery-section__copy', copy))
  return header
}

function statusChip(value: string): HTMLElement {
  const normalized = value.toLowerCase().replace(/[^a-z0-9_-]/g, '-')
  return node('span', `delivery-status delivery-status--${normalized}`, value.replaceAll('_', ' '))
}

function feedback(text: string, tone: 'info' | 'warning' | 'danger' = 'info'): HTMLElement {
  const element = node('p', `delivery-feedback delivery-feedback--${tone}`, text)
  element.setAttribute('role', tone === 'danger' ? 'alert' : 'status')
  return element
}

function formatDate(value?: string): string {
  if (!value) return 'Not available'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

export class DeliveryInspectorView {
  private readonly root: HTMLElement
  private readonly body: HTMLElement
  private readonly launcher: HTMLButtonElement
  private readonly refreshButton: HTMLButtonElement
  private open: boolean

  constructor(onRefresh: () => void) {
    this.open = this.readOpenState()
    this.root = node('aside', 'delivery-inspector')
    this.root.id = 'cddm-browser-delivery'
    this.root.setAttribute('aria-label', 'Browser delivery inspector')

    const header = node('header', 'delivery-inspector__header')
    const identity = node('div', 'delivery-inspector__identity')
    identity.append(node('span', 'delivery-inspector__eyebrow', 'Execution surface'), node('h2', '', 'Browser delivery'))
    const controls = node('div', 'delivery-inspector__header-actions')
    this.refreshButton = actionButton('Refresh', 'quiet')
    this.refreshButton.addEventListener('click', onRefresh)
    const closeButton = actionButton('Close', 'quiet')
    closeButton.setAttribute('aria-label', 'Close browser delivery inspector')
    closeButton.addEventListener('click', () => this.setOpen(false))
    controls.append(this.refreshButton, closeButton)
    header.append(identity, controls)

    this.body = node('div', 'delivery-inspector__body')
    this.root.append(header, this.body)

    this.launcher = actionButton('Browser delivery', 'primary')
    this.launcher.classList.add('delivery-inspector-launcher')
    this.launcher.setAttribute('aria-controls', this.root.id)
    this.launcher.addEventListener('click', () => this.setOpen(true))

    document.body.append(this.root, this.launcher)
    this.applyOpenState()
  }

  destroy(): void {
    document.body.classList.remove('delivery-inspector-open')
    this.root.remove()
    this.launcher.remove()
  }

  render(state: DeliveryInspectorState, actions: DeliveryInspectorActions): void {
    this.refreshButton.disabled = state.busy || state.loading
    this.body.replaceChildren()

    if (state.feedback) this.body.append(feedback(state.feedback, state.feedback.toLowerCase().includes('error') ? 'danger' : 'info'))
    if (state.loading) {
      const loading = node('div', 'delivery-loading')
      loading.append(node('span', 'delivery-loading__indicator'), node('strong', '', 'Reading delivery authority'), node('p', '', 'Loading plan, route, browser binding and command lifecycle.'))
      this.body.append(loading)
      return
    }
    if (!state.snapshot) {
      this.body.append(feedback('Browser delivery state is unavailable.', 'danger'))
      return
    }

    this.body.append(
      this.connectionSection(state, actions),
      this.confirmationSection(state, actions),
      this.activitySection(state.snapshot.deliveries),
    )
  }

  private connectionSection(state: DeliveryInspectorState, actions: DeliveryInspectorActions): HTMLElement {
    const snapshot = state.snapshot!
    const section = node('section', 'delivery-section')
    section.append(sectionHeader('01', 'Connection', 'Bind one live extension worker to the current backend lane.'))

    const fields = node('dl', 'delivery-fields')
    fields.append(field('Lane', snapshot.workUnit.route.lane_key ?? 'No dispatch lane', true))
    if (snapshot.binding) {
      fields.append(
        field('Binding', `${shortIdentity(snapshot.binding.binding_id)} · v${snapshot.binding.binding_version}`, true),
        field('Worker', shortIdentity(snapshot.binding.worker_id), true),
        field('Target', targetLabel(snapshot.binding.target), true),
        field('Last seen', formatDate(snapshot.binding.last_seen)),
      )
    } else {
      fields.append(field('Binding', 'Not configured'))
    }
    const stateRow = node('div', 'delivery-field')
    stateRow.append(node('dt', 'delivery-field__label', 'Readiness'), statusChip(snapshot.binding?.readiness ?? 'unbound'))
    fields.prepend(stateRow)
    section.append(fields)

    const choices = snapshot.workers.filter((worker) => worker.state === 'live' && worker.target)
    if (snapshot.workUnit.route.action !== 'dispatch' || !snapshot.workUnit.route.lane_key) {
      section.append(feedback('The current backend route is not browser-dispatchable.', 'warning'))
      return section
    }

    const form = node('div', 'delivery-binding-form')
    const select = node('select', 'delivery-select') as HTMLSelectElement
    select.setAttribute('aria-label', 'Live browser target')
    select.disabled = state.busy
    if (choices.length === 0) select.append(new Option('No live ChatGPT targets', ''))
    choices.forEach((worker, index) => select.append(new Option(`${shortIdentity(worker.worker_id)} · ${targetLabel(worker.target!)}`, String(index))))
    form.append(select)

    const actionsRow = node('div', 'delivery-actions')
    const bind = actionButton(snapshot.binding ? 'Rebind target' : 'Bind target', 'primary')
    bind.disabled = choices.length === 0 || state.busy
    bind.addEventListener('click', () => {
      const selected = choices[Number(select.value)]
      if (selected?.target) actions.bind(selected)
    })
    actionsRow.append(bind)
    if (snapshot.binding?.enabled) {
      const disable = actionButton('Disable binding', 'danger')
      disable.disabled = state.busy
      disable.addEventListener('click', actions.disableBinding)
      actionsRow.append(disable)
    }
    form.append(actionsRow)
    section.append(form)
    return section
  }

  private confirmationSection(state: DeliveryInspectorState, actions: DeliveryInspectorActions): HTMLElement {
    const snapshot = state.snapshot!
    const section = node('section', 'delivery-section')
    section.append(sectionHeader('02', 'Confirmation', 'Freeze and confirm one exact backend-generated delivery intent.'))

    const eligibility = deliveryEligibility(snapshot.workUnit, snapshot.plan, snapshot.binding)
    const statusRow = node('div', 'delivery-readiness')
    statusRow.append(node('span', 'delivery-field__label', 'Eligibility'), statusChip(eligibility.ready ? 'ready' : 'unavailable'))
    section.append(statusRow)

    if (state.confirmation) {
      const frozen = state.confirmation
      const fields = node('dl', 'delivery-fields delivery-fields--review')
      fields.append(
        field('Plan', `#${frozen.planID} · ${frozen.summary}`),
        field('Exact Head', frozen.expectedHead || '—', true),
        field('Lane', frozen.lane, true),
        field('Target', frozen.target, true),
        field('Binding', `v${frozen.bindingVersion} · ${frozen.bindingReadiness}`),
      )
      section.append(fields, feedback('Confirming sends exactly this reviewed backend prompt. ChatGPT response content is never read.', 'warning'))
      const prompt = node('pre', 'delivery-prompt', frozen.prompt)
      prompt.setAttribute('aria-label', 'Frozen prompt')
      section.append(prompt)
      const controls = node('div', 'delivery-actions')
      const confirm = actionButton(state.busy ? 'Sending…' : 'Confirm and send', 'primary')
      confirm.disabled = state.busy
      confirm.addEventListener('click', actions.confirm)
      const cancel = actionButton('Cancel review', 'secondary')
      cancel.disabled = state.busy
      cancel.addEventListener('click', actions.cancelConfirmation)
      controls.append(confirm, cancel)
      section.append(controls)
      return section
    }

    if (!eligibility.ready) section.append(feedback(eligibility.reason, 'warning'))
    const review = actionButton('Review delivery', 'primary')
    review.disabled = !eligibility.ready || state.busy
    review.addEventListener('click', actions.review)
    section.append(review)
    return section
  }

  private activitySection(deliveries: DeliveryCommand[]): HTMLElement {
    const section = node('section', 'delivery-section delivery-section--activity')
    section.append(sectionHeader('03', 'Activity', 'Recent command lifecycle for this work unit.'))
    const list = node('div', 'delivery-activity')
    const ordered = [...deliveries].sort((left, right) => right.created_at.localeCompare(left.created_at)).slice(0, 12)
    if (ordered.length === 0) {
      list.append(node('p', 'delivery-empty', 'No delivery attempts for this work unit.'))
    } else {
      ordered.forEach((command) => {
        const card = node('article', 'delivery-command')
        const header = node('header', 'delivery-command__header')
        header.append(node('code', '', shortIdentity(command.id)), statusChip(command.status))
        const fields = node('dl', 'delivery-fields delivery-fields--compact')
        fields.append(
          field('Plan', `#${command.plan_id}`),
          field('Head', shortIdentity(command.expected_head || '—'), true),
          field('Target', command.target_ref, true),
          field('Created', formatDate(command.created_at)),
        )
        card.append(header, fields)
        if (command.outcome_reason) card.append(feedback(command.outcome_reason, command.status === 'uncertain' ? 'warning' : 'info'))
        if (command.outcome_evidence) card.append(node('p', 'delivery-command__evidence', `Evidence: ${command.outcome_evidence}`))
        if (command.status === 'uncertain') card.append(node('p', 'delivery-command__note', 'No automatic retry. A new attempt requires fresh explicit confirmation.'))
        list.append(card)
      })
    }
    section.append(list)
    return section
  }

  private readOpenState(): boolean {
    try { return globalThis.sessionStorage?.getItem(OPEN_KEY) !== 'false' } catch { return true }
  }

  private setOpen(open: boolean): void {
    this.open = open
    try { globalThis.sessionStorage?.setItem(OPEN_KEY, String(open)) } catch { /* presentation preference is non-authoritative */ }
    this.applyOpenState()
  }

  private applyOpenState(): void {
    this.root.hidden = !this.open
    this.launcher.hidden = this.open
    this.launcher.setAttribute('aria-expanded', String(this.open))
    document.body.classList.toggle('delivery-inspector-open', this.open)
  }
}
