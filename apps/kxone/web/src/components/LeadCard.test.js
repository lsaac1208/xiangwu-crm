import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LeadCard from './LeadCard.vue'

describe('LeadCard mobile information hierarchy', () => {
  it('renders a flat actionable row with owner and overdue state', async () => {
    const lead = {
      id: 1,
      customer_name: '王小明',
      product_name: '高考复读',
      status: 'following',
      business_stage: 'invitation',
      inviter_name: '员工己',
      next_followup_due_at: '2020-01-01T00:00:00Z',
      created_at: '2026-08-24T08:00:00Z'
    }
    const wrapper = mount(LeadCard, {
      props: { lead },
      global: { stubs: { 'van-icon': { template: '<i />' }, StatusBadge: { template: '<span>status</span>' } } }
    })
    expect(wrapper.get('button').classes()).toContain('overdue')
    expect(wrapper.text()).toContain('王小明')
    expect(wrapper.text()).toContain('高考复读')
    expect(wrapper.text()).toContain('负责：员工己')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('click')?.[0]?.[0]).toEqual(lead)
  })
})
