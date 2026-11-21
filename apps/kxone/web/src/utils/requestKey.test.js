import { afterEach, describe, expect, it, vi } from 'vitest'
import { createRequestKey } from './requestKey'
afterEach(() => vi.unstubAllGlobals())
describe('secure request identity', () => {
 it('uses secure UUID support when available', () => {
  const randomUUID = vi.fn(() => 'native-uuid')
  vi.stubGlobal('crypto', { randomUUID })
  expect(createRequestKey()).toBe('native-uuid')
 })
 it('creates UUIDv4 without randomUUID and retains secure randomness', () => {
  vi.stubGlobal('crypto', { getRandomValues: bytes => { bytes.fill(255); return bytes } })
  expect(createRequestKey()).toBe('ffffffff-ffff-4fff-bfff-ffffffffffff')
 })
 it('refuses insecure randomness with a readable error', () => {
  vi.stubGlobal('crypto', undefined)
  expect(() => createRequestKey()).toThrow('安全')
 })
})
