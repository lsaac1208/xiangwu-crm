import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const viewportWidth = ref(typeof window === 'undefined' ? 1280 : window.innerWidth)
let subscribers = 0
let mediaQuery

const updateViewport = () => {
  viewportWidth.value = window.innerWidth
}

const attach = () => {
  if (typeof window === 'undefined' || subscribers > 0) return
  mediaQuery = typeof window.matchMedia === 'function' ? window.matchMedia('(max-width: 767px)') : null
  window.addEventListener('resize', updateViewport, { passive: true })
  if (mediaQuery?.addEventListener) mediaQuery.addEventListener('change', updateViewport)
  else if (mediaQuery?.addListener) mediaQuery.addListener(updateViewport)
  updateViewport()
}

const detach = () => {
  if (typeof window === 'undefined' || subscribers > 0) return
  window.removeEventListener('resize', updateViewport)
  if (!mediaQuery) return
  if (mediaQuery.removeEventListener) mediaQuery.removeEventListener('change', updateViewport)
  else mediaQuery.removeListener(updateViewport)
  mediaQuery = undefined
}

export const useResponsiveLayout = () => {
  onMounted(() => {
    attach()
    subscribers += 1
  })
  onBeforeUnmount(() => {
    subscribers = Math.max(0, subscribers - 1)
    detach()
  })

  return {
    viewportWidth,
    isMobile: computed(() => viewportWidth.value <= 767),
    isCompactDesktop: computed(() => viewportWidth.value >= 768 && viewportWidth.value <= 1023),
    isDesktop: computed(() => viewportWidth.value >= 768)
  }
}
