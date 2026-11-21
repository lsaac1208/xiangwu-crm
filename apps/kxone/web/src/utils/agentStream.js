// SSE records can be split anywhere, including inside UTF-8 Chinese characters.
export const consumeAgentEvents = async (body, signal, onEvent) => {
  if (!body?.getReader) throw new Error('当前浏览器无法读取实时回答，请升级浏览器后重试')
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let terminal = false
  const dispatch = record => {
    const lines = record.split(/\r?\n/)
    const data = lines.filter(line => line.startsWith('data:')).map(line => line.slice(5).trimStart()).join('\n')
    if (!data) return
    const event = JSON.parse(data)
    if (event.type === 'done' || event.type === 'error') terminal = true
    onEvent(event)
  }
  try {
    while (!signal?.aborted) {
      const { value, done } = await reader.read()
      buffer += decoder.decode(value, { stream: !done })
      let boundary
      while ((boundary = /\r?\n\r?\n/.exec(buffer))) {
        dispatch(buffer.slice(0, boundary.index))
        buffer = buffer.slice(boundary.index + boundary[0].length)
      }
      if (done) {
        if (buffer.trim()) dispatch(buffer)
        break
      }
    }
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
  return terminal
}
