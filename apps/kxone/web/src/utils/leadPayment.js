export const paymentStatusLabel = status => ({ partial: '部分付款', paid: '已付全款', refunded: '已退款' })[status] || '付款状态待确认'

export const correctionStage = lead => lead?.current_owner_role === 'closer' || lead?.delivered_at || lead?.business_stage === 'negotiation' ? 'negotiation' : 'invitation'

export const canCorrectLead = (lead, role, userID) => {
  if (!lead || !userID) return false
  if (['admin', 'manager'].includes(role)) return true
  if (correctionStage(lead) === 'negotiation') return role === 'closer' && [lead.current_owner_userid, lead.closer_userid].includes(userID)
  return ['sales', 'inviter'].includes(role) && [lead.current_owner_userid, lead.inviter_userid, lead.owner_userid].includes(userID)
}

export const validMoney = value => /^(?:0|[1-9]\d*)(?:\.\d{1,2})?$/.test(String(value)) && Number(value) <= 999999999.99

export const leadListReturnPath = value => typeof value === 'string' && /^\/(?:leads|agent(?:\/[A-Za-z0-9_-]+)?)(?:\?|$)/.test(value) && !/[\\\r\n#]/.test(value) ? value : '/leads'
