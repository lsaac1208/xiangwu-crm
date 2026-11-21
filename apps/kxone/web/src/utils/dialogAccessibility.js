import { setDialogDefaultOptions } from 'vant'
import { overlayTrigger, rememberOverlayOrigin, markOverlayClosed, restoreOverlayFocus } from './overlayTrigger'

// Vant's function dialog is mounted outside the app. Its title is used as an
// element ID, and focus is not trapped or returned, so adapt that shared instance.
export const installDialogAccessibility = () => {
  let trigger
  let root
  const controls = () => [...(root?.querySelectorAll('button:not([disabled]), input:not([disabled]), [href], [tabindex="0"]') || [])]
    .filter(element => element.getClientRects().length > 0)
  const onKeydown = event => {
    const items = controls()
    if (event.key === 'Escape') {
      const cancel = root?.querySelector('.van-dialog__cancel:not([disabled])')
      if (cancel) {
        event.preventDefault()
        event.stopPropagation()
        cancel.click()
      }
    } else if (event.key === 'Tab' && items.length) {
      const first = items[0]
      const last = items.at(-1)
      if (event.shiftKey && (document.activeElement === first || document.activeElement === root)) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === root)) {
        event.preventDefault()
        first.focus()
      }
    }
  }
  setDialogDefaultOptions({
    overlayProps: { 'aria-hidden': true, tabindex: -1, role: 'presentation' },
    onOpen: () => { trigger = overlayTrigger() },
    onOpened: () => {
      root = [...document.querySelectorAll('.van-dialog')].find(element => element.getClientRects().length)
      if (!root) return
      rememberOverlayOrigin(root, trigger)
      const title = root.querySelector('.van-dialog__header')?.textContent?.trim()
      if (title) {
        root.setAttribute('aria-label', title)
        root.removeAttribute('aria-labelledby')
      }
      root.setAttribute('aria-modal', 'true')
      root.addEventListener('keydown', onKeydown, true)
      // Confirmation must stay an explicit action, especially for deletion.
      const initial = root.querySelector('.van-dialog__cancel:not([disabled])') || controls()[0]
      initial?.focus()
    },
    onClose: () => { markOverlayClosed(root) },
    onClosed: () => {
      root?.removeEventListener('keydown', onKeydown, true)
      restoreOverlayFocus(trigger)
      root = null
    }
  })
}
