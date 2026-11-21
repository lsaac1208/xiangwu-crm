import { afterEach, expect, it, vi } from 'vitest'
import axios from 'axios'

vi.mock('vant', () => ({ showFailToast: vi.fn() }))
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

it('does not refresh again for a late 401 after refresh failure, and permits a new login session', async () => {
  vi.resetModules()
  const assign = vi.fn()
  vi.stubGlobal('window', { location: { pathname: '/leads', search: '?status=all', hash: '', assign } })
  const refresh = vi.spyOn(axios, 'post').mockRejectedValue(Object.assign(new Error('revoked'), { response: { status: 401, data: { error: 'invalid_refresh_token' } } }))
  const { default: api } = await import('./index.js')
  api.defaults.adapter = config => {
    if (config.url === '/auth/login') return Promise.resolve({ config, status: 200, data: { code: 0, data: {} } })
    return Promise.reject({ config, response: { status: 401 } })
  }
  await Promise.allSettled([api.get('/leads'), api.get('/workbench')])
  // This request finishes after the failed shared promise has been cleared.
  await expect(api.get('/preferences')).rejects.toBeDefined()
  expect(refresh).toHaveBeenCalledTimes(1)
  expect(assign).toHaveBeenCalledWith('/login?reason=session_expired&redirect=%2Fleads%3Fstatus%3Dall')
  await api.post('/auth/login', {})
  await expect(api.get('/leads')).rejects.toBeDefined()
  expect(refresh).toHaveBeenCalledTimes(2)
})

it('shares one refresh between an API request and an Agent stream', async () => {
  vi.resetModules()
  vi.stubGlobal('window', { location: { pathname: '/agent/conversation-one', search: '', hash: '', assign: vi.fn() } })
  let finishRefresh
  const refresh = vi.spyOn(axios, 'post').mockImplementation(() => new Promise(resolve => { finishRefresh = resolve }))
  const fetch = vi.fn().mockResolvedValueOnce({ status: 401, ok: false }).mockResolvedValueOnce({ status: 200, ok: true })
  vi.stubGlobal('fetch', fetch)
  const { default: api, fetchWithSession } = await import('./index.js')
  let calls = 0
  api.defaults.adapter = config => ++calls === 1 ? Promise.reject({ config, response: { status: 401 } }) : Promise.resolve({ config, status: 200, data: { ok: true } })
  const requests = [api.get('/agent/settings'), fetchWithSession('/api/agent/runs/run-one/events')]
  await vi.waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
  finishRefresh({ status: 200 })
  await Promise.all(requests)
  expect(refresh).toHaveBeenCalledTimes(1)
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(fetch.mock.calls[0][1].credentials).toBe('same-origin')
})

it('retries a stream authentication failure once and retains the conversation return address', async () => {
  vi.resetModules()
  const assign = vi.fn()
  vi.stubGlobal('window', { location: { pathname: '/agent/conversation-one', search: '', hash: '', assign } })
  const refresh = vi.spyOn(axios, 'post').mockResolvedValue({ status: 200 })
  const fetch = vi.fn().mockResolvedValue({ status: 401, ok: false, json: async () => ({ message: '会话已失效' }) })
  vi.stubGlobal('fetch', fetch)
  const { fetchWithSession } = await import('./index.js')
  await expect(fetchWithSession('/api/agent/runs/run-one/events')).rejects.toThrow('会话已失效')
  expect(refresh).toHaveBeenCalledTimes(1)
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(assign).toHaveBeenCalledWith('/login?reason=session_expired&redirect=%2Fagent%2Fconversation-one')
})
