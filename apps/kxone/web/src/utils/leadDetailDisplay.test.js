import { describe, expect, it } from 'vitest'

import { buildLeadWorkflowSteps, detailSourceInfo, leadOwnerEmptyLabel } from './leadDetailDisplay'

describe('lead detail presentation model', () => {
  it('separates source platform, account and campaign', () => {
    expect(detailSourceInfo({ source_channel: 'ad:xiaohongshu:武汉襄武学校', source_campaign: '暑期招生', city: '武汉市' })).toEqual({
      platform: '小红书', account: '武汉襄武学校', campaign: '暑期招生', city: '武汉市'
    })
  })

  it('formats legacy RFC3339 lead times in source details', () => {
    expect(detailSourceInfo({ source_channel: 'form:公众号表单', source_campaign: '高考复读 / 留资时间:2026-07-30T01:12:43.656Z' }).campaign)
      .toBe('高考复读 / 留资时间:2026-07-30 09:12:43')
  })

  it('marks the active dual-stage workflow step', () => {
    const steps = buildLeadWorkflowSteps({ status: 'following', business_stage: 'negotiation' }, { status: 'arrived_delivered' }, true)
    expect(steps.map(item => item.state)).toEqual(['complete', 'complete', 'complete', 'active', 'pending'])
  })

  it('marks appointment cancellations and terminal outcomes', () => {
    const cancelled = buildLeadWorkflowSteps({ status: 'following', business_stage: 'invitation' }, { status: 'no_show' }, true)
    expect(cancelled[2].state).toBe('warning')
    const converted = buildLeadWorkflowSteps({ status: 'converted', business_stage: 'finished' }, null, true)
    expect(converted[4].state).toBe('success')
  })

  it('provides business-friendly empty owner labels', () => {
    expect(leadOwnerEmptyLabel({ status: 'pending_dispatch' })).toBe('尚未分配邀约师')
    expect(leadOwnerEmptyLabel({ status: 'assigned_pending_claim' })).toBe('等待邀约师领取')
  })
})
