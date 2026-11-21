import { test, expect } from '@playwright/test'

const customers = [
  { id: 701, lead_no: 'REVIEW-701', customer_name: '隔离客户甲', status: 'following', business_stage: 'invitation', ownership_state: 'private', result_status: 'active', current_owner_userid: 'owner-a', assigned_inviter_userid: 'owner-a', next_followup_due_at: '2020-01-01T00:00:00Z', created_at: '2026-10-31T00:00:00Z' },
  { id: 702, lead_no: 'REVIEW-702', customer_name: '隔离客户乙', status: 'assigned_pending_claim', business_stage: 'invitation', ownership_state: 'assigned_pending_claim', result_status: 'active', assigned_inviter_userid: 'owner-a', created_at: '2026-10-31T00:00:00Z' }
]
async function fixture(page, { healthFails = false } = {}) {
  const requests = []
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname.slice(4)
    const json = body => route.fulfill({ json: body })
    if (path === '/user/info') return json({ id: 1, wecom_userid: 'review-admin', username: 'review-admin', role: 'admin', features: { workflow_v2_enabled: false, appointment_enabled: false, pool_enabled: true } })
    if (path === '/leads') {
      requests.push(url)
      const keyword = url.searchParams.get('keyword') || ''
      const items = customers.filter(lead => lead.customer_name.includes(keyword))
      return json({ Leads: items, Total: items.length, Page: 1, PageSize: 20, TotalPages: 1 })
    }
    const leadID = path.match(/^\/leads\/(\d+)$/)
    if (leadID) return json({ lead: customers.find(lead => lead.id === Number(leadID[1])), followups: [], dispatch_records: [], activities: [], notification_status: 'available', notification_failures: [{ id: 1, event_type: 'new_lead', will_retry: false }] })
    if (path === '/workbench') return json({ summary: { overdue_followup: 1, pending_claim: 1 }, tasks: [{ lead_id: 701, lead_no: 'REVIEW-701', customer_name: '隔离客户甲', title: '客户回访已逾期', type: 'overdue_followup' }] })
    if (path === '/admin/group-robot/health') return healthFails ? route.fulfill({ status: 503, json: { error: 'group_robot_health_failed' } }) : json({ available: true, healthy: false, failed_count: 2, retrying_count: 1, exhausted_count: 1, pending_overdue_count: 1, last_success_at: null, recent_failures: [{ id: 1, lead_id: 701, event_type: 'new_lead', reason: '请求超时', will_retry: true }] })
    if (path === '/admin/settings') return json({ settings: [] })
    if (path === '/me/lead-views') return json({ items: [] })
    if (path === '/me/lead-list-preferences') return json({ version: 1 })
    if (path === '/channel-integrations') return json({ integrations: [] })
    if (path.startsWith('/users/') || path === '/admin/users') return json({ bindings: [], items: [], total: 0 })
    return json({})
  })
  return requests
}

test('switching customers replaces page identity and never retains the previous customer', async ({ page }) => {
  await fixture(page)
  await page.goto('/leads/701')
  await expect(page.getByRole('heading', { name: '隔离客户甲', exact: true })).toBeVisible()
  await page.evaluate(() => document.querySelector('#app').__vue_app__.config.globalProperties.$router.push('/leads/702'))
  await expect(page.getByRole('heading', { name: '隔离客户乙', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '隔离客户甲', exact: true })).toHaveCount(0)
})

test('changing the customer in an unfinished followup keeps the existing draft until explicitly abandoned', async ({ page }) => {
  await fixture(page)
  await page.goto('/leads/701/followup')
  await page.getByLabel('跟进内容', { exact: true }).fill('尚未提交的跟进草稿')
  const changeCustomer = () => page.evaluate(() => { void document.querySelector('#app').__vue_app__.config.globalProperties.$router.push('/leads/702/followup') })
  await changeCustomer()
  await expect(page.getByRole('button', { name: '继续填写', exact: true })).toBeVisible({ timeout: 2000 })
  await page.getByRole('button', { name: '继续填写', exact: true }).click()
  await expect(page).toHaveURL(/\/leads\/701\/followup$/)
  await expect(page.getByLabel('跟进内容', { exact: true })).toHaveValue('尚未提交的跟进草稿')
  await changeCustomer()
  await page.getByRole('button', { name: '放弃填写', exact: true }).click()
  await expect(page).toHaveURL(/\/leads\/702\/followup$/)
  await expect(page.getByLabel('跟进内容', { exact: true })).toHaveValue('')
})

for (const width of [390, 1440]) {
  test(`triage navigation keeps keyword filtering and account preference ${width}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 1000 })
    const requests = await fixture(page)
    await page.goto('/leads')
    await expect.poll(() => requests.some(url => url.searchParams.get('status') === 'needs_action')).toBe(true)
    await expect(page.locator(width < 768 ? '.lead-mobile-select-row' : '.lead-table tbody tr')).toHaveCount(2)
    await page.evaluate(() => document.fonts.ready)
    await page.screenshot({ path: info.outputPath(`triage-${width}.png`), fullPage: true })
    await page.evaluate(() => document.querySelector('#app').__vue_app__.config.globalProperties.$router.push('/leads?status=needs_action&keyword=不存在的客户'))
    await expect.poll(() => requests.some(url => url.searchParams.get('keyword') === '不存在的客户')).toBe(true)
    await expect(page.getByText('没有匹配的线索，请尝试其他关键词', { exact: true })).toBeVisible()
    const rows = page.locator(width < 768 ? '.lead-mobile-select-row' : '.lead-table tbody tr')
    await expect(rows).toHaveCount(0)
    await page.getByRole('button', { name: '全部', exact: true }).first().click()
    await page.reload()
    if (width < 768) await expect(page.getByRole('button', { name: '全部', exact: true }).first()).toHaveAttribute('aria-pressed', 'true')
    else await expect(page.getByRole('combobox', { name: '全部线索邀约师筛选' })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
  test(`notification status and persistent lead failure are visible ${width}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 1000 })
    await fixture(page)
    await page.goto('/admin/settings?section=notification')
    await expect(page.getByText('群机器人投递需要关注', { exact: true })).toBeVisible()
    await expect(page.locator('.robot-delivery-health')).toContainText('等待重试 1 条')
    await expect(page.locator('.robot-delivery-health')).toContainText('积压超过 10 分钟：1 条')
    await page.screenshot({ path: info.outputPath(`notification-health-${width}.png`), fullPage: true })
    await page.locator('.robot-delivery-health').getByRole('link', { name: '线索 701' }).click()
    await expect(page.locator('.lead-notification-alert')).toContainText('已停止自动重试')
    await page.screenshot({ path: info.outputPath(`notification-lead-${width}.png`), fullPage: true })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

test('a failed status query stays unknown and can be retried', async ({ page }) => {
  await fixture(page, { healthFails: true })
  await page.goto('/admin/settings?section=notification')
  await expect(page.getByText('投递状态未知', { exact: true })).toBeVisible()
  await expect(page.getByText('近 24 小时未发现投递异常', { exact: true })).toHaveCount(0)
  await expect(page.locator('.robot-delivery-health').getByRole('button', { name: '重新检查' })).toBeEnabled()
})

test('a saved lead keeps its notification warning after navigation and reload until acknowledged', async ({ page }) => {
  await fixture(page)
  await page.route('**/api/leads', route => route.request().method() === 'POST'
    ? route.fulfill({ status: 201, json: { lead: customers[0], warnings: ['notification_failed'] } })
    : route.fallback())
  await page.route('**/api/leads/701', route => route.fulfill({ json: { lead: customers[0], followups: [], dispatch_records: [], activities: [], notification_status: 'available', notification_failures: [] } }))
  await page.goto('/leads/new')
  await page.getByLabel('客户姓名', { exact: true }).fill('通知反馈验收')
  await page.getByLabel('微信号', { exact: true }).fill('isolated-notification-fixture')
  await page.getByRole('combobox', { name: '来源渠道', exact: true }).selectOption('转介绍')
  await page.getByRole('button', { name: '创建线索并查看详情' }).click()
  await expect(page.locator('.lead-notification-alert')).toContainText('线索已保存，群提醒需要确认')
  await page.reload()
  await expect(page.locator('.lead-notification-alert')).toContainText('系统会尝试补发')
  await page.locator('.lead-notification-alert').getByRole('button', { name: '知道了' }).click()
  await page.reload()
  await expect(page.locator('.lead-notification-alert')).toHaveCount(0)
})
