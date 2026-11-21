import { test, expect } from '@playwright/test'
import { randomUUID } from 'node:crypto'

for (const user of ['inviter_001', 'closer_001']) {
  test(`committed followup response can be safely retried by ${user}`, async ({ page }) => {
    await page.request.get('/test-session?user=admin')
    let id
    if (user === 'closer_001') {
      const list = await (await page.request.get('/api/leads?page_size=100')).json()
      id = (list.leads ?? list.Leads).find(lead => lead.lead_no === 'FOLLOWUP-RETRY-CLOSER').id
    } else {
      const created = await page.request.post('/api/leads', {
        headers: { 'Idempotency-Key': randomUUID() },
        data: { customer_name: `跟进响应丢失${user}`, customer_mobile: `139${String(Date.now() % 100000000).padStart(8, '0')}`, source_channel: '好学校' }
      })
      expect(created.status()).toBe(201)
      id = (await created.json()).lead.id
      expect((await page.request.post(`/api/leads/${id}/assign`, { data: { sales_userid: user } })).status()).toBe(200)
    }
    const path = `/api/leads/${id}`
    await page.request.get(`/test-session?user=${user}`)
    if (user !== 'closer_001') expect((await page.request.post(`${path}/claim`)).status()).toBe(200)
    const keys = []
    let first = true
    await page.route(`**${path}/followups`, async route => {
      keys.push(route.request().headers()['idempotency-key'])
      if (first) {
        first = false
        const committed = await route.fetch()
        expect(committed.status()).toBe(200)
        return route.abort('failed')
      }
      return route.continue()
    })
    await page.goto(`/leads/${id}/followup`)
    await expect(page.getByRole('region', { name: '当前跟进客户' })).toContainText(user === 'closer_001' ? '跟进响应丢失谈单' : `跟进响应丢失${user}`)
    await page.locator('textarea').fill('真实隔离接口：响应丢失后仍只记一次跟进')
    await page.locator('button[type=submit]').click()
    await expect(page.getByRole('alert')).toContainText('尚未确认提交结果')
    const committed = await (await page.request.get(path)).json()
    expect(committed.followups).toHaveLength(1)
    await page.locator('button[type=submit]').click()
    await expect(page).toHaveURL(new RegExp(`/leads/${id}$`))
    const replayed = await (await page.request.get(path)).json()
    expect(replayed.followups).toHaveLength(1)
    expect(replayed.followups[0].id).toBe(committed.followups[0].id)
    expect(replayed.lead.version).toBe(committed.lead.version)
    expect(replayed.lead.last_followup_at).toBe(committed.lead.last_followup_at)
    expect(keys).toHaveLength(2)
    expect(keys[0]).toBe(keys[1])
    expect(keys[0]).toMatch(/^[\da-f]{8}-[\da-f]{4}-4[\da-f]{3}-[89ab][\da-f]{3}-[\da-f]{12}$/)
  })
}
