const labels = {
  new_lead: '新线索', dispatch: '派发提醒', dispatch_assigned: '派发提醒',
  dispatch_overdue: '待分配超时', claim_overdue: '待领取超时', followup_due: '回访提醒',
  pool_auto_return: '自动回公海', appointment_pending_assignment: '预约登记',
  appointment_cancelled: '预约取消', duplicate_inquiry: '重复留资'
}
export const robotEventLabel = code => code ? labels[code] || '其他通知' : '通知'
