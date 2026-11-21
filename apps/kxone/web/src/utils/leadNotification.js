import { robotEventLabel } from './robotEvents'

const key = (lead, account) => `kxone.notify-queue-failed.${account || 'anonymous'}.${lead}`
const maxAge = 24 * 60 * 60 * 1000
export const markQueueFailure = (lead, account) => {
  try { sessionStorage.setItem(key(lead, account), String(Date.now())) } catch { /* optional session feedback */ }
}
export const hasQueueFailure = (lead, account) => {
  try {
    const value = Number(sessionStorage.getItem(key(lead, account)))
    if (value > 0 && Date.now() - value < maxAge) return true
    sessionStorage.removeItem(key(lead, account))
  } catch { /* unavailable storage */ }
  return false
}
export const dismissQueueFailure = (lead, account) => {
  try { sessionStorage.removeItem(key(lead, account)) } catch { /* optional session feedback */ }
}

export const notificationAlert = ({ failures = [], queueFailed = false, status } = {}) => {
  const rows = Array.isArray(failures) ? failures.filter(Boolean) : []
  if (rows.length) {
    const events = [...new Set(rows.map(row => robotEventLabel(row.event_type)))].join('、')
    const retrying = rows.filter(row => row.will_retry).length
    const retry = retrying === 0 ? '已停止自动重试' : retrying === rows.length ? '系统会自动重试' : '部分通知会重试，其余已停止重试'
    return { kind: 'delivery', title: '群机器人通知未送达', message: `${events}未送达。${retry}，如需立即处理请直接联系负责人。`, dismissible: false }
  }
  if (queueFailed) return { kind: 'queue', title: '线索已保存，群提醒需要确认', message: '系统会尝试补发，请核对群消息或直接联系负责人确认收到提醒。', dismissible: true }
  if (status === 'unknown') return { kind: 'unknown', title: '通知状态暂不可用', message: '线索资料可正常处理，请稍后刷新投递状态。', dismissible: false }
  return null
}
