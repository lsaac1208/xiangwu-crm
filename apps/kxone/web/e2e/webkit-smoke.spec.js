import { test, expect } from '@playwright/test'
import { mockAPI } from './crm-fixtures.js'

for (const width of [375, 1440]) {
  test(`WebKit core pages, forms and overlays ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await mockAPI(page)
    for (const path of ['/leads', '/leads/28', '/agent', '/appointments', '/admin/settings', '/public-pool', '/login']) {
      await page.goto(path)
      await expect(page.locator('h1').first()).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBeTruthy()
    }
    if (width < 768) {
      await page.goto('/leads')
      for (const label of ['选择线索状态', '线索排序', '线索视图']) {
        const trigger = page.getByRole('button', { name: label, exact: true })
        await trigger.click()
        await expect(page.locator('.lead-mobile-menu')).toBeInViewport({ ratio: 1 })
        await page.keyboard.press('Escape')
        await expect(page.locator('.lead-mobile-menu')).toBeHidden()
        await expect(trigger).toBeFocused()
      }
    }
    await page.goto('/leads/new')
    await page.getByLabel('客户姓名', { exact: true }).fill('浏览器兼容测试')
    await expect(page.getByLabel('客户姓名', { exact: true })).toHaveValue('浏览器兼容测试')
    await page.reload()
    await page.goto('/leads/28')
    await expect(page.locator('.overview-heading')).toBeVisible()
    if (width < 768) await page.getByRole('button', { name: '更多线索操作' }).click()
    else await page.locator('.more-actions-toggle').click()
    await page.getByRole('button', { name: '删除线索', exact: true }).click()
    await expect(page.locator('.delete-lead-popup')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.locator('.delete-lead-popup')).toBeHidden()
    if (width < 768) await expect(page.getByRole('button', { name: '更多线索操作' })).toBeFocused()
    await page.goto('/stats/sla')
    await expect(page.getByRole('heading', { name: '团队与渠道', exact: true })).toBeVisible()
    if (width < 768) {
      await page.getByRole('tab', { name: '跟进监督', exact: true }).click()
      await expect(page.getByRole('region', { name: '当前跟进监督' })).toBeVisible()
      await page.getByRole('tab', { name: '团队', exact: true }).click()
    }
    await page.locator('.report-filter-actions > button').first().click()
    await expect(page.locator('.stats-calendar-popup')).toBeInViewport({ ratio: 1 })
    await page.keyboard.press('Escape')
    await expect(page.locator('.stats-calendar-popup')).toBeHidden()
    await page.goto('/leads/28/followup')
    await page.locator('.datetime-trigger').click()
    await expect(page.locator('.followup-date-popup')).toBeInViewport({ ratio: 1 })
    await page.keyboard.press('Escape')
    await expect(page.locator('.followup-date-popup')).toBeHidden()
    await expect(page.locator('.datetime-trigger')).toBeFocused()
  })
}
