import { test, expect } from '@playwright/test'

for (const width of [375, 1440]) {
  test(`账号安全变更后重新登录并保留返回地址 ${width}`, async ({ page, request }) => {
    await page.setViewportSize({ width, height: 900 })
    await request.get('/test-session?user=admin')
    const username = `session_browser_${width}`
    const created = await request.post('/api/admin/users', { data: { username, password: 'browser-test-only', role: 'manager', status: 'active' } })
    expect(created.status()).toBe(201)
    const account = await created.json()
    const login = await page.request.post('/api/auth/login', { data: { username, password: 'browser-test-only' } })
    expect(login.ok()).toBeTruthy()
    await page.goto('/leads?status=all&keyword=安全回归')
    await page.waitForLoadState('networkidle')
    const changed = await request.put(`/api/admin/users/${account.id}`, { data: { expected_version: account.version, username, role: 'manager', department_id: 2, status: 'active' } })
    expect(changed.ok()).toBeTruthy()
    let refreshes = 0
    page.on('request', req => { if (req.url().endsWith('/api/auth/refresh')) refreshes++ })
    await page.reload({ waitUntil: 'commit' })
    await expect(page).toHaveURL(/\/login\?reason=session_expired&redirect=/)
    await expect(page.getByRole('alert')).toContainText('请重新登录')
    expect(refreshes).toBe(1)
    await page.getByRole('textbox', { name: '账号', exact: true }).fill(username)
    await page.getByLabel('密码', { exact: true }).fill('browser-test-only')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page).toHaveURL(/\/leads\?status=all&keyword=/)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  })
}

test('刷新接口无响应时释放等待并保留页面，网络恢复后可重试', async ({ page }) => {
  test.setTimeout(30_000)
  await page.request.get('/test-session?user=admin')
  await page.route('**/api/leads?**', route => route.fulfill({ status: 401, json: { error: 'expired' } }))
  let refreshes = 0
  await page.route('**/api/auth/refresh', () => { refreshes++ })
  await page.goto('/leads?status=all', { waitUntil: 'commit' })
  const failure = page.locator('.crm-state-panel--error').first()
  await expect(failure).toBeVisible({ timeout: 15_000 })
  await expect(page).toHaveURL(/\/leads\?status=all$/)
  expect(refreshes).toBe(1)
  await page.unroute('**/api/leads?**')
  await failure.getByRole('button', { name: '重试', exact: true }).click()
  await expect(failure).toBeHidden()
  await expect(page.locator('.lead-table tbody tr').first()).toBeVisible()
})

for (const width of [375, 1440]) {
  test(`登录拒绝经过浏览器规范化的站外返回地址 ${width}`, async ({ page, request }) => {
    await page.setViewportSize({ width, height: 900 })
    await request.get('/test-session?user=admin')
    const username = `redirect_browser_${width}`
    const created = await request.post('/api/admin/users', { data: { username, password: 'browser-test-only', role: 'manager', status: 'active' } })
    expect(created.status()).toBe(201)
    let offsiteRequests = 0
    await page.route('https://attacker.invalid/**', route => { offsiteRequests++; return route.abort() })
    const query = new URLSearchParams({ redirect: '/\t/attacker.invalid/phishing' })
    await page.goto(`/login?${query}`)
    await page.getByRole('textbox', { name: '账号', exact: true }).fill(username)
    await page.getByLabel('密码', { exact: true }).fill('browser-test-only')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page).toHaveURL(/\/leads$/)
    expect(offsiteRequests).toBe(0)
  })
}
