export const formatDate = (dateStr) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  const now = new Date()
  const diff = now - date
  
  const minutes = Math.floor(diff / 60000)
  const hours = Math.floor(diff / 3600000)
  const days = Math.floor(diff / 86400000)

  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes}分钟前`
  if (hours < 24) return `${hours}小时前`
  if (days < 7) return `${days}天前`
  
  return date.toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  })
}

export const formatDateTime = (dateStr) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

const pad = value => String(value).padStart(2, '0')

const shanghaiDateFormatter = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit'
})

export const formatBusinessDate = (value = new Date()) => {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const parts = Object.fromEntries(
    shanghaiDateFormatter.formatToParts(date)
      .filter(part => part.type !== 'literal')
      .map(part => [part.type, part.value])
  )
  return `${parts.year}-${parts.month}-${parts.day}`
}

export const formatCalendarDate = value => {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

export const shiftISODate = (dateString, days) => {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(dateString || ''))
  if (!match) return ''
  const date = new Date(Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3])))
  date.setUTCDate(date.getUTCDate() + days)
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}`
}

export const formatCompactDateTime = (dateStr) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  if (Number.isNaN(date.getTime())) return '-'
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

const formatClock = date => `${pad(date.getHours())}:${pad(date.getMinutes())}`

const formatElapsed = milliseconds => {
  const minutes = Math.max(1, Math.floor(milliseconds / 60000))
  if (minutes < 60) return `${minutes}分钟`
  const hours = Math.max(1, Math.floor(minutes / 60))
  if (hours < 24) return `${hours}小时`
  return `${Math.max(1, Math.floor(hours / 24))}天`
}

export const describeFollowupSchedule = (dateStr, now = new Date()) => {
  if (!dateStr) {
    return { label: '暂未安排', exact: '', tone: 'muted', overdue: false }
  }
  const target = new Date(dateStr)
  if (Number.isNaN(target.getTime())) {
    return { label: '暂未安排', exact: '', tone: 'muted', overdue: false }
  }

  const delta = target.getTime() - now.getTime()
  const exact = formatCompactDateTime(target)
  if (delta < 0) {
    return { label: `逾期 ${formatElapsed(Math.abs(delta))}`, exact, tone: 'danger', overdue: true }
  }

  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const targetDay = new Date(target.getFullYear(), target.getMonth(), target.getDate())
  const calendarDays = Math.round((targetDay.getTime() - today.getTime()) / 86400000)
  if (calendarDays === 0) {
    return { label: `今天 ${formatClock(target)}`, exact, tone: 'warning', overdue: false }
  }
  if (calendarDays === 1) {
    return { label: `明天 ${formatClock(target)}`, exact, tone: delta <= 86400000 ? 'warning' : 'normal', overdue: false }
  }
  return {
    label: `${pad(target.getMonth() + 1)}月${pad(target.getDate())}日 ${formatClock(target)}`,
    exact,
    tone: delta <= 86400000 ? 'warning' : 'normal',
    overdue: false
  }
}
