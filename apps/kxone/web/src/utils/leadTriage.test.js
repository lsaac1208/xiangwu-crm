import { beforeEach, describe, expect, it } from 'vitest'
import { leadNextStep, preferredLeadView, rememberLeadView, workbenchTaskLabel } from './leadTriage'

describe('lead triage guidance', () => {
  it('describes the next step without claiming another person can act', () => {
    expect(leadNextStep({ status: 'pending_dispatch' })).toBe('建议：分配负责人')
    expect(leadNextStep({ status: 'assigned_pending_claim', assigned_inviter_userid: 'a' }, 'a')).toBe('建议：领取后跟进')
    expect(leadNextStep({ status: 'assigned_pending_claim', assigned_inviter_userid: 'a' }, 'b')).toBe('建议：提醒负责人领取')
    expect(leadNextStep({ status: 'converted', next_followup_due_at: '2020-01-01' })).toBe('')
    expect(leadNextStep({ status: 'invalid' })).toBe('')
    expect(leadNextStep({ status: 'following', current_owner_userid: 'a', next_followup_due_at: '2020-01-01' }, 'b')).toBe('建议：提醒负责人及时跟进')
  })
  it('does not label an active appointment or pool item as an ordinary overdue followup', () => {
    expect(leadNextStep({ status: 'following', current_appointment_id: 8, next_followup_due_at: '2020-01-01' })).toBe('建议：查看预约安排')
    expect(leadNextStep({ status: 'public_pool', next_followup_due_at: '2020-01-01' })).toBe('')
    expect(leadNextStep({ status: 'following', next_followup_due_at: '2020-01-01' })).toBe('建议：联系客户并补写跟进')
  })
  it('makes equal task titles distinguishable by customer or lead number', () => {
    expect(workbenchTaskLabel({ title: '客户回访已逾期', customer_name: '客户甲', lead_no: 'L-1' })).toBe('客户回访已逾期 · 客户甲')
    expect(workbenchTaskLabel({ title: '客户回访已逾期', lead_no: 'L-2' })).toBe('客户回访已逾期 · L-2')
  })
})

describe('preferred lead view', () => {
  beforeEach(() => localStorage.clear())
  it('remembers an explicit full-list preference for only that account', () => {
    expect(preferredLeadView('a')).toBe('needs_action')
    rememberLeadView('a', 'all')
    expect(preferredLeadView('a')).toBe('all')
    expect(preferredLeadView('b')).toBe('needs_action')
    rememberLeadView('a', 'following')
    expect(preferredLeadView('a')).toBe('all')
  })
})
