import { test, expect } from '@playwright/test'

for (const width of [375, 1440]) {
  test(`新建失败重试保持录入标识，审计记录可查询 ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.request.get('/test-session?user=admin')
    await page.goto('/leads/new')
    const keys = []
    let fail = true
    let committedID
    await page.route('**/api/leads', async route => {
      if (route.request().method() !== 'POST') return route.continue()
      keys.push(route.request().headers()['idempotency-key'])
      if (fail) {
        fail = false
        const committed = await route.fetch()
        expect(committed.status()).toBe(201)
        committedID = (await committed.json()).lead.id
        return route.abort('failed')
      }
      return route.continue()
    })
    await page.getByLabel('客户姓名', { exact: true }).fill(`修复验收${width}`)
    await page.getByLabel('联系电话', { exact: true }).fill('13999995678')
    await page.getByRole('combobox', { name: '来源渠道', exact: true }).selectOption('好学校')
    await page.getByRole('button', { name: '创建线索并查看详情' }).click()
    await expect(page.getByRole('alert')).toContainText('填写内容已保留')
    await expect(page.getByLabel('客户姓名', { exact: true })).toHaveValue(`修复验收${width}`)
    await page.getByRole('button', { name: '创建线索并查看详情' }).click()
    await expect(page).toHaveURL(/\/leads\/\d+$/)
    expect(keys).toHaveLength(2)
    expect(keys[0]).toMatch(/^[0-9a-f-]{36}$/)
    expect(keys[1]).toBe(keys[0])
    const id = new URL(page.url()).pathname.split('/').pop()
    expect(Number(id)).toBe(committedID)
    await page.goto('/admin/settings?section=access')
    await expect(page.getByRole('heading', { name: '客户资料访问记录' })).toBeVisible()
    await page.getByLabel('线索 ID', { exact: true }).fill(id)
    await page.getByRole('button', { name: '查询', exact: true }).click()
    await expect(page.locator('.audit-results')).toContainText(`线索 ${id}`)
    await expect(page.locator('.audit-results')).not.toContainText('13999995678')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
    await page.screenshot({ path: `audit-evidence/repair-access-${width}.png`, fullPage: true })
  })
}
