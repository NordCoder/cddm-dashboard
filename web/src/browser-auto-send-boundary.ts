import {
  automaticDeliveryRoute,
  readAutoSendEnabled,
  writeAutoSendEnabled,
} from './browser-auto-send-model.js'

function manualReviewOpen(): boolean {
  return document.querySelector('.delivery-fields--review') !== null
}

function setPreference(enabled: boolean): void {
  const path = globalThis.location.pathname
  const stored = writeAutoSendEnabled(path, enabled)
  document.body.classList.toggle('delivery-auto-send-enabled', stored && enabled)
}

function setBoundaryStatus(message: string): void {
  const status = document.querySelector('.delivery-auto-send-state')
  if (status) status.textContent = `Auto-send: ${message}`
}

function synchronizeControl(): void {
  const route = automaticDeliveryRoute(globalThis.location.pathname)
  const checkbox = document.querySelector('.delivery-auto-send input[type="checkbox"]') as HTMLInputElement | null
  if (!route || !checkbox) {
    document.body.classList.remove('delivery-auto-send-enabled')
    return
  }
  if (checkbox.checked && manualReviewOpen()) {
    checkbox.checked = false
    setPreference(false)
    setBoundaryStatus('Cancel the open manual review before enabling')
    return
  }
  const enabled = readAutoSendEnabled(globalThis.location.pathname)
  checkbox.checked = enabled
  document.body.classList.toggle('delivery-auto-send-enabled', enabled)
}

if (typeof document !== 'undefined') {
  document.addEventListener('change', (event) => {
    const checkbox = event.target
    if (!(checkbox instanceof HTMLInputElement) || !checkbox.matches('.delivery-auto-send input[type="checkbox"]')) return
    if (checkbox.checked && manualReviewOpen()) {
      checkbox.checked = false
      setPreference(false)
      setBoundaryStatus('Cancel the open manual review before enabling')
      event.stopImmediatePropagation()
      return
    }
    setPreference(checkbox.checked)
  }, true)

  const observer = new MutationObserver(synchronizeControl)
  observer.observe(document.body, { childList: true, subtree: true })
  globalThis.addEventListener('popstate', synchronizeControl)
  queueMicrotask(synchronizeControl)
  globalThis.addEventListener('pagehide', () => {
    observer.disconnect()
    globalThis.removeEventListener('popstate', synchronizeControl)
  }, { once: true })
}
