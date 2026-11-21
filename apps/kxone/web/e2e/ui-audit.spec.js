import { test, expect } from '@playwright/test'
import { createRequire } from 'node:module'
import { mockAPI, json, leads } from './crm-fixtures.js'
const require = createRequire(import.meta.url)
const axePath = require.resolve('axe-core/axe.min.js')
const crmPaths = ['/login', '/leads', '/leads/28', '/leads/new', '/leads/28/followup', '/leads/28/appointment', '/appointments', '/public-pool', '/stats/sla', '/account/password', '/admin/settings?section=notifications', '/admin/settings?section=channels', '/admin/settings?section=users', '/admin/settings?section=rules', '/admin/settings?section=ai', '/missing']
async function auditPage(page, path, issues) {
  await page.goto(path)
  await expect(page.locator('h1').first()).toBeVisible()
  await page.evaluate(() => document.fonts.ready)
  await page.addScriptTag({ path: axePath })
  const violations = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(v => ({ id: v.id, nodes: v.nodes.map(n => ({ target: n.target, summary: n.failureSummary })) })))
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  if (overflow > 1 || violations.length) issues.push({ path, overflow, violations })
}
for (const width of [320, 375, 768, 1440]) {
  test(`CRM all routes accessibility and boundaries ${width}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 }); await mockAPI(page)
    const issues = []
    for (const path of crmPaths) await auditPage(page, path, issues)
    await info.attach('findings', { body: JSON.stringify(issues, null, 2), contentType: 'application/json' })
    expect(issues).toEqual([])
  })

}
for (const width of [320, 375, 1024, 1280, 1440, 1920]) {
  test(`payment states stay in their own cells ${width}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 }); await mockAPI(page)
    const records = ['', 'partial', 'paid', 'refunded'].map((payment_status, i) => ({ ...leads[0], id: i + 1, status: 'converted', payment_status, business_stage: 'finished', customer_name: '名字很长的家长及学生咨询记录用于验证排版', source_channel: '公众号表单', source_account_name: '武汉襄五学校服务号家长咨询入口' }))
    await page.route('**/api/leads?**', route => json(route, { leads: records, total: 4, total_pages: 1 }))
    await page.goto('/leads?status=converted')
    await expect(page.getByText('付款待确认').first()).toBeVisible()
    const overlaps = await page.locator('.status-summary').evaluateAll(nodes => nodes.flatMap(node => {
      const cell = node.closest('td') || node.closest('.lead-card'); if (!cell) return []
      const rect = node.getBoundingClientRect(), parent = cell.getBoundingClientRect()
      return rect.right > parent.right + 1 || rect.left < parent.left - 1 ? [node.textContent] : []
    }))
    expect(overlaps).toEqual([])
    await page.addScriptTag({ path: axePath })
    const statusContrast = await page.evaluate(async () => (await window.axe.run('.status-summary', { runOnly: ['color-contrast'] })).violations)
    expect(statusContrast).toEqual([])
    await page.screenshot({ path: info.outputPath(`payment-${width}.png`), fullPage: true })
  })
}
test('CRM failed reads recover without losing filters or exposing forms', async ({ page }) => {
  await mockAPI(page)
  let failed = true, count = 0
  await page.route('**/api/leads?**', route => { count++; return failed ? json(route, {}, 503) : route.fallback() })
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/leads?keyword=张')
  await expect(page.getByText('线索加载失败，筛选条件已保留，请重试')).toBeVisible()
  await page.waitForTimeout(700); expect(count).toBe(1)
  failed = false; await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.locator('.lead-mobile-list').getByText('张若溪', { exact: true }).first()).toBeVisible(); await expect(page).toHaveURL(/keyword=/)
  for (const path of ['/leads/28', '/leads/28/followup', '/leads/28/appointment']) {
    await page.route('**/api/leads/28', route => json(route, {}, 503))
    await page.goto(path); await expect(page.getByRole('button', { name: '重试', exact: true })).toBeVisible()
    await expect(page.locator('form')).toHaveCount(0)
    await page.unroute('**/api/leads/28')
  }
})

test('rating is keyboard operable and appointment errors stay beside inputs', async ({ page }) => {
  await mockAPI(page)
  await page.goto('/leads/new')
  await page.getByRole('radio', { name: '3 星', exact: true }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('radio', { name: '4 星', exact: true })).toBeChecked()
  await page.goto('/leads/28/appointment')
  await page.getByLabel('开始时间').fill('2026-10-01T14:00')
  await page.getByLabel('预计结束').fill('2026-10-01T13:00')
  await page.getByLabel('交接说明').fill('测试交接说明')
  await page.getByRole('button', { name: '提交预约，等待主管分配' }).click()
  await expect(page.getByText('结束时间必须晚于开始时间')).toBeVisible()
  await expect(page.getByLabel('交接说明')).toHaveValue('测试交接说明')
})
test('appointment conflict checking blocks writes and ignores stale responses', async ({ page }) => {
  await mockAPI(page)
  await page.route('**/api/appointments?**', route => json(route, { appointments: [{ id: 3, customer_name: '测试预约', start_at: '2026-10-01T14:00:00+08:00', end_at: '2026-10-01T15:00:00+08:00', status: 'pending_assignment', version: 1, campus: '襄阳' }] }))
  await page.route('**/api/users/closers', route => json(route, { bindings: [{ wecom_userid: 'one', employee_name: '甲老师' }, { wecom_userid: 'two', employee_name: '乙老师' }] }))
  let slowRoute
  await page.route('**/api/appointments/conflicts?**', route => {
    if (new URL(route.request().url()).searchParams.get('closer_userid') === 'one') { slowRoute = route; return }
    return json(route, { conflicts: [] })
  })
  await page.goto('/appointments')
  const trigger = page.getByRole('button', { name: '分配谈单师', exact: true })
  await trigger.click()
  const popup = page.getByRole('dialog', { name: '分配谈单师', exact: true })
  await expect(popup).toBeVisible()
  await popup.getByText('甲老师').click()
  await expect(popup.getByRole('button', { name: '确认安排' })).toBeDisabled()
  await popup.getByText('乙老师').click()
  await expect(popup.getByRole('button', { name: '确认安排' })).toBeEnabled()
  await expect.poll(() => Boolean(slowRoute)).toBe(true)
  await json(slowRoute, { conflicts: [{ code: 'test_conflict', level: 'hard', message: '已过时的冲突' }] })
  await expect(popup.getByText('已过时的冲突')).toHaveCount(0)
  await popup.getByRole('button', { name: '确认安排' }).focus()
  await page.keyboard.press('Tab')
  await expect(popup.getByRole('button', { name: '关闭', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(popup).toBeHidden(); await expect(trigger).toBeFocused()
})
test('auth failures retain entered values and provide persistent feedback', async ({ page }) => {
  await mockAPI(page)
  await page.route('**/api/auth/login', route => json(route, { message: '测试登录失败' }, 503))
  await page.goto('/login')
  await page.getByLabel('账号', { exact: true }).fill('test-account')
  await page.getByLabel('密码', { exact: true }).fill('test-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('测试登录失败')
  await expect(page.getByLabel('账号', { exact: true })).toHaveValue('test-account')
  await page.route('**/api/user/password', route => json(route, { message: '测试修改失败' }, 503))
  await page.goto('/account/password')
  await page.getByLabel('当前密码').fill('old-password')
  await page.getByLabel('新密码', { exact: true }).fill('new-password')
  await page.getByLabel('确认密码').fill('new-password')
  await page.getByRole('button', { name: '确认修改' }).click()
  await expect(page.locator('.form-error')).toHaveText('测试修改失败')
  await expect(page.getByLabel('新密码', { exact: true })).toHaveValue('new-password')
})
test('settings failures hide editable defaults and retry each section', async ({ page }) => {
  await mockAPI(page)
  let failed = true
  for (const path of ['/admin/settings', '/admin/users', '/channel-integrations']) {
    await page.route(`**/api${path}**`, route => failed ? json(route, {}, 503) : route.fallback())
  }
  for (const section of ['notifications', 'rules', 'users', 'channels']) {
    failed = true; await page.goto('/admin/settings?section=' + section)
    await expect(page.getByText('设置加载失败', { exact: true })).toBeVisible()
    if (['notifications', 'rules'].includes(section)) await expect(page.getByRole('switch')).toHaveCount(0)
    failed = false; await page.getByRole('button', { name: '重试', exact: true }).click()
    await expect(page.getByText('设置加载失败', { exact: true })).toHaveCount(0)
  }
})
test('touchscreen phone keeps every CRM page within viewport', async ({ browser }, info) => {
  const context = await browser.newContext({ viewport: { width: 320, height: 720 }, isMobile: true, hasTouch: true, locale: 'zh-CN', timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce' })
  const page = await context.newPage()
  await mockAPI(page)
  const issues = []
  for (const path of crmPaths) await auditPage(page, path, issues)
  await info.attach('touch-findings', { body: JSON.stringify(issues, null, 2), contentType: 'application/json' })
  expect(issues).toEqual([])
  await context.close()
})
