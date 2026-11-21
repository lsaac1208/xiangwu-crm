import { describe, expect, it } from 'vitest'

import { displayLeadCloser, displayLeadInviter, displayLeadOwner, displayLeadTitle, displaySourceCampaign, displaySourceCampaignSummary, displaySourceChannel } from './leadDisplay'

describe('lead source display', () => {
  it('labels website submissions as online forms instead of official-account forms', () => {
    expect(displaySourceChannel('form:学校官网')).toBe('在线表单 · 学校官网')
    expect(displayLeadTitle({ source_channel: 'form:学校官网' })).toBe('在线表单线索')
  })

  it('keeps the account name available for identifying an official-account form', () => {
    expect(displaySourceChannel('form:公众号表单')).toBe('在线表单 · 公众号表单')
  })

  it('uses a neutral label when a form has no account name', () => {
    expect(displaySourceChannel('form:')).toBe('在线表单')
  })

  it('does not change advertising source labels', () => {
    expect(displaySourceChannel('ad:tencent:招生广告')).toBe('腾讯广告 · 招生广告')
    expect(displaySourceChannel('ad:douyin:襄五抖音')).toBe('抖音 · 襄五抖音')
    expect(displayLeadTitle({ source_channel: 'ad:douyin:襄五抖音' })).toBe('抖音广告线索')
  })

  it('formats RFC3339 lead times in China timezone', () => {
    expect(displaySourceCampaign('高考复读 / 留资时间:2026-07-30T01:12:43.656Z')).toBe('高考复读 / 留资时间:2026-07-30 09:12:43')
  })

  it('keeps already formatted platform lead times unchanged', () => {
    expect(displaySourceCampaign('留客资 / 留资时间:2026-07-29 17:14:26')).toBe('留客资 / 留资时间:2026-07-29 17:14:26')
  })

  it('extracts the ad group from legacy Tencent campaign details', () => {
    const campaign = '参与赔付 / campaign_v3_1784706608318096198_1109 / 0722艾米-复读 / 0727-0708复读混剪4_play_1785125202042_202607271206260 / 页面:https://h5.gdt.qq.com/xjviewer/nemo/87227586 / 留资时间:2026-07-31 08:23:23'
    expect(displaySourceCampaignSummary('ad:tencent:武汉明壹致行教育科技有限公司', campaign)).toBe('广告组：0722艾米-复读')
  })

  it('extracts the ad group from labeled Tencent campaign details', () => {
    const campaign = '状态:参与赔付 / 计划:campaign_v3_1784706608318096198_1109 / 广告组：0722艾米-复读 / 创意:复读混剪4'
    expect(displaySourceCampaignSummary('ad:tencent:武汉明壹致行教育科技有限公司', campaign)).toBe('广告组：0722艾米-复读')
  })

  it('hides Tencent campaign details when no ad group can be identified safely', () => {
    const campaign = '参与赔付 / 春季招生计划 / 创意_play_1785125202042_202607271206260 / 页面:https://h5.gdt.qq.com/example'
    expect(displaySourceCampaignSummary('ad:tencent:招生广告', campaign)).toBe('')
  })

  it('keeps non-Tencent campaign summaries unchanged', () => {
    const campaign = '百度地图 / 留资时间:2026-07-29 20:50:14'
    expect(displaySourceCampaignSummary('ad:baidu:百度地图', campaign)).toBe(campaign)
  })
})

describe('lead owner display', () => {
  it('uses inviter real name before enterprise WeCom UserID', () => {
    const lead = { business_stage: 'invitation', inviter_name: '占伟', inviter_userid: 'tw' }
    expect(displayLeadInviter(lead)).toBe('占伟')
    expect(displayLeadOwner(lead)).toBe('占伟')
  })

  it('uses closer real name during negotiation', () => {
    const lead = { business_stage: 'negotiation', closer_name: '谈单老师', closer_userid: 'closer01' }
    expect(displayLeadCloser(lead)).toBe('谈单老师')
    expect(displayLeadOwner(lead)).toBe('谈单老师')
  })

  it('falls back to UserID only when no real name is available', () => {
    expect(displayLeadInviter({ inviter_userid: 'tw' })).toBe('tw')
    expect(displayLeadCloser({ closer_userid: 'closer01' })).toBe('closer01')
  })
})
