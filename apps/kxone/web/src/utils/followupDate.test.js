import { describe, expect, it } from 'vitest'

import {
  combineFollowupDateParts,
  datePartsFromDate,
  formatFollowupDateTime,
  timePartsFromDate
} from './followupDate'

describe('follow-up date picker helpers', () => {
  it('round-trips the selected local date and time', () => {
    const source = new Date(2026, 6, 23, 16, 45)
    const combined = combineFollowupDateParts(datePartsFromDate(source), timePartsFromDate(source))

    expect(combined.getFullYear()).toBe(2026)
    expect(combined.getMonth()).toBe(6)
    expect(combined.getDate()).toBe(23)
    expect(combined.getHours()).toBe(16)
    expect(combined.getMinutes()).toBe(45)
  })

  it('formats the value shown in the read-only field', () => {
    expect(formatFollowupDateTime(new Date(2026, 0, 2, 9, 5))).toBe('2026-01-02 09:05')
  })
})
