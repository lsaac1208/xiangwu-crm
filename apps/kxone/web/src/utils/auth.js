export const loginReturnPath = (value, origin = window.location.origin) => {
  if (typeof value !== 'string' || !value.startsWith('/') || value.includes('\\')) return '/leads'
  if (Array.from(value).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)) return '/leads'
  try {
    const destination = new URL(value, origin)
    if (destination.origin !== origin) return '/leads'
    return destination.pathname + destination.search + destination.hash
  } catch {
    return '/leads'
  }
}

export const logout = async () => {
  const response = await fetch('/api/auth/logout', {
    method: 'POST',
    credentials: 'same-origin'
  })
  if (!response.ok) throw new Error('退出失败，请检查网络后重试')
  window.location.replace('/login')
}
