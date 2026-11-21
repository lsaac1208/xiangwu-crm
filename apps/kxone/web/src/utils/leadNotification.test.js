import { beforeEach, describe, expect, it } from 'vitest'
import { notificationAlert, markQueueFailure, hasQueueFailure, dismissQueueFailure } from './leadNotification'

describe('lead notification feedback', () => {
  beforeEach(() => sessionStorage.clear())
  it('keeps a creation warning across navigation without mixing accounts', () => {
    markQueueFailure(7, 'a')
    expect(hasQueueFailure(7, 'a')).toBe(true)
    expect(hasQueueFailure(7, 'b')).toBe(false)
    dismissQueueFailure(7, 'a')
    expect(hasQueueFailure(7, 'a')).toBe(false)
  })
  it('reports actual delivery failures and never promises retries for exhausted rows', () => {
    const stopped = notificationAlert({ failures: [{ event_type: 'new_lead', will_retry: false }] })
    expect(stopped.title).toBe('群机器人通知未送达')
    expect(stopped.message).toContain('已停止自动重试')
    expect(stopped.dismissible).toBe(false)
    const mixed = notificationAlert({ failures: [{ event_type: 'new_lead', will_retry: true }, { event_type: 'dispatch_assigned', will_retry: false }] })
    expect(mixed.message).toContain('部分通知会重试')
    expect(mixed.message).toContain('新线索、派发提醒')
  })
  it('distinguishes unknown state from healthy and retains queue recovery context', () => {
    expect(notificationAlert({ status: 'unknown' }).title).toBe('通知状态暂不可用')
    expect(notificationAlert({ queueFailed: true }).message).toContain('系统会尝试补发')
    expect(notificationAlert({ failures: [] })).toBeNull()
  })
})
