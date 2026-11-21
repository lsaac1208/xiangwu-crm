import { expect, test } from '@playwright/test'
import { mockAPI } from './crm-fixtures.js'

test('phone task routes have one action bar and top-level routes retain navigation', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await mockAPI(page)
  for (const path of ['/leads', '/public-pool', '/appointments', '/admin/settings']) {
    await page.goto(path)
    await expect(page.getByRole('navigation', { name: 'CRM 移动端主导航' })).toBeVisible()
  }
  for (const path of ['/leads/28', '/leads/new', '/leads/28/followup', '/leads/28/appointment', '/account/password']) {
    await page.goto(path)
    await expect(page.locator('h1').first()).toBeVisible()
    await expect(page.getByRole('navigation', { name: 'CRM 移动端主导航' })).toHaveCount(0)
    const height = await page.locator('.app-shell').evaluate(el => getComputedStyle(el).getPropertyValue('--kx-shell-mobile-nav-height').trim())
    expect(height).toBe('0px')
    if (path === '/account/password') continue
    const bar = page.locator(path === '/leads/28' ? '.mobile-detail-actionbar' : '.crm-mobile-submit')
    await expect(bar).toBeVisible()
    const bounds = await bar.boundingBox()
    expect(bounds.y + bounds.height).toBeLessThanOrEqual(813)
    expect(bounds.height).toBeGreaterThanOrEqual(44)
  }
})

test('phone keyboard makes the submission bar flow without losing input', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await mockAPI(page)
  await page.goto('/leads/new')
  const name = page.getByLabel('客户姓名', { exact: true })
  await name.fill('键盘布局测试')
  await name.focus()
  await page.evaluate(() => {
    Object.defineProperty(window.visualViewport, 'height', { configurable: true, value: 440 })
    window.visualViewport.dispatchEvent(new Event('resize'))
  })
  await expect(page.locator('.app-shell')).toHaveAttribute('data-keyboard-open', 'true')
  await expect(page.locator('.crm-mobile-submit')).toHaveCSS('position', 'static')
  await expect(name).toHaveValue('键盘布局测试')
  await page.evaluate(() => {
    delete window.visualViewport.height
    window.visualViewport.dispatchEvent(new Event('resize'))
  })
  await expect(page.locator('.app-shell')).toHaveAttribute('data-keyboard-open', 'false')
  await expect(page.locator('.crm-mobile-submit')).toHaveCSS('position', 'fixed')
})

test('directly opened password page returns to the workspace', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await mockAPI(page)
  await page.goto('/account/password')
  await page.locator('.van-nav-bar__left').click()
  await expect(page).toHaveURL(/\/leads$/)
})

test('shared confirmation dialog traps keyboard focus and returns to its trigger', async ({ page }) => {
  await mockAPI(page)
  await page.goto('/leads')
  await page.getByRole('button', { name: '当前账户', exact: true }).click()
  const trigger = page.getByRole('button', { name: '退出登录', exact: true })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: '退出登录', exact: true })
  const cancel = dialog.getByRole('button', { name: '取消', exact: true })
  const confirm = dialog.getByRole('button', { name: '确认', exact: true })
  await expect(cancel).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(confirm).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(cancel).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(trigger).toBeFocused()
  await expect(page).toHaveURL(/\/leads$/)
})

test('phone actions sheet returns focus through a closed opener after cancelling deletion', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 })
  await mockAPI(page)
  await page.goto('/leads/28')
  const more = page.getByRole('button', { name: '更多线索操作' })
  await more.click()
  await page.getByRole('button', { name: '删除线索', exact: true }).click()
  await expect(page.locator('.delete-lead-popup')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('.delete-lead-popup')).toBeHidden()
  await expect(more).toBeFocused()
})
