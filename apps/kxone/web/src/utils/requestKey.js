// Keep one key for an editing intent. Never replace it after an unknown result.
export const createRequestKey = () => {
  const secure = globalThis.crypto
  if (typeof secure?.randomUUID === 'function') return secure.randomUUID()
  if (typeof secure?.getRandomValues !== 'function') {
    throw new Error('当前浏览器不支持安全提交，请保留填写内容后使用支持安全连接的浏览器。')
  }
  const bytes = secure.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
