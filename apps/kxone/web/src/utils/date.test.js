import { describe, expect, it } from 'vitest'

import {
  describeFollowupSchedule,
  formatBusinessDate,
  formatCalendarDate,
  formatCompactDateTime,
  shiftISODate
} from './date'

describe('desktop lead list date display', () => {
  const now = new Date(2026, 6, 29, 10, 0)

  it('shows missing and overdue follow-up schedules clearly', () => {
    expect(describeFollowupSchedule('', now)).toEqual({
      label: '暂未安排', exact: '', tone: 'muted', overdue: false
    })
    expect(describeFollowupSchedule(new Date(2026, 6, 29, 7, 30), now)).toMatchObject({
      label: '逾期 2小时', tone: 'danger', overdue: true
    })
  })

  it('uses action-oriented labels for today and tomorrow', () => {
    expect(describeFollowupSchedule(new Date(2026, 6, 29, 15, 30), now)).toMatchObject({
      label: '今天 15:30', tone: 'warning'
    })
    expect(describeFollowupSchedule(new Date(2026, 6, 30, 9, 0), now)).toMatchObject({
      label: '明天 09:00', tone: 'warning'
    })
  })

  it('formats exact timestamps without locale-dependent punctuation', () => {
    expect(formatCompactDateTime(new Date(2026, 0, 2, 9, 5))).toBe('2026-01-02 09:05')
  })
})

describe('CRM business date ranges', () => {
  it('formats instants as Asia/Shanghai calendar dates independent of UTC', () => {
    expect(formatBusinessDate(new Date('2026-07-31T15:59:59Z'))).toBe('2026-07-31')
    expect(formatBusinessDate(new Date('2026-07-31T16:00:00Z'))).toBe('2026-08-01')
  })

  it('shifts ISO calendar dates across month boundaries', () => {
    expect(shiftISODate('2026-08-01', -6)).toBe('2026-07-26')
    expect(shiftISODate('2026-03-01', -1)).toBe('2026-02-28')
  })

  it('keeps a calendar picker selection as a date-only value', () => {
    expect(formatCalendarDate(new Date(2026, 6, 31))).toBe('2026-07-31')
  })
})
