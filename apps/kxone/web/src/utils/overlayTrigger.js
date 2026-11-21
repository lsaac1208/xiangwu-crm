let lastTrigger
let lastActivation = 0
const origins = new WeakMap()

// Safari does not focus buttons after a pointer click. Remember the actual
// opener before its click handler mounts a dialog, including keyboard clicks.
if (typeof document !== 'undefined') {
  document.addEventListener('click', event => {
    lastTrigger = event.target?.closest?.('button, a[href], input, select, textarea, [tabindex], [role="button"]')
    lastActivation = performance.now()
  }, true)
}

export const overlayTrigger = () => {
  if (lastTrigger?.isConnected && lastTrigger.getClientRects().length && performance.now() - lastActivation < 2000) return lastTrigger
  return document.activeElement
}

export const rememberOverlayOrigin = (root, trigger) => {
  if (root && trigger) origins.set(root, { trigger, open: true })
}

export const markOverlayClosed = root => {
  const origin = origins.get(root)
  if (origin) origin.open = false
}

export const restoreOverlayFocus = trigger => {
  const visited = new Set()
  while (trigger && !visited.has(trigger)) {
    visited.add(trigger)
    const parent = origins.get(trigger.closest?.('.van-popup'))
    if (parent?.open !== false && trigger.isConnected && trigger.getClientRects().length && !trigger.disabled) {
      trigger.focus?.()
      if (document.activeElement === trigger) return
    }
    // A sheet may close while opening the next dialog. Its option is then
    // hidden: return through the sheet's opener instead of focusing the body.
    trigger = parent?.trigger
  }
}
