import { canCorrectLead } from './leadPayment'

const profileFieldLabels = {
  customer_name: '客户姓名',
  customer_mobile_masked: '联系电话',
  city: '城市',
  product_name: '咨询项目',
  source_channel: '来源渠道'
}

export const canEditLeadProfile = (lead, role, currentUserID) => {
  if (!lead || role === 'dispatcher') return false
  if (role === 'admin' || role === 'manager') return true
  if (['invalid', 'converted'].includes(lead.status)) return canCorrectLead(lead, role, currentUserID)
  if (lead.result_status !== 'active' || !['claimed', 'following'].includes(lead.status) || !currentUserID) return false
  if (['sales', 'inviter'].includes(role) && lead.business_stage === 'invitation') {
    return [lead.current_owner_userid, lead.inviter_userid, lead.owner_userid].includes(currentUserID)
  }
  if (role === 'closer' && lead.business_stage === 'negotiation') {
    return [lead.current_owner_userid, lead.closer_userid].includes(currentUserID)
  }
  return false
}

export const validateLeadProfilePhone = (value, existingWechat) => {
  const phone = String(value || '').trim()
  if (!phone) return { valid: !!String(existingWechat || '').trim(), normalized: '' }
  if (!/^[+\d\s-]+$/.test(phone)) return { valid: false, normalized: '' }
  const normalized = phone.replace(/^\+?86/, '').replace(/[\s-]/g, '')
  return { valid: normalized.length >= 6 && normalized.length <= 20, normalized }
}

export const buildLeadProfilePayload = (lead, form) => ({
  version: lead.version,
  profile_version: lead.profile_version,
  customer_name: String(form.customer_name || '').trim(),
  customer_mobile: String(form.customer_mobile || '').trim(),
  city: String(form.city || '').trim(),
  product_name: String(form.product_name || '').trim(),
  ...(form.source_channel !== undefined ? { source_channel: String(form.source_channel || '').trim() } : {})
})

export const profileChangeLabels = fields => (Array.isArray(fields) ? fields : [])
  .map(field => profileFieldLabels[field])
  .filter(Boolean)

export const profileChangeSummary = (before, after) => Object.keys(after || {}).map(field => {
  const label = profileFieldLabels[field] || field
  return `${label}：${before?.[field] || '未填写'} → ${after?.[field] || '未填写'}`
}).join('；')
