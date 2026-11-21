import { onBeforeUnmount, onMounted, ref } from 'vue'

// A focused editor and a shortened visual viewport identify a phone keyboard.
// Pinch zoom and ordinary window resizing must not move submission controls.
export const useKeyboardViewport = () => {
  const keyboardOpen = ref(false)
  let viewport
  let restingHeight = 0
  let width = 0
  let mounted = false
  const update = () => {
    if (!mounted) return
    const currentWidth = window.innerWidth
    const height = viewport?.height || window.innerHeight
    const editor = document.activeElement?.matches('textarea, input:not([type="checkbox"]):not([type="radio"]):not([type="button"]):not([type="submit"]):not([type="range"]):not([type="color"]), [contenteditable="true"]')
    if (!editor || Math.abs(currentWidth - width) > 100) restingHeight = window.innerHeight
    width = currentWidth
    keyboardOpen.value = currentWidth <= 767 && Boolean(editor) && (!viewport || Math.abs(viewport.scale - 1) < 0.05) && Math.max(restingHeight, window.innerHeight) - height > 150
  }
  const updateAfterFocus = () => queueMicrotask(update)
  onMounted(() => {
    mounted = true
    viewport = window.visualViewport
    restingHeight = window.innerHeight
    width = window.innerWidth
    viewport?.addEventListener('resize', update)
    window.addEventListener('resize', update)
    document.addEventListener('focusin', updateAfterFocus)
    document.addEventListener('focusout', updateAfterFocus)
  })
  onBeforeUnmount(() => {
    mounted = false
    viewport?.removeEventListener('resize', update)
    window.removeEventListener('resize', update)
    document.removeEventListener('focusin', updateAfterFocus)
    document.removeEventListener('focusout', updateAfterFocus)
  })
  return { keyboardOpen }
}
