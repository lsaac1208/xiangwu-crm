import { describe, expect, it } from 'vitest'
import { buildLeadProfilePayload, canEditLeadProfile, profileChangeSummary, validateLeadProfilePhone } from './leadProfile'

describe('线索客户资料编辑', () => {
  const invitation = { status: 'following', result_status: 'active', business_stage: 'invitation', current_owner_userid: 'inviter-1' }

  it('应用管理员、当前负责人和调度员权限', () => {
    expect(canEditLeadProfile(invitation, 'manager', 'manager-1')).toBe(true)
    expect(canEditLeadProfile(invitation, 'inviter', 'inviter-1')).toBe(true)
    expect(canEditLeadProfile(invitation, 'inviter', 'inviter-2')).toBe(false)
    expect(canEditLeadProfile(invitation, 'dispatcher', 'dispatcher-1')).toBe(false)
  })

  it('校验电话格式和联系方式保底', () => {
    expect(validateLeadProfilePhone('+86 138-0013-8000', '')).toEqual({ valid: true, normalized: '13800138000' })
    expect(validateLeadProfilePhone('', 'wx-id').valid).toBe(true)
    expect(validateLeadProfilePhone('', '').valid).toBe(false)
    expect(validateLeadProfilePhone('138(0013)8000', '').valid).toBe(false)
  })

  it('生成带双版本号的修改请求', () => {
    expect(buildLeadProfilePayload({ version: 7, profile_version: 2 }, {
      customer_name: ' 王小明 ', customer_mobile: ' 13800138000 ', city: ' 武汉 ', product_name: ' 高考复读 '
    })).toEqual({ version: 7, profile_version: 2, customer_name: '王小明', customer_mobile: '13800138000', city: '武汉', product_name: '高考复读' })
  })

  it('业务过程只展示脱敏电话', () => {
    const summary = profileChangeSummary({ customer_mobile_masked: '139****5678' }, { customer_mobile_masked: '138****8000' })
    expect(summary).toBe('联系电话：139****5678 → 138****8000')
    expect(summary).not.toContain('13800138000')
  })
})
