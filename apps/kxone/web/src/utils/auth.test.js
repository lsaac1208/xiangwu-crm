import { describe, expect, it } from 'vitest'
import { loginReturnPath } from './auth'

describe('login return navigation', () => {
  const origin = 'https://crm.example.test'

  it('keeps the requested local page, filters and fragment after login', () => {
    expect(loginReturnPath('/leads?status=all&keyword=%E5%BC%A0#list', origin))
      .toBe('/leads?status=all&keyword=%E5%BC%A0#list')
    expect(loginReturnPath('/agent/conversation-one', origin)).toBe('/agent/conversation-one')
  })

  it.each([
    undefined, '', 'https://attacker.invalid/', '//attacker.invalid/',
    '/\t/attacker.invalid/phishing', '/\n/attacker.invalid/',
    '/\r/attacker.invalid/', '/\\attacker.invalid/', '/leads\u0000',
    'javascript:alert(1)'
  ])('returns to leads for an unsafe destination: %j', value => {
    expect(loginReturnPath(value, origin)).toBe('/leads')
  })
})
