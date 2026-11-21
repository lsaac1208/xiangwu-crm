import { test, expect } from '@playwright/test'
import { mockAPI, json, managementStats } from './crm-fixtures.js'

for (const width of [375, 390, 768, 1440]) {
  test(`report readability and independent panels ${width}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 844 })
    await mockAPI(page)
    let loads = 0
    await page.route('**/api/stats/management*', route => { loads++; return json(route, managementStats) })
    await page.goto('/stats/sla')
    await expect(page.locator('.report-people-cards')).toBeVisible()
    if (width < 768) {
      const smallTargets = await page.locator('.statistics-report button').evaluateAll(buttons => buttons.filter(button => {
        const rect = button.getBoundingClientRect()
        return rect.width > 0 && rect.height > 0 && !button.disabled && (rect.height < 44 || rect.width < 44)
      }).map(button => button.textContent.trim()))
      expect(smallTargets).toEqual([])
      const tabs = page.getByRole('tablist', { name: '统计模块' })
      await expect(tabs).toBeVisible()
      await expect(tabs.getByRole('tab', { name: '团队', exact: true })).toHaveAttribute('aria-selected', 'true')
      const result = page.locator('.report-people-result').first()
      const box = await result.boundingBox()
      expect(box.y + box.height).toBeLessThan(786)
      await tabs.getByRole('tab', { name: '跟进监督', exact: true }).click()
      await expect(page.getByRole('region', { name: '当前跟进监督' })).toBeVisible()
      await tabs.getByRole('tab', { name: '渠道', exact: true }).click()
      const source = page.locator('.report-channel-table tbody button').first()
      expect((await source.boundingBox()).height).toBeGreaterThanOrEqual(44)
      await source.click()
      await expect(page.locator('.report-source-detail')).toBeVisible()
      expect((await page.locator('.report-detail-close').boundingBox()).height).toBeGreaterThanOrEqual(44)
      await tabs.getByRole('tab', { name: '团队', exact: true }).click()
      await tabs.getByRole('tab', { name: '渠道', exact: true }).click()
      await expect(page.locator('.report-source-detail')).toBeVisible()
      expect(loads).toBe(1)
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.screenshot({ path: info.outputPath(`report-${width}.png`), fullPage: true })
  })
}

test('followup shows the customer without exposing a full phone', async ({ page }) => {
  await mockAPI(page)
  await page.goto('/leads/28/followup')
  const context = page.getByRole('region', { name: '当前跟进客户' })
  await expect(context).toContainText('张若溪')
  await expect(context).toContainText('邀约阶段')
  expect(await context.textContent()).not.toMatch(/138\d{8}/)
})

test('missing UUID browser capability still supports lead creation', async ({ page }) => {
  await mockAPI(page)
  await page.addInitScript(() => Object.defineProperty(Crypto.prototype, 'randomUUID', { value: undefined, configurable: true }))
  await page.goto('/leads/new')
  await page.getByLabel('客户姓名', { exact: true }).fill('兼容录入')
  await page.getByLabel('联系电话', { exact: true }).fill('13912345678')
  await page.getByRole('combobox', { name: '来源渠道', exact: true }).selectOption({ label: '好学校' })
  await page.route('**/api/leads', route => route.request().method() === 'POST' ? json(route, { lead: { id: 28 } }, 201) : route.fallback())
  const key = page.waitForRequest(request => new URL(request.url()).pathname === '/api/leads' && request.method() === 'POST')
  await page.getByRole('button', { name: '创建线索并查看详情', exact: true }).click()
  expect((await key).headers()['idempotency-key']).toMatch(/^[\da-f]{8}-[\da-f]{4}-4[\da-f]{3}-[89ab][\da-f]{3}-[\da-f]{12}$/)
})

test('temporary refresh failure preserves draft and retry can recover', async ({ page }) => {
  await mockAPI(page)
  let refreshCalls = 0, submissions = 0, dialogs = 0
  const keys = []
  page.on('dialog', async dialog => { dialogs++; await dialog.dismiss() })
  await page.route('**/api/auth/refresh', route => {
    refreshCalls++
    return refreshCalls === 1 ? json(route, { error: 'temporary_unavailable', message: '登录验证暂不可用' }, 503) : json(route, {})
  })
  await page.route('**/api/leads/28/followups', route => {
    submissions++; keys.push(route.request().headers()['idempotency-key'])
    return json(route, submissions < 3 ? { error: 'expired' } : { success: true }, submissions < 3 ? 401 : 200)
  })
  await page.goto('/leads/28/followup')
  const draft = '网络恢复后应能安全重试的跟进草稿'
  await page.locator('textarea').fill(draft)
  await page.locator('button[type=submit]').click()
  await expect(page.getByRole('alert')).toContainText('暂不可用')
  await expect(page.locator('textarea')).toHaveValue(draft)
  await expect(page).toHaveURL(/\/leads\/28\/followup$/)
  await page.locator('button[type=submit]').click()
  await expect(page).toHaveURL(/\/leads\/28$/)
  expect(refreshCalls).toBe(2)
  expect(dialogs).toBe(0)
  expect(keys.length).toBe(3)
  expect(new Set(keys).size).toBe(1)
})

test('invalid session still requests login after canceled leave', async ({ page }) => {
  await mockAPI(page)
  let refreshCalls = 0, dialogs = 0
  page.on('dialog', async dialog => { dialogs++; await dialog.dismiss() })
  await page.route('**/api/auth/refresh', route => { refreshCalls++; return json(route, { error: 'invalid_refresh_token' }, 401) })
  await page.route('**/api/leads/28/followups', route => json(route, { error: 'expired' }, 401))
  await page.goto('/leads/28/followup')
  await page.locator('textarea').fill('离页取消也不能恢复已经撤销的会话')
  await page.locator('button[type=submit]').click()
  await expect.poll(() => dialogs).toBe(1)
  await expect(page.locator('button[type=submit]')).toBeEnabled()
  await page.locator('button[type=submit]').click()
  await expect.poll(() => dialogs).toBe(2)
  expect(refreshCalls).toBe(1)
})

test('no secure randomness keeps the form visible and prevents submission', async ({ page }) => {
  await mockAPI(page)
  await page.addInitScript(() => {
    Object.defineProperty(Crypto.prototype, 'randomUUID', { value: undefined, configurable: true })
    Object.defineProperty(Crypto.prototype, 'getRandomValues', { value: undefined, configurable: true })
  })
  let writes = 0
  await page.route('**/api/leads', route => { if (route.request().method() === 'POST') writes++; return route.fallback() })
  await page.goto('/leads/new')
  await page.getByLabel('客户姓名', { exact: true }).fill('保留的客户输入')
  await page.getByLabel('联系电话', { exact: true }).fill('13912345678')
  await page.getByRole('combobox', { name: '来源渠道', exact: true }).selectOption({ label: '好学校' })
  await page.getByRole('button', { name: '创建线索并查看详情', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('安全提交')
  await expect(page.getByLabel('客户姓名', { exact: true })).toHaveValue('保留的客户输入')
  expect(writes).toBe(0)
})
