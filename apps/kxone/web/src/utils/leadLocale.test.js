import { describe, expect, it } from 'vitest'

import { followupResultLabel, localizeSystemRemark, operationSourceLabel } from './leadLocale'

describe('lead detail Chinese labels', () => {
  it('maps follow-up result codes to Chinese', () => {
    expect(followupResultLabel('interested')).toBe('有意向')
    expect(followupResultLabel('no_answer')).toBe('未接通')
  })

  it('maps operation source codes to Chinese', () => {
    expect(operationSourceLabel('wecom_card')).toBe('企业微信卡片')
    expect(operationSourceLabel('h5')).toBe('CRM 页面')
  })

  it('localizes source codes embedded in system remarks', () => {
    expect(localizeSystemRemark('销售领取线索（wecom_card）')).toBe('销售领取线索（企业微信卡片）')
  })

  it('keeps unknown future values visible for diagnostics', () => {
    expect(followupResultLabel('future_result')).toBe('future_result')
  })
})
