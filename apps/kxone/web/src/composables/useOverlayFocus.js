import { nextTick, onBeforeUnmount, unref, watch } from 'vue'
import { overlayTrigger, rememberOverlayOrigin, markOverlayClosed, restoreOverlayFocus } from '@/utils/overlayTrigger'

const focusableSelector = [
  'button:not([disabled])',
  '[href]',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])'
].join(',')

export const useOverlayFocus = (visible, container, onEscape) => {
  let previousFocus
  const rootElement = () => {
    // Vant Popup renders a fragment; its exposed popupRef is the dialog element.
    const root = unref(container.value?.popupRef) || container.value?.$el || container.value
    return root?.nodeType === 1 ? root : null
  }
  const onKeydown = event => {
    const root = rootElement()
    if (!root) return
    if (event.key === 'Escape') {
      event.preventDefault()
      onEscape?.()
      return
    }
    if (event.key !== 'Tab') return
    const controls = [...root.querySelectorAll(focusableSelector)].filter(item => !item.hidden && !item.disabled && item.getClientRects().length > 0)
    if (!controls.length) return
    const first = controls[0]
    const last = controls[controls.length - 1]
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }

  watch(visible, async open => {
    if (open) {
      previousFocus = overlayTrigger()
      await nextTick()
      rememberOverlayOrigin(rootElement(), previousFocus)
      rootElement()?.addEventListener('keydown', onKeydown)
      rootElement()?.querySelector('[data-autofocus], input, button, [tabindex="0"]')?.focus()
    } else {
      markOverlayClosed(rootElement())
      rootElement()?.removeEventListener('keydown', onKeydown)
      restoreOverlayFocus(previousFocus)
    }
  })

  onBeforeUnmount(() => rootElement()?.removeEventListener('keydown', onKeydown))
}
