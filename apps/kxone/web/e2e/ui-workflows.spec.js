import { managementStats, followupSupervision } from './crm-fixtures.js'
import { test, expect } from '@playwright/test'
import { mockAPI, json, poolLeads, user } from './crm-fixtures.js'

test('logout reports failure, then leaves the workspace after successful retry', async ({ page }) => {
  await mockAPI(page)
  let failed = true
  await page.route('**/api/auth/logout', route => json(route, {}, failed ? 503 : 200))
  await page.goto('/leads')
  await page.getByRole('button', { name: '当前账户', exact: true }).click()
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page.getByRole('button', { name: '确认', exact: true }).click()
  await expect(page.getByText('退出失败，请检查网络后重试')).toBeVisible()
  await expect(page).toHaveURL(/\/leads$/)
  failed = false
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page.getByRole('button', { name: '确认', exact: true }).click()
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.locator('.app-sidebar')).toHaveCount(0)
})

for (const width of [375, 1440]) {
  test(`pool pagination keeps existing records and retries the failed page ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 }); await mockAPI(page)
    let failed = true
    const requested = []
    await page.route('**/api/public-pool?**', route => {
      const current = new URL(route.request().url()).searchParams.get('page'); requested.push(current)
      if (current === '2' && failed) return json(route, {}, 503)
      return json(route, { leads: current === '1' ? poolLeads : [{ ...poolLeads[0], id: 31, customer_name: '下一页客户' }], total: 2, total_pages: 2 })
    })
    await page.goto('/public-pool')
    const list = page.locator(width < 768 ? '.pool-mobile-list' : '.pool-table')
    await expect(list.getByText('周同学', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '加载更多', exact: true }).click()
    await expect(page.getByText('后续线索加载失败')).toBeVisible()
    await expect(list.getByText('周同学', { exact: true })).toBeVisible()
    failed = false
    await page.getByRole('button', { name: '重试加载更多', exact: true }).click()
    await expect(list.getByText('下一页客户', { exact: true })).toBeVisible()
    expect(requested).toEqual(['1', '2', '2'])
  })
}

test('statistics changes ignore delayed responses and preserve the current date filter', async ({ page }) => {
  await mockAPI(page)
  let oldRequest
  await page.route('**/api/stats/management?**', route => {
    if (new URL(route.request().url()).searchParams.get('start_date') === '2026-08-21') {
      oldRequest = route
      return
    }
    return json(route, { ...managementStats, assignment: { ...managementStats.assignment, total: 30 } })
  })
  await page.goto('/stats/sla')
  const newLeads = page.getByRole('button', { name: /条线索 ↗/ })
  await expect(newLeads).toContainText('30')
  await page.getByRole('button', { name: '近7天', exact: true }).click()
  await expect.poll(() => Boolean(oldRequest)).toBe(true)
  await page.getByRole('button', { name: '近30天', exact: true }).click()
  const staleResponse = page.waitForResponse(response => response.url().includes('start_date=2026-08-21'))
  await json(oldRequest, { ...managementStats, assignment: { ...managementStats.assignment, total: 7 } })
  await (await staleResponse).finished()
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  await expect(newLeads).toContainText('30')
  await expect(page.getByRole('button', { name: '近30天', exact: true, pressed: true })).toHaveAttribute('aria-pressed', 'true')
})

test('statistics retry preserves dates and custom ranges remain available', async ({ page }) => {
  test.setTimeout(20_000)
  await mockAPI(page)
  let failed = false
  await page.route('**/api/stats/management?**', route => failed ? json(route, {}, 503) : route.fallback())
  await page.goto('/stats/sla')
  await expect(page.getByRole('button', { name: /条线索 ↗/ })).toBeVisible()
  failed = true
  await page.getByRole('button', { name: '近7天', exact: true }).click()
  await expect(page.getByText('统计数据加载失败', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '近7天', exact: true, pressed: true })).toHaveAttribute('aria-pressed', 'true')
  failed = false
  const retried = page.waitForResponse(response => response.url().includes('/api/stats/management?'))
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  expect(new URL((await retried).url()).searchParams.get('start_date')).toBe('2026-08-21')
  await expect(page.getByRole('button', { name: /条线索 ↗/ })).toBeVisible()
  await page.getByRole('button', { name: '自定义', exact: true }).click()
  for (const day of ['10', '12']) {
    const timestamp = new Date(`2026-08-${day}T00:00:00+08:00`).getTime()
    await page.locator(`[data-calendar-date="${timestamp}"]`).click()
  }
  const customized = page.waitForResponse(response => response.url().includes('/api/stats/management?'))
  await page.getByRole('button', { name: '确认', exact: true }).click()
  const params = new URL((await customized).url()).searchParams
  expect(params.get('start_date')).toBe('2026-08-10')
  expect(params.get('end_date')).toBe('2026-08-12')
})

for (const [path, field] of [['/leads/new', '客户姓名'], ['/leads/28/followup', '跟进内容'], ['/leads/28/appointment', '交接说明']]) {
  test(`unsaved input is protected when leaving ${path}`, async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 900 }); await mockAPI(page)
    await page.goto(path)
    await page.getByLabel(field, { exact: true }).fill('正在填写的重要信息')
    const back = page.locator('.van-nav-bar__left')
    await back.click()
    const dialog = page.getByRole('dialog', { name: '离开当前表单？' })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: '继续填写' }).click()
    await expect(page.getByLabel(field, { exact: true })).toHaveValue('正在填写的重要信息')
    await back.click()
    await dialog.getByRole('button', { name: '放弃填写' }).click()
    await expect(page).toHaveURL(path === '/leads/new' ? /\/leads$/ : /\/leads\/28$/)
  })
}

test('mobile more menu supports keyboard close and restores the trigger', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 900 }); await mockAPI(page)
  await page.goto('/leads')
  const trigger = page.getByRole('button', { name: '更多', exact: true })
  await trigger.click()
  const popup = page.getByRole('dialog', { name: '更多功能' })
  await expect(popup.getByRole('button', { name: '关闭更多菜单' })).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(popup.getByRole('button', { name: '退出登录' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(popup).toBeHidden()
  await expect(trigger).toBeFocused()
})

test('appointment date changes ignore previous-day responses', async ({ page }) => {
  await mockAPI(page)
  let oldRequest
  await page.route('**/api/appointments?**', route => {
    if (new URL(route.request().url()).searchParams.get('start_at').startsWith('2026-08-27')) { oldRequest = route; return }
    return json(route, { appointments: [] })
  })
  await page.goto('/appointments')
  await page.getByRole('button', { name: '日视图' }).click()
  await expect(page.getByText('张若溪', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '下一天' }).click()
  await expect.poll(() => Boolean(oldRequest)).toBe(true)
  await page.getByRole('button', { name: '下一天' }).click()
  await expect(page.getByText('正在加载日程', { exact: true })).toHaveCount(0)
  await json(oldRequest, { appointments: [{ id: 99, customer_name: '过时预约', start_at: '2026-08-28T14:00:00+08:00', status: 'pending_assignment' }] })
  await expect(page.getByLabel('选择日程日期')).toHaveValue('2026-08-29')
  await expect(page.getByText('过时预约')).toHaveCount(0)
})

test('closer retries inviter loading without losing customer information', async ({ page }) => {
  await mockAPI(page)
  await page.route('**/api/user/info', route => json(route, { ...user, role: 'closer' }))
  let failed = true
  await page.route('**/api/users/inviters', route => failed ? json(route, {}, 503) : route.fallback())
  await page.goto('/leads/new')
  await page.getByLabel('客户姓名', { exact: true }).fill('待录入家长')
  await expect(page.getByText('邀约师加载失败', { exact: true })).toBeVisible()
  failed = false
  await page.getByRole('button', { name: '重试加载邀约师' }).click()
  await expect(page.locator('.inviter-option').getByText('员工己')).toBeVisible()
  await expect(page.getByLabel('客户姓名', { exact: true })).toHaveValue('待录入家长')
})

test('role navigation and restricted routes agree for all CRM roles', async ({ page }) => {
  await mockAPI(page)
  for (const role of ['sales', 'inviter', 'closer', 'dispatcher', 'manager', 'admin']) {
    await page.route('**/api/user/info', route => json(route, { ...user, role }))
    await page.goto('/leads')
    const nav = page.locator('.app-nav')
    await expect(nav.getByRole('link', { name: '新建线索' })).toBeVisible()
    await expect(nav.getByRole('link', { name: '统计看板' })).toHaveCount(['manager', 'admin'].includes(role) ? 1 : 0)
    await expect(nav.getByRole('link', { name: '系统设置' })).toHaveCount(role === 'admin' ? 1 : 0)
    await expect(nav.getByRole('link', { name: '预约排班' })).toHaveCount(role === 'dispatcher' ? 0 : 1)
    await page.goto('/admin/settings')
    await expect(page).toHaveURL(role === 'admin' ? /\/admin\/settings/ : /\/leads$/)
    await page.unroute('**/api/user/info')
  }
})

test('dispatcher can choose an inviter for batch assignment and filters clear selection', async ({ page }) => {
  await mockAPI(page)
  await page.route('**/api/user/info', route => json(route, { ...user, role: 'dispatcher' }))
  await page.goto('/leads')
  await page.getByRole('checkbox', { name: '选择线索 KX202608270029', exact: true }).check()
  const bar = page.getByRole('region', { name: '批量操作' })
  await bar.getByLabel('批量操作目标人员').selectOption('fixture-inviter')
  await expect(bar.getByRole('button', { name: '批量分配', exact: true })).toBeEnabled()
  await page.locator('.van-search input').fill('查找另一位家长')
  await expect(bar).toHaveCount(0)
})

test('tablet lead form leaves room for input and all five rating options', async ({ page }, info) => {
  await mockAPI(page)
  let unloadPrompts = 0
  page.on('dialog', dialog => { if (dialog.type() === 'beforeunload') unloadPrompts++; return dialog.accept() })
  for (const width of [768, 1024, 1199, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/leads/new')
    const phone = await page.getByLabel('联系电话', { exact: true }).boundingBox()
    expect(phone.width).toBeGreaterThan(120)
    const rating = await page.getByRole('radiogroup', { name: '意向星级' }).boundingBox()
    const field = await page.locator('.rating-field').boundingBox()
    expect(rating.x + rating.width).toBeLessThanOrEqual(field.x + field.width)
    await page.getByRole('radiogroup', { name: '意向星级' }).locator('label').last().click()
    await expect(page.getByRole('radio', { name: '5 星', exact: true })).toBeChecked()
    await page.screenshot({ path: info.outputPath(`create-tablet-${width}.png`), fullPage: true })
  }
  expect(unloadPrompts).toBe(3)
})


test('statistics supervision loads independently and retries without changing cohort data', async ({ page }) => {
 await mockAPI(page)
 let pending, failed = false, reports = 0
 await page.route('**/api/stats/management?**', route => { reports++; return route.fallback() })
 await page.route('**/api/stats/supervision', route => { if (failed) return json(route, {}, 503); pending = route })
 await page.goto('/stats/sla')
 await expect(page.getByRole('button', { name: /条线索 ↗/ })).toContainText('38')
 await expect(page.getByText('正在加载当前待办', { exact: true })).toBeVisible()
 await expect(page.locator('.report-supervision-summary')).toHaveCount(0)
 await json(pending, followupSupervision)
 await expect(page.locator('.report-supervision-summary')).toContainText('跟进逾期')
 failed = true
 await page.getByRole('button', { name: '刷新数据', exact: true }).click()
 await expect(page.getByText('当前待办加载失败', { exact: true })).toBeVisible()
 await expect(page.getByRole('button', { name: /条线索 ↗/ })).toContainText('38')
 const prior = reports
 failed = false
 pending = null
 await page.getByRole('button', { name: '重新加载待办', exact: true }).click()
 await expect.poll(() => Boolean(pending)).toBe(true)
 await json(pending, followupSupervision)
 await expect(page.locator('.report-supervision-summary')).toBeVisible()
 expect(reports).toBe(prior)
})

test('statistics brief reports zero, ties and small-sample leaders honestly', async ({ page }) => {
 await mockAPI(page)
 for (const mode of ['zero', 'tie', 'small']) {
  await page.route('**/api/stats/management?**', route => {
   const base = managementStats.teams[0]
   const teams = mode === 'zero' ? [{ ...base, converted: 0 }] : mode === 'tie' ? [{ ...base, assigned: 20, converted: 5 }, { ...base, sales_userid: 'other', sales_name: '另一位成员', assigned: 30, converted: 5 }] : [{ ...base, assigned: 10, converted: 5 }, { ...base, sales_userid: 'other', sales_name: '另一位成员', assigned: 30, converted: 4 }]
   return json(route, { ...managementStats, teams, assignment: { ...managementStats.assignment, converted: mode === 'zero' ? 0 : 10 } })
  })
  await page.goto('/stats/sla')
  const brief = page.getByRole('region', { name: '本期简报', exact: true })
  if (mode === 'zero') await expect(brief.getByRole('heading')).toHaveText('本期线索还在推进中')
  if (mode === 'tie') await expect(brief.getByRole('heading')).toHaveText('2 位成员的归属线索成交数并列领先')
  if (mode === 'small') { await expect(brief.getByRole('heading')).toContainText('员工己'); await expect(brief).toContainText('样本较少') }
  await page.unroute('**/api/stats/management?**')
 }
})
