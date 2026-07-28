const ENABLED_KEY = 'cddm.browser-delivery.auto-send.enabled'

function manualReviewOpen(): boolean {
  return document.querySelector('.delivery-fields--review') !== null
}

function setPreference(enabled: boolean): void {
  try { globalThis.localStorage?.setItem(ENABLED_KEY, String(enabled)) } catch { /* local preference only */ }
  document.body.classList.toggle('delivery-auto-send-enabled', enabled)
}

function setBoundaryStatus(message: string): void {
  const status = document.querySelector('.delivery-auto-send-state')
  if (status) status.textContent = `Auto-send: ${message}`
}

function synchronizeControl(): void {
  const checkbox = document.querySelector('.delivery-auto-send input[type="checkbox"]') as HTMLInputElement | null
  if (!checkbox) return
  if (checkbox.checked && manualReviewOpen()) {
    checkbox.checked = false
    setPreference(false)
    setBoundaryStatus('Cancel the open manual review before enabling')
    return
  }
  document.body.classList.toggle('delivery-auto-send-enabled', checkbox.checked)
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
  queueMicrotask(synchronizeControl)
  globalThis.addEventListener('pagehide', () => observer.disconnect(), { once: true })
}
