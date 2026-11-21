import { test, expect } from '@playwright/test'

for (const width of [375, 1440]) {
  test(`management report uses real cohort data and preserves drilldown filters ${width}`, async ({ page }, info) => {
    await page.request.get('/test-session?user=admin')
    await page.setViewportSize({ width, height: 1000 })
    const aiRequests = [], errors = []
    page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/agent')) aiRequests.push(request.url()) })
    page.on('pageerror', error => errors.push(error.message))
    const loaded = page.waitForResponse(r => r.url().includes('/api/stats/management'))
    await page.goto('/agent/retained-private-conversation')
    await expect(page).toHaveURL(/\/stats\/sla$/)
    expect((await loaded).status()).toBe(200)
    await expect(page.getByRole('heading', { name: '团队与渠道', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'AI 分析', exact: true })).toHaveCount(0)
    await expect(page.locator('.report-owner-row')).not.toHaveCount(0)
    const supervisionBefore = await page.locator('.report-supervision-summary').textContent()
    const allResponse = page.waitForResponse(r => r.url().includes('/api/stats/management') && !new URL(r.url()).search)
    await page.getByRole('button', { name: '全部', exact: true }).click()
    const aggregate = await (await allResponse).json()
    const list = await (await page.request.get('/api/leads?page_size=100')).json()
    expect(aggregate.assignment.total).toBe((list.total ?? list.Total))
    const allButton = page.getByRole('button', { name: /条线索 ↗/ })
    await expect(allButton).toContainText(String(aggregate.assignment.total))
    const teamGroup = page.getByRole('group', { name: '团队排序', exact: true })
    await teamGroup.getByRole('button', { name: /转化率/ }).click()
    const inviterRows = aggregate.teams.filter(row => row.role === 'inviter').sort((a,b) => b.conversion_rate - a.conversion_rate || b.assigned - a.assigned || a.sales_userid.localeCompare(b.sales_userid))
    await expect(page.locator('.report-people-name > strong')).toHaveText(inviterRows.map(row => row.sales_name))
    await page.getByRole('button', { name: '谈单归属', exact: true }).click()
    await expect(page.locator('.report-people-name > strong')).toContainText(['closer_001'])
    const datedResponse = page.waitForResponse(r => r.url().includes('/api/stats/management') && new URL(r.url()).searchParams.has('start_date'))
    await page.getByRole('button', { name: '近7天', exact: true }).click()
    const dateParams = new URL((await datedResponse).url()).searchParams
    const filtered = page.waitForResponse(r => r.url().includes('/api/stats/management') && new URL(r.url()).searchParams.get('sales_userid') === 'closer_001')
    await page.getByRole('button', { name: '邀约归属', exact: true }).click()
    await page.getByRole('button', { name: '全部人员', exact: true }).click()
    await page.locator('.sales-picker-option').filter({ hasText: 'closer_001' }).click()
    await expect(page.getByRole('button', { name: '谈单归属', exact: true })).toHaveAttribute('aria-pressed', 'true')
    const filteredResponse = await filtered
    expect(filteredResponse.status()).toBe(200)
    const filteredStats = await filteredResponse.json()
    await expect(page.locator('.report-people-cards > button')).toHaveCount(1)
    await expect(page.locator('.report-brief h2')).toContainText('相关线索表现')
    expect(await page.locator('.report-supervision-summary').textContent()).toBe(supervisionBefore)
    await expect(page.locator('.report-owner-row')).not.toHaveCount(0)
    if (width < 768) await page.getByRole('tab', { name: '渠道', exact: true }).click()
    await page.getByRole('group', { name: '渠道比较指标', exact: true }).getByRole('button', { name: '转化率', exact: true }).click()
    await page.locator('.report-channels tbody .report-link').first().click()
    await expect(page.locator('.report-source-detail')).toBeVisible()
    if (width === 375) {
      const table = page.locator('.report-channel-table')
      expect(await table.evaluate(el => el.scrollWidth <= el.parentElement.clientWidth + 1)).toBe(true)
      await expect(table.getByRole('columnheader', { name: '转化率', exact: true })).toBeVisible()
      await expect(table.getByRole('columnheader', { name: '无效率', exact: true })).toBeVisible()
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.screenshot({ path: info.outputPath(`statistics-${width}.png`), fullPage: true })
    const drilledResponse = page.waitForResponse(r => new URL(r.url()).pathname === '/api/leads' && new URL(r.url()).searchParams.get('sales_userid') === 'closer_001')
    await allButton.click()
    await expect(page).toHaveURL(/\/leads\?/);
    const drilldown = new URL(page.url()).searchParams
    expect(drilldown.get('start_date')).toBe(dateParams.get('start_date'))
    expect(drilldown.get('end_date')).toBe(dateParams.get('end_date'))
    expect(drilldown.get('sales_userid')).toBe('closer_001')
    expect(drilldown.get('status')).toBe('all')
    const response = await drilledResponse
    expect(response.status()).toBe(200)
    expect(((await response.json()).total ?? (await response.json()).Total)).toBe(filteredStats.assignment.total)
    expect(aiRequests).toEqual([])
    expect(errors).toEqual([])
  })
}

test('supervision drilldown is current-owner scoped and excludes cohort dates', async ({ page }) => {
  await page.request.get('/test-session?user=admin')
  const loaded = page.waitForResponse(r => r.url().includes('/api/stats/supervision'))
  await page.goto('/stats/sla')
  const data = await (await loaded).json()
  const owner = data.owners.find(row => row.overdue_followup > 0)
  expect(owner).toBeTruthy()
  const row = page.locator('.report-owner-row').filter({ hasText: owner.name })
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/api/leads' && new URL(r.url()).searchParams.get('status') === 'followup_overdue')
  await row.getByRole('link', { name: '看逾期线索 ↗', exact: true }).click()
  await expect(page).toHaveURL(/\/leads\?/);
  const url = new URL(page.url())
  expect(url.searchParams.get('sales_userid')).toBe(owner.userid)
  expect(url.searchParams.has('start_date')).toBe(false)
  expect(url.searchParams.has('end_date')).toBe(false)
  const actual = await response
  expect(actual.status()).toBe(200)
  const list = await actual.json()
  expect((list.total ?? list.Total)).toBe(owner.overdue_followup)
  for (const lead of (list.leads ?? list.Leads)) expect(lead.current_owner_userid).toBe(owner.userid)
})

test('statistics and legacy AI URLs enforce manager permissions', async ({ page }) => {
  await page.request.get('/test-session?user=inviter_001')
  for (const endpoint of ['management', 'supervision', 'dashboard']) expect((await page.request.get(`/api/stats/${endpoint}`)).status()).toBe(403)
  for (const path of ['/stats/sla', '/agent', '/agent/private-history']) {
    await page.goto(path)
    await expect(page).toHaveURL(/\/leads$/)
    await expect(page.getByRole('heading', { name: '团队与渠道', exact: true })).toHaveCount(0)
  }
  await page.context().clearCookies()
  expect((await page.request.get('/api/stats/management')).status()).toBe(401)
  expect((await page.request.get('/api/stats/supervision')).status()).toBe(401)
})
