import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CrmTabs from './CrmTabs.vue'

describe('CrmTabs', () => {
  const tabs = [
    { value: 'followups', label: '跟进记录', count: 2 },
    { value: 'timeline', label: '业务过程', count: 4 },
    { value: 'system', label: '系统信息' }
  ]

  it('exposes complete tab and panel semantics', () => {
    const wrapper = mount(CrmTabs, { props: { modelValue: 'followups', tabs, idPrefix: 'test' } })
    const active = wrapper.get('[role="tab"][aria-selected="true"]')
    expect(active.attributes('aria-controls')).toBe('test-followups-panel')
    expect(wrapper.get('[role="tabpanel"]').attributes('aria-labelledby')).toBe('test-followups')
    expect(wrapper.findAll('[role="tab"]')[1].attributes('tabindex')).toBe('-1')
  })

  it('supports arrow, home and end keyboard navigation', async () => {
    vi.stubGlobal('requestAnimationFrame', callback => callback())
    const wrapper = mount(CrmTabs, { props: { modelValue: 'followups', tabs } })
    await wrapper.get('[role="tablist"]').trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['timeline'])
    await wrapper.get('[role="tablist"]').trigger('keydown', { key: 'End' })
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual(['system'])
    vi.unstubAllGlobals()
  })
})
