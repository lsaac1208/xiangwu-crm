import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
const { post, showFailToast } = vi.hoisted(() => ({ post: vi.fn(), showFailToast: vi.fn() }))
vi.mock('axios', () => ({ default: { post, create: () => ({ interceptors: { response: { use: vi.fn() } } }) } }))
vi.mock('vant', () => ({ showFailToast }))
beforeEach(() => { vi.resetModules(); post.mockReset() })
afterEach(() => vi.unstubAllGlobals())
describe('session refresh recovery', () => {
 for (const status of [429, 503, undefined]) {
  it(`can recover after temporary ${status || 'network'} failure`, async () => {
   const { refreshSession } = await import('./index')
   const error = { response: status ? { status } : undefined }
   post.mockRejectedValueOnce(error).mockResolvedValueOnce({ data: {} })
   await expect(refreshSession()).rejects.toBe(error)
   await expect(refreshSession()).resolves.toEqual({ data: {} })
   expect(post).toHaveBeenCalledTimes(2)
  })
 }
 it('shares one refresh for concurrent requests', async () => {
  const { refreshSession } = await import('./index')
  post.mockResolvedValue({ data: {} })
  await Promise.all([refreshSession(), refreshSession(), refreshSession()])
  expect(post).toHaveBeenCalledTimes(1)
 })
 it('does not refresh an authoritatively invalid session again', async () => {
  const { refreshSession } = await import('./index')
  post.mockRejectedValue({ response: { status: 401, data: { error: 'invalid_refresh_token' } } })
  await expect(refreshSession()).rejects.toBeDefined()
  await expect(refreshSession()).rejects.toThrow('session_expired')
  expect(post).toHaveBeenCalledTimes(1)
 })
})
