import { displaySourceCampaign, displaySourceChannel } from './leadDisplay'

const terminalStatuses = new Set(['converted', 'closed', 'invalid'])

export const detailSourceInfo = lead => {
  const display = displaySourceChannel(lead?.source_channel)
  const [platform, ...account] = display === '-' ? [] : display.split(' · ')
  return {
    platform: platform || '来源未填写',
    account: account.join(' · ') || '',
    campaign: displaySourceCampaign(lead?.source_campaign),
    city: String(lead?.city || '').trim()
  }
}

const step = (key, label, state = 'pending') => ({ key, label, state })

export const buildLeadWorkflowSteps = (lead, appointment, dualStageEnabled = true) => {
  if (!lead) return []
  const terminal = terminalStatuses.has(lead.status) || lead.business_stage === 'finished'
  if (!dualStageEnabled) {
    return [
      step('created', '新线索', 'complete'),
      step('claimed', '领取', ['claimed', 'following', 'converted', 'closed', 'invalid'].includes(lead.status) ? 'complete' : 'active'),
      step('following', '持续跟进', terminal ? 'complete' : (lead.status === 'following' ? 'active' : 'pending')),
      step('result', '业务结果', terminal ? (lead.status === 'converted' ? 'success' : 'warning') : 'pending')
    ]
  }

  const appointmentStatus = appointment?.status || ''
  const appointmentWarning = ['cancelled', 'no_show'].includes(appointmentStatus)
  const appointmentComplete = lead.business_stage === 'negotiation' || terminal || appointmentStatus === 'arrived_delivered' || !!lead.delivered_at
  const invitationComplete = lead.business_stage === 'negotiation' || terminal || appointmentComplete
  return [
    step('created', '新线索', 'complete'),
    step('invitation', '邀约跟进', invitationComplete ? 'complete' : 'active'),
    step('appointment', '预约到访', appointmentWarning ? 'warning' : (appointmentComplete ? 'complete' : (appointment ? 'active' : 'pending'))),
    step('negotiation', '谈单跟进', terminal ? 'complete' : (lead.business_stage === 'negotiation' ? 'active' : 'pending')),
    step('result', '业务结果', terminal ? (lead.status === 'converted' ? 'success' : 'warning') : 'pending')
  ]
}

export const leadOwnerEmptyLabel = lead => {
  if (lead?.status === 'pending_dispatch') return '尚未分配邀约师'
  if (lead?.status === 'assigned_pending_claim') return '等待邀约师领取'
  if (lead?.business_stage === 'negotiation') return '尚未交付谈单师'
  return '负责人待确认'
}
