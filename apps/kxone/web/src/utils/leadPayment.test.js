import { expect, it } from 'vitest'
import { leadListReturnPath } from './leadPayment'
it('restores approved list and Agent locations without permitting external return URLs', () => {
  expect(leadListReturnPath('/agent/conversation-one')).toBe('/agent/conversation-one')
  expect(leadListReturnPath('/leads?status=all&keyword=张')).toBe('/leads?status=all&keyword=张')
  for (const value of ['https://example.com', '//example.com', '/agent/../../admin/settings', '/agent/one\\evil', '/leads\nheader', '/admin/settings']) expect(leadListReturnPath(value)).toBe('/leads')
})
