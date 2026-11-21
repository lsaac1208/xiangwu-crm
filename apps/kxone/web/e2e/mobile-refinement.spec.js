import { test, expect } from '@playwright/test'
import { mockAPI, json, user } from './crm-fixtures.js'

for (const role of ['admin', 'manager', 'inviter', 'closer', 'dispatcher']) {
  test(`phone state choices and navigation preserve ${role} access`, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 812 })
    await mockAPI(page)
    await page.route('**/api/user/info', route => json(route, { ...user, role }))
    await page.goto('/leads')
    if (role === 'dispatcher') {
      await expect(page.getByRole('button', { name: '选择线索状态', exact: true })).toHaveCount(0)
      await page.getByRole('button', { name: '我的经手', exact: true }).click()
      await expect(page).toHaveURL(/status=dispatcher_handled/)
    } else {
      const trigger = page.getByRole('button', { name: '选择线索状态', exact: true })
      await trigger.click()
      const sheet = page.locator('.lead-mobile-menu')
      await expect(sheet.getByRole('button', { name: '待分配邀约', exact: true })).toHaveCount(['admin', 'manager'].includes(role) ? 1 : 0)
      for (const label of ['跟进中', '回访逾期', '已成交', '无效线索', '全部']) {
        await expect(sheet.getByRole('button', { name: label, exact: true })).toBeVisible()
      }
      await sheet.getByRole('button', { name: '无效线索', exact: true }).click()
      await expect(page).toHaveURL(/status=invalid/)
      await expect(page.locator('.lead-mobile-results')).toContainText('无效线索')
      await expect(trigger).toBeFocused()
    }
    await page.getByRole('navigation', { name: 'CRM 移动端主导航' }).getByRole('button', { name: '更多', exact: true }).click()
    const more = page.getByRole('dialog', { name: '更多功能' })
    await expect(more.getByRole('button', { name: '系统设置', exact: true })).toHaveCount(role === 'admin' ? 1 : 0)
    await expect(more.getByRole('button', { name: '统计看板', exact: true })).toHaveCount(['admin', 'manager'].includes(role) ? 1 : 0)
    await page.keyboard.press('Escape')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
  })
}

test('phone sorting sheet preserves all six sorts and clears selection', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await mockAPI(page)
  await page.goto('/leads')
  for (const [label, by, order] of [
    ['最近更新', 'updated_at', 'desc'], ['最新创建', 'created_at', 'desc'],
    ['最早创建', 'created_at', 'asc'], ['回访时间优先', 'next_followup_due_at', 'asc'],
    ['最近分配', 'assigned_at', 'desc'], ['最近成交', 'converted_at', 'desc']
  ]) {
    await page.getByRole('checkbox', { name: '选择线索 KX202608270029', exact: true }).check()
    await page.getByRole('button', { name: '线索排序', exact: true }).click()
    await page.locator('.lead-mobile-menu').getByRole('button', { name: label, exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('sort_by')).toBe(by)
    await expect.poll(() => new URL(page.url()).searchParams.get('sort_order')).toBe(order)
    await expect(page.getByRole('region', { name: '批量操作' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '线索排序', exact: true })).toBeFocused()
  }
})

test('phone view editor returns focus through its closed menu and preserves conflicting input', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await mockAPI(page)
  await page.route('**/api/me/lead-views', route => route.request().method() === 'POST' ? json(route, { error: 'conflict' }, 409) : json(route, { items: [] }))
  await page.goto('/leads')
  const trigger = page.getByRole('button', { name: '线索视图', exact: true })
  await trigger.click()
  await page.getByRole('button', { name: '保存当前视图', exact: true }).click()
  const sheet = page.getByRole('dialog', { name: '保存视图', exact: true })
  await sheet.getByLabel('视图名称', { exact: true }).fill('本周回访')
  await sheet.getByRole('button', { name: '保存', exact: true }).click()
  await expect(sheet).toContainText('视图版本或名称冲突，输入已保留')
  await expect(sheet.getByLabel('视图名称', { exact: true })).toHaveValue('本周回访')
  await page.keyboard.press('Escape')
  await expect(sheet).toBeHidden()
  await expect(trigger).toBeFocused()
})
