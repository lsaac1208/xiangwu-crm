import axe from 'axe-core'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CrmStatePanel from './CrmStatePanel.vue'
import CrmTabs from './CrmTabs.vue'

describe('shared component accessibility', () => {
  it('has no automatic axe violations for tabs and error feedback', async () => {
    const wrapper = mount({
      components: { CrmTabs, CrmStatePanel },
      template: `<main><h1>线索详情</h1><CrmTabs model-value="first" :tabs="[{ value: 'first', label: '第一项' }]" id-prefix="axe"><CrmStatePanel state="error" title="加载失败" action-label="重试" /></CrmTabs></main>`
    }, { global: { stubs: { 'van-icon': true, 'van-loading': true } }, attachTo: document.body })
    const result = await axe.run(wrapper.element)
    expect(result.violations).toEqual([])
    wrapper.unmount()
  })
})
