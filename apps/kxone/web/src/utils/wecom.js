export const isMobileViewport = () => {
  if (typeof window === 'undefined' || typeof navigator === 'undefined') {
    return false
  }
  const narrowViewport = window.matchMedia?.('(max-width: 767px)').matches || window.innerWidth <= 767
  return narrowViewport
}
