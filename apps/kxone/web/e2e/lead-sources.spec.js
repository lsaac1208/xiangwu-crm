import { test, expect } from '@playwright/test'

const sources = [
  '好学校', '主动来电（员工丙）', '自己上门', '转介绍',
  '襄五抖音（员工甲）', '襄五视频号（员工甲）', '襄五小红书（员工甲/员工丙）',
  '襄武抖音（员工乙）', '襄武视频号（员工乙）', '襄武小红书（员工乙/员工丙）',
  '员工庚抖音（员工丁）', '员工庚视频号（员工丁）', '员工庚小红书（员工丁）',
  '员工己抖音（员工乙）', '员工己视频号（员工乙）', '员工己小红书（员工乙）',
  '员工丁抖音（员工乙）', '员工丁视频号（员工乙）', '员工丁小红书（员工乙）',
  '员工己2抖音（员工甲）', '员工己2视频号（员工甲）', '员工己2小红书（员工甲）',
  '员工戊抖音（员工甲）', '员工戊视频号（员工甲）', '员工戊小红书（员工甲）'
]

for (const width of [375, 1440]) {
  test(`来源必选、新建、更换及历史空来源补填 ${width}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.request.get('/test-session')
    await page.goto('/leads/new')
    await page.getByLabel('客户姓名', { exact: true }).fill(`新来源客户${width}`)
    await page.getByLabel('联系电话', { exact: true }).fill('13912345678')
    const sourceField = page.getByRole('combobox', { name: '来源渠道', exact: true })
    expect(await sourceField.locator('option').allTextContents()).toEqual(['请选择来源渠道', ...sources])
    await page.getByRole('button', { name: '创建线索并查看详情' }).click()
    await expect(page.locator('.van-field__error-message').filter({ hasText: '请选择来源渠道' })).toBeVisible()
    await expect(page).toHaveURL(/\/leads\/new$/)
    await sourceField.selectOption('主动来电（员工丙）')
    await page.getByRole('button', { name: '创建线索并查看详情' }).click()
    await expect(page).toHaveURL(/\/leads\/\d+$/)
    const id = new URL(page.url()).pathname.split('/').pop()
    let before = (await (await page.request.get(`/api/leads/${id}`)).json()).lead
    expect(before.source_channel).toBe('主动来电（员工丙）')
    const statsResponse = await page.request.get('/api/stats/management')
    expect(statsResponse.ok()).toBeTruthy()
    const stats = await statsResponse.json()
    expect(stats.sources.find(item => item.source === '主动来电（员工丙）')?.total).toBeGreaterThan(0)
    expect(stats.sources.find(item => item.source === '主动来电（老王）')?.total).toBeGreaterThan(0)
    await page.getByRole('button', { name: '编辑资料', exact: true }).click()
    await page.getByRole('combobox', { name: '来源渠道' }).selectOption('转介绍')
    await page.getByRole('button', { name: '保存资料', exact: true }).click()
    await expect(page.getByRole('button', { name: '保存资料', exact: true })).toBeHidden()
    await page.reload()
    let detail = await (await page.request.get(`/api/leads/${id}`)).json()
    expect(detail.lead.source_channel).toBe('转介绍')
    expect(detail.lead.version).toBe(before.version)
    expect(detail.lead.profile_version).toBe(before.profile_version + 1)
    expect(detail.lead.status).toBe(before.status)
    await page.getByRole('tab', { name: '系统信息', exact: true }).click()
    await expect(page.locator('.system-info-grid').getByText('转介绍', { exact: true })).toBeVisible()

    const legacy = (await (await page.request.get(`/api/leads?keyword=${encodeURIComponent(`补来源${width}`)}`)).json()).Leads
    const empty = legacy.find(lead => !lead.source_channel)
    expect(empty).toBeTruthy()
    await page.goto(`/leads/${empty.id}`)
    await page.getByRole('tab', { name: '系统信息', exact: true }).click()
    await expect(page.locator('.system-info-grid').getByText('来源未填写', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '编辑资料', exact: true }).click()
    await page.getByRole('combobox', { name: '来源渠道' }).selectOption(sources[11])
    await page.screenshot({ path: info.outputPath(`source-edit-${width}.png`), fullPage: true, animations: 'disabled' })
    await page.getByRole('button', { name: '保存资料', exact: true }).click()
    await expect(page.getByRole('button', { name: '保存资料', exact: true })).toBeHidden()
    await page.reload()
    detail = await (await page.request.get(`/api/leads/${empty.id}`)).json()
    expect(detail.lead.source_channel).toBe(sources[11])
    expect(detail.activities.some(item => JSON.parse(item.to_value_json || '{}').source_channel === sources[11])).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1)

    const old = legacy.find(lead => lead.source_channel === '主动来电（老王）')
    expect(old).toBeTruthy()
    await page.goto(`/leads/${old.id}`)
    await page.getByRole('button', { name: '编辑资料', exact: true }).click()
    await expect(page.getByRole('combobox', { name: '来源渠道' })).toHaveValue('主动来电（老王）')
    await page.getByLabel('城市', { exact: true }).fill('武汉')
    await page.getByRole('button', { name: '保存资料', exact: true }).click()
    await expect(page.getByRole('button', { name: '保存资料', exact: true })).toBeHidden()
    detail = await (await page.request.get(`/api/leads/${old.id}`)).json()
    expect(detail.lead.source_channel).toBe('主动来电（老王）')
  })
}
