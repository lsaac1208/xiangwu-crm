import { describe, expect, it } from 'vitest'

import { displayLeadMobile, displayLeadWechat } from './leadContact'

describe('lead contact display', () => {
  it('always derives a safe dispatcher display from raw contacts when present', () => {
    const lead = {
      customer_mobile: '13812345678',
      customer_mobile_masked: '138****5678',
      customer_wechat: 'private-wechat',
      customer_wechat_masked: 'p**********at'
    }

    expect(displayLeadMobile(lead, true)).toBe('138****5678')
    expect(displayLeadWechat(lead, true)).toBe('pr********at')
  })

  it('masks raw values defensively when the server mask is absent', () => {
    expect(displayLeadMobile({ customer_mobile: '13812345678' }, true)).toBe('138****5678')
    expect(displayLeadWechat({ customer_wechat: 'wechat123' }, true)).toBe('we*****23')
  })

  it('does not trust an unmasked server field or expose short contacts', () => {
    expect(displayLeadMobile({ customer_mobile_masked: '13812345678' }, true)).toBe('138****5678')
    expect(displayLeadWechat({ customer_wechat_masked: 'wechat123' }, true)).toBe('we*****23')
    expect(displayLeadMobile({ customer_mobile_masked: '1380*5678' }, true)).toBe('138****5678')
    expect(displayLeadMobile({ customer_mobile_masked: '12345' }, true)).toBe('*****')
    expect(displayLeadWechat({ customer_wechat_masked: 'wx' }, true)).toBe('**')
  })

  it('keeps full contacts for roles that are allowed to see them', () => {
    expect(displayLeadMobile({ customer_mobile: '13812345678' })).toBe('13812345678')
    expect(displayLeadWechat({ customer_wechat: 'wechat123' })).toBe('wechat123')
  })
})
