import { randomUUID } from 'node:crypto'
import { test, expect } from '@playwright/test'

for (const width of [375, 1440]) {
  test(`跟进保留五颗星且显式改级才更新 ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.request.get('/test-session')
    const created = await page.request.post('/api/leads', { headers: { 'Idempotency-Key': randomUUID() }, data: { customer_name: `评级保留${width}`, customer_mobile: '13912345678', source_channel: '好学校', star_rating: 5 } })
    expect(created.ok()).toBeTruthy()
    const lead = (await created.json()).lead
    await page.request.post(`/api/leads/${lead.id}/assign`, { data: { sales_userid: 'manager_001' } })
    await page.request.post(`/api/leads/${lead.id}/claim`)
    await page.goto(`/leads/${lead.id}/followup`)
    await expect(page.getByRole('radio', { name: '5 星', exact: true })).toBeChecked()
    await page.getByLabel('跟进内容').fill('只填写内容，保留五颗星')
    const submission = page.waitForRequest(req => req.method() === 'POST' && req.url().endsWith('/followups'))
    await page.getByRole('button', { name: '提交邀约跟进', exact: true }).click()
    const posted = (await submission).postDataJSON()
    expect(posted).not.toHaveProperty('star_rating')
    await expect(page).toHaveURL(new RegExp(`/leads/${lead.id}$`))
    expect((await (await page.request.get(`/api/leads/${lead.id}`)).json()).lead.star_rating).toBe(5)
    await page.goto(`/leads/${lead.id}/followup`)
    await page.getByRole('radio', { name: '4 星', exact: true }).check()
    await page.getByLabel('跟进内容').fill('明确调整为四颗星')
    await page.getByRole('button', { name: '提交邀约跟进', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/leads/${lead.id}$`))
    expect((await (await page.request.get(`/api/leads/${lead.id}`)).json()).lead.star_rating).toBe(4)
  })
}

test('指定负责人在非全部状态和日期筛选中保持', async ({ page }) => {
  await page.request.get('/test-session')
  const requests = []
  page.on('request', req => { if (new URL(req.url()).pathname === '/api/leads') requests.push(new URL(req.url())) })
  await page.goto('/leads?status=claimed&sales_userid=manager_002&start_date=2026-01-01&end_date=2026-12-31&sort_by=created_at&sort_order=asc')
  await expect.poll(() => requests.length).toBeGreaterThan(0)
  expect(requests[0].searchParams.get('sales_userid')).toBe('manager_002')
  expect(requests[0].searchParams.get('start_date')).toBe('2026-01-01')
  expect(requests[0].searchParams.get('sort_by')).toBe('created_at')
})

test('资料冲突保留修改并合并未改字段的最新值', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.request.get('/test-session')
  const response = await page.request.post('/api/leads', { headers: { 'Idempotency-Key': randomUUID() }, data: { customer_name: '资料冲突验证', customer_mobile: '13912345678', city: '原城市', source_channel: '好学校' } })
  const { lead } = await response.json()
  await page.goto(`/leads/${lead.id}`)
  await page.getByRole('button', { name: '编辑资料', exact: true }).click()
  const popup = page.locator('.profile-editor')
  await popup.getByLabel('客户姓名', { exact: true }).fill('本地保留姓名')
  const changed = await page.request.patch(`/api/leads/${lead.id}/profile`, { data: { version: lead.version, profile_version: lead.profile_version, city: '其他人新城市' } })
  expect(changed.ok(), await changed.text()).toBeTruthy()
  await popup.getByRole('button', { name: '保存资料', exact: true }).click()
  await expect(popup.getByRole('alert')).toContainText('输入已保留')
  await expect(popup.getByLabel('客户姓名', { exact: true })).toHaveValue('本地保留姓名')
  await popup.getByRole('button', { name: '刷新资料版本（保留输入）' }).click()
  await expect(popup.getByLabel('城市', { exact: true })).toHaveValue('其他人新城市')
  await popup.getByRole('button', { name: '保存资料', exact: true }).click()
  await expect(popup).toBeHidden()
  const final = await (await page.request.get(`/api/leads/${lead.id}`)).json()
  expect(final.lead.customer_name).toBe('本地保留姓名')
  expect(final.lead.city).toBe('其他人新城市')
})
