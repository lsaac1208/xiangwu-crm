import { describe, expect, it } from 'vitest'
import { consumeAgentEvents } from './agentStream'

const bodyFromChunks = text => {
  const encoded = new TextEncoder().encode(text)
  let offset = 0
  return { getReader: () => ({
    read: async () => offset < encoded.length ? { value: encoded.slice(offset, ++offset), done: false } : { done: true },
    cancel: async () => {}, releaseLock: () => {}
  }) }
}
describe('Agent event stream', () => {
  it('handles heartbeats, CRLF and UTF-8 split across network chunks', async () => {
    const events = []
    const terminal = await consumeAgentEvents(bodyFromChunks(': heartbeat\r\n\r\nevent: update\r\nid: 1\r\ndata: {"seq":1,"type":"delta","data":{"text":"查询襄阳数据"}}\r\n\r\ndata: {"seq":2,"type":"done","data":{}}\n\n'), new AbortController().signal, event => events.push(event))
    expect(events.map(event => event.type)).toEqual(['delta', 'done'])
    expect(events[0].data.text).toBe('查询襄阳数据')
    expect(terminal).toBe(true)
  })
  it('reports an unexpected EOF as recoverable rather than complete', async () => {
    const events = []
    expect(await consumeAgentEvents(bodyFromChunks('data: {"seq":4,"type":"status","data":{"text":"查询中"}}\n\n'), undefined, event => events.push(event))).toBe(false)
    expect(events).toHaveLength(1)
  })
})
