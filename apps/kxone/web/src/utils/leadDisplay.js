const sourceNames = {
  baidu: '百度广告',
  tencent: '腾讯广告',
  xiaohongshu: '小红书',
  douyin: '抖音',
  form: '在线表单',
  manual: '手动录入'
}

const adDefaultTitles = {
  baidu: '百度广告线索',
  tencent: '腾讯广告线索',
  xiaohongshu: '小红书广告线索',
  douyin: '抖音广告线索',
  form: '在线表单线索',
  manual: '手动录入线索'
}

const emptyValues = new Set(['', '-', '--', '未知客户', '未留姓名', '未知'])

const cleanValue = (value) => {
  const text = String(value || '').trim()
  return emptyValues.has(text) ? '' : text
}

const rfc3339Timestamp = /留资时间:\s*(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))/g

const formatChinaDateTime = (value) => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const parts = new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23'
  }).formatToParts(date)
  const values = Object.fromEntries(parts.map(part => [part.type, part.value]))
  return `${values.year}-${values.month}-${values.day} ${values.hour}:${values.minute}:${values.second}`
}

export const displaySourceCampaign = (value) => {
  return String(value || '').trim().replace(rfc3339Timestamp, (_, timestamp) => `留资时间:${formatChinaDateTime(timestamp)}`)
}

const sourceCampaignSegments = value => displaySourceCampaign(value)
  .split(/\s+\/\s+/)
  .map(segment => segment.trim())
  .filter(Boolean)

const tencentAdgroupFromCampaign = value => {
  const segments = sourceCampaignSegments(value)
  const labeledAdgroup = segments.find(segment => /^广告组\s*[:：]/.test(segment))
  if (labeledAdgroup) return labeledAdgroup.replace(/^广告组\s*[:：]\s*/, '')

  const campaignIndex = segments.findIndex(segment => /^campaign(?:_v\d+)?[_-]/i.test(segment))
  if (campaignIndex >= 0) {
    const legacyAdgroup = segments[campaignIndex + 1] || ''
    const isLabeledField = /^(?:状态|计划|广告组|创意|页面|留资时间)\s*[:：]/.test(legacyAdgroup)
    const looksMachineGenerated = /(?:_play_\d{10,}|_\d{16,}(?:_|$))/i.test(legacyAdgroup)
    if (legacyAdgroup && !isLabeledField && !looksMachineGenerated) return legacyAdgroup
  }
  return ''
}

export const displaySourceCampaignSummary = (sourceChannel, value) => {
  const campaign = displaySourceCampaign(value)
  if (getLeadSourceKind(sourceChannel) !== 'tencent') return campaign

  const adgroup = tencentAdgroupFromCampaign(campaign)
  return adgroup ? `广告组：${adgroup}` : ''
}

export const isMissingCustomerName = (name) => {
  return !cleanValue(name)
}

export const displayCustomerName = (lead) => {
  return isMissingCustomerName(lead?.customer_name) ? '未留姓名' : lead.customer_name
}

export const displayLeadTitle = (lead) => {
  if (!lead) return '-'
  if (isChannelLead(lead)) {
    return adDefaultTitles[getAdPlatform(lead.source_channel)] || '广告线索'
  }
  return cleanValue(lead.customer_name) || cleanValue(lead.product_name) || displaySourceChannel(lead.source_channel) || '线索'
}

export const displayLeadDesc = (lead) => {
  if (!lead) return '-'
  const source = displaySourceChannel(lead.source_channel)
  if (isChannelLead(lead)) {
    return source || cleanValue(lead.customer_name) || cleanValue(lead.lead_no) || '-'
  }
  return cleanValue(lead.product_name) || source || '-'
}

export const displayLeadOwner = (lead) => {
  if (!lead) return ''
  if (lead.business_stage === 'negotiation') return displayLeadCloser(lead)
  return displayLeadInviter(lead)
}

export const displayLeadInviter = (lead) => {
  if (!lead) return ''
  return cleanValue(lead.inviter_name) ||
    cleanValue(lead.assigned_inviter_name) ||
    cleanValue(lead.assigned_sales_name) ||
    cleanValue(lead.current_owner_name) ||
    cleanValue(lead.owner_name) ||
    cleanValue(lead.inviter_userid) ||
    cleanValue(lead.assigned_inviter_userid) ||
    cleanValue(lead.assigned_sales_userid) ||
    cleanValue(lead.current_owner_userid) ||
    cleanValue(lead.owner_userid) ||
    ''
}

export const displayLeadCloser = (lead) => {
  if (!lead) return ''
  return cleanValue(lead.closer_name) ||
    cleanValue(lead.current_owner_name) ||
    cleanValue(lead.owner_name) ||
    cleanValue(lead.closer_userid) ||
    cleanValue(lead.current_owner_userid) ||
    cleanValue(lead.owner_userid) ||
    ''
}

export const displayLeadDispatcher = (lead) => {
  if (!lead) return ''
  return cleanValue(lead.last_dispatch_manager_name) ||
    cleanValue(lead.last_dispatch_manager_userid)
}

export const displaySourceChannel = (sourceChannel) => {
  const value = String(sourceChannel || '').trim()
  if (!value) return '-'
  if (!value.startsWith('ad:') && !value.startsWith('form:') && !value.startsWith('manual:')) return value

  const parts = value.split(':')
  const kind = value.startsWith('ad:') ? parts[1] : parts[0]
  const platform = sourceNames[kind] || kind || '来源'
  const account = value.startsWith('ad:') ? parts.slice(2).join(':') : parts.slice(1).join(':')
  return account ? `${platform} · ${account}` : platform
}

export const isChannelLead = (lead) => {
  return getLeadSourceKind(lead.source_channel) !== ''
}

export const isAdLead = isChannelLead

export const getAdPlatform = (sourceChannel) => {
  return getLeadSourceKind(sourceChannel)
}

const getLeadSourceKind = (sourceChannel) => {
  const value = String(sourceChannel || '')
  if (value.startsWith('ad:')) return value.split(':')[1] || ''
  if (value.startsWith('form:')) return 'form'
  if (value.startsWith('manual:')) return 'manual'
  return ''
}

export const isLeadFinished = lead => ['converted', 'invalid', 'closed'].includes(lead?.status)
export const leadStageLabel = lead => isLeadFinished(lead) ? '已结束' : ({ invitation: '邀约阶段', negotiation: '谈单阶段', finished: '已结束' })[lead?.business_stage] || '邀约阶段'
