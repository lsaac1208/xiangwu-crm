import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import AppShell from './AppShell.vue'
import { useUserStore } from '@/stores/user'

const mountShell = async role => {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useUserStore()
  store.role = role
  store.userInfo = {
    username: `${role}-account`,
    employee_name: `${role}-name`,
    features: { appointment_enabled: true, pool_enabled: true, workflow_v2_enabled: true }
  }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/leads', component: { template: '<div />' } },
      { path: '/leads/new', meta: { hideMobileNav: true }, component: { template: '<div />' } },
      { path: '/appointments', component: { template: '<div />' } },
      { path: '/public-pool', component: { template: '<div />' } },
      { path: '/stats/sla', component: { template: '<div />' } },
      { path: '/admin/settings', component: { template: '<div />' } },
      { path: '/account/password', component: { template: '<div />' } }
    ]
  })
  await router.push('/leads')
  await router.isReady()
  return mount(AppShell, {
    global: {
      plugins: [pinia, router],
      stubs: {
        'van-icon': { template: '<i />' },
        'van-popup': { template: '<div><slot /></div>' }
      }
    },
    slots: { default: '<section>content</section>' }
  })
}

describe('AppShell role navigation', () => {
  it('shows the complete administrator workspace navigation', async () => {
    const wrapper = await mountShell('admin')
    expect(wrapper.text()).toContain('线索工作台')
    expect(wrapper.text()).toContain('预约排班')
    expect(wrapper.text()).toContain('邀约公海')
    expect(wrapper.text()).toContain('统计看板')
    expect(wrapper.find('a[href="/stats/sla"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('AI 分析')
    expect(wrapper.text()).toContain('系统设置')
    expect(wrapper.text()).toContain('admin-name')
  })

  it('keeps dispatcher navigation inside the allowed lead workflow', async () => {
    const wrapper = await mountShell('dispatcher')
    expect(wrapper.text()).toContain('线索工作台')
    expect(wrapper.text()).toContain('新建线索')
    expect(wrapper.text()).not.toContain('预约排班')
    expect(wrapper.text()).not.toContain('AI 分析')
    expect(wrapper.text()).not.toContain('统计看板')
    expect(wrapper.text()).not.toContain('系统设置')
  })

  it('removes mobile navigation on task routes and restores it on the workspace', async () => {
    const wrapper = await mountShell('admin')
    const router = wrapper.vm.$router
    expect(wrapper.find('.app-mobile-nav').exists()).toBe(true)
    await router.push('/leads/new')
    expect(wrapper.find('.app-mobile-nav').exists()).toBe(false)
    expect(wrapper.classes()).toContain('app-shell--task')
    expect(wrapper.find('.app-sidebar').text()).toContain('系统设置')
    await router.push('/appointments')
    expect(wrapper.find('.app-mobile-nav').exists()).toBe(true)
    expect(wrapper.classes()).not.toContain('app-shell--task')
    wrapper.unmount()
  })
})
