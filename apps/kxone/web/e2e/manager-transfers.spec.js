import { randomUUID } from 'node:crypto'
import { test, expect } from '@playwright/test'

test('主管批量分配后接收主管可将整批转回', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.request.get('/test-session')
  const leads = []
  for (const suffix of ['甲', '乙']) {
    const response = await page.request.post('/api/leads', { headers: { 'Idempotency-Key': randomUUID() }, data: { customer_name: `批量主管交接${suffix}`, customer_mobile: '13912345678', city: '襄阳', source_channel: '主动来电（员工丙）' } })
    expect(response.ok(), await response.text()).toBeTruthy()
    leads.push((await response.json()).lead)
  }
  for (const [actor, target, action] of [['manager_001', 'manager_002', '批量分配'], ['manager_002', 'manager_001', '批量转派']]) {
    await page.request.get(`/test-session?user=${actor}`)
    await page.goto('/leads?status=all&keyword=批量主管交接')
    for (const lead of leads) await page.getByRole('checkbox', { name: `选择线索 ${lead.lead_no}`, exact: true }).check()
    await page.getByLabel('批量操作目标人员').selectOption(target)
    await page.getByRole('button', { name: action, exact: true }).click()
    await page.getByRole('button', { name: '确认', exact: true }).click()
    await expect(page.getByRole('region', { name: '批量操作' })).toBeHidden()
    for (const lead of leads) {
      const detail = await (await page.request.get(`/api/leads/${lead.id}`)).json()
      expect(detail.lead.dispatch_manager_userid).toBe(target)
      expect(detail.lead.assigned_inviter_userid).toBe(target)
      expect(detail.lead.status).toBe('assigned_pending_claim')
      expect(detail.dispatch_records).toHaveLength(action === '批量分配' ? 1 : 2)
    }
  }
})
