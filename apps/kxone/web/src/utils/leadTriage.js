const preferenceKey = account => `kxone.leads.preferred-view.${account || 'anonymous'}`

export const preferredLeadView = account => {
  try { return localStorage.getItem(preferenceKey(account)) === 'all' ? 'all' : 'needs_action' } catch { return 'needs_action' }
}

export const rememberLeadView = (account, view) => {
  if (!['all', 'needs_action'].includes(view)) return
  try { localStorage.setItem(preferenceKey(account), view) } catch { /* optional preference */ }
}

export const leadNextStep = (lead, actor = '', now = Date.now()) => {
  if (!lead || ['converted', 'closed', 'invalid', 'public_pool'].includes(lead.status)) return ''
  if (lead.current_appointment_id) return '建议：查看预约安排'
  if (lead.status === 'pending_dispatch') return '建议：分配负责人'
  if (lead.status === 'assigned_pending_claim') return (lead.assigned_inviter_userid || lead.assigned_sales_userid) === actor ? '建议：领取后跟进' : '建议：提醒负责人领取'
  if (['claimed', 'following'].includes(lead.status)) {
    const due = Date.parse(lead.next_followup_due_at)
    const owner = lead.current_owner_userid || lead.owner_userid || lead.inviter_userid
    if (actor && owner && actor !== owner) return Number.isFinite(due) && due < now ? '建议：提醒负责人及时跟进' : '建议：关注负责人回访安排'
    return Number.isFinite(due) && due < now ? '建议：联系客户并补写跟进' : '建议：记录联系结果和下次回访'
  }
  return ''
}

export const workbenchTaskLabel = task => {
  const who = task.customer_name || task.lead_no || (task.lead_id ? `线索 ${task.lead_id}` : '')
  const title = task.title || '查看待办'
  return who ? `${title} · ${who}` : title
}
