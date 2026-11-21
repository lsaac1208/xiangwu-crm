import { test, expect } from '@playwright/test'

for (const width of [375, 1440]) {
test(`20个视图仍可重命名、更新条件、删除并清理默认引用 ${width}`, async ({ page }) => {
  await page.setViewportSize({ width, height: 1000 })
  await page.request.get('/test-session?user=manager_002')
  const existing = await (await page.request.get('/api/me/lead-views')).json()
  for (const view of existing.items || []) await page.request.delete(`/api/me/lead-views/${view.id}`)
  const views = []
  for (let i = 0; i < 20; i++) {
    const response = await page.request.post('/api/me/lead-views', { data: { name: `回归视图${i + 1}`, filters: { status: 'all' }, sort_by: 'updated_at', sort_order: 'desc' } })
    expect(response.ok(), await response.text()).toBeTruthy()
    views.push(await response.json())
  }
  const preferences = await (await page.request.get('/api/me/lead-list-preferences')).json()
  const prefResponse = await page.request.put('/api/me/lead-list-preferences', { data: { ...preferences, default_view_id: views[0].id } })
  expect(prefResponse.ok(), await prefResponse.text()).toBeTruthy()
  await page.goto('/leads?status=following')
  const openViewMenu = async () => { if (width < 768) await page.locator('.lead-mobile-tools button').last().click() }
  const manageView = async () => {
    await openViewMenu()
    await page.getByRole('button', { name: width < 768 ? '管理当前视图' : '管理视图', exact: true }).click()
  }
  await openViewMenu()
  await expect(page.getByRole('button', { name: width < 768 ? '保存当前视图' : '保存视图', exact: true })).toBeDisabled()
  await expect(page.getByText(/已达 20 个上限/)).toBeVisible()
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBeTruthy()
  if (width < 768) await page.locator('.lead-mobile-menu').getByRole('button', { name: views[0].name, exact: true }).click()
  else await page.getByLabel('保存的线索视图').selectOption(String(views[0].id))
  await manageView()
  await page.getByLabel('视图名称', { exact: true }).fill('已重命名的回归视图')
  await page.screenshot({ path: `/tmp/crm-batch3-view-${width}.png`, fullPage: true })
  await page.getByRole('button', { name: '重命名', exact: true }).click()
  await expect(page.locator('[aria-label="管理视图"]')).toBeHidden()
  if (width < 768) {
    await page.locator('.lead-mobile-tools button').nth(1).click()
    await page.locator('.lead-mobile-menu').getByRole('button', { name: '最早创建', exact: true }).click()
  } else await page.getByLabel('线索排序').selectOption('created_at:asc')
  await manageView()
  await page.getByRole('button', { name: '更新为当前筛选条件', exact: true }).click()
  const saved = (await (await page.request.get('/api/me/lead-views')).json()).items.find(item => item.id === views[0].id)
  expect(saved.name).toBe('已重命名的回归视图')
  expect(saved.sort_by).toBe('created_at')
  expect(saved.sort_order).toBe('asc')
  await manageView()
  await page.getByRole('button', { name: '删除视图', exact: true }).click()
  await page.getByRole('button', { name: '确认', exact: true }).click()
  await openViewMenu()
  await expect(page.getByRole('button', { name: width < 768 ? '保存当前视图' : '保存视图', exact: true })).toBeEnabled()
  const cleared = await (await page.request.get('/api/me/lead-list-preferences')).json()
  expect(cleared.default_view_id).toBeNull()
  for (const view of views.slice(1)) await page.request.delete(`/api/me/lead-views/${view.id}`)
})

}
