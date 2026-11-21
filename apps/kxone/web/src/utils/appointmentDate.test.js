import { describe, expect, it } from 'vitest'
import { localDateKey, localDateTime, businessISO } from './appointmentDate'
describe('Shanghai appointment dates independent of browser timezone', () => {
  it('uses the next Shanghai day for UTC evening', () => {
    expect(localDateKey('2026-07-24T18:30:00Z')).toBe('2026-07-25')
    expect(localDateTime('2026-07-24T18:30:00Z')).toBe('2026-07-25T02:30')
    expect(businessISO('2026-07-25T10:30')).toBe('2026-07-25T02:30:00.000Z')
  })
})
