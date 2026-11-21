import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'

import { useUserStore } from './user'

describe('public pool feature display', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('uses sales terminology when the single-chain workflow is active', () => {
    const store = useUserStore()
    store.userInfo = { features: { workflow_v2_enabled: false, pool_enabled: true, pool_auto_return_enabled: true } }

    expect(store.poolTitle()).toBe('销售公海')
    expect(store.isPoolEnabled()).toBe(true)
    expect(store.isPoolAutoReturnEnabled()).toBe(true)
  })

  it('uses invitation terminology without coupling pool availability to workflow V2', () => {
    const store = useUserStore()
    store.userInfo = { features: { workflow_v2_enabled: true, pool_enabled: false, pool_auto_return_enabled: false } }

    expect(store.poolTitle()).toBe('邀约公海')
    expect(store.isPoolEnabled()).toBe(false)
    expect(store.isPoolAutoReturnEnabled()).toBe(false)
  })
})

describe('dispatcher capabilities', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('can create and initially assign leads without inheriting manager access', () => {
    const store = useUserStore()
    store.role = 'dispatcher'

    expect(store.isDispatcher()).toBe(true)
    expect(store.canCreateLead()).toBe(true)
    expect(store.canAssignLead()).toBe(true)
    expect(store.isManager()).toBe(false)
    expect(store.isAdmin()).toBe(false)
    expect(store.isInviter()).toBe(false)
  })
})

describe('current account identity', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('uses the employee name and exposes account and localized role labels', () => {
    const store = useUserStore()
    store.role = 'dispatcher'
    store.userInfo = { employee_name: '员工乙', username: 'staff_b', account_id: 'immutable-3' }

    expect(store.displayName()).toBe('员工乙')
    expect(store.accountName()).toBe('staff_b')
    expect(store.roleName()).toBe('线索调度员')
  })

  it('falls back to the immutable account when display fields are absent', () => {
    const store = useUserStore()
    store.userInfo = { account_id: 'account-9' }

    expect(store.displayName()).toBe('account-9')
    expect(store.accountName()).toBe('account-9')
  })
})
