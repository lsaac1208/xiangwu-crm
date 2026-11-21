import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import path from 'node:path'
import { json, mockAPI, user } from './crm-fixtures.js'

// All records are isolated browser fixtures. These tests open and cancel dialogs;
// any write request is rejected and fails the test instead of touching a server.
const writes = new WeakMap()
const axePath = createRequire(import.meta.url).resolve('axe-core/axe.min.js')
const staff = [
  { id: 7, username: 'inviter.mei', employee_name: '员工己', wecom_userid: 'fixture-inviter', role: 'inviter', status: 'active', department_id: 1, group_robot_mention_userid: 'fixture-inviter', version: 2 },
  { id: 8, username: 'closer.zhao', employee_name: '赵老师', wecom_userid: 'fixture-closer', role: 'closer', status: 'active', department_id: 1, version: 1 },
  { id: 9, username: 'closer.chen', employee_name: '陈老师', wecom_userid: 'fixture-closer-two', role: 'closer', status: 'active', department_id: 1, version: 1 }
]
const scheduled = {
  id: 3, lead_id: 28, lead_no: 'KX202608270028', customer_name: '张若溪',
  start_at: '2026-08-27T09:30:00+08:00', end_at: '2026-08-27T11:00:00+08:00',
  campus: '襄阳校区', visitor_count: 2, inviter_name: '员工己', inviter_userid: 'fixture-inviter',
  closer_name: '赵老师', closer_userid: 'fixture-closer', status: 'scheduled', version: 1,
  handoff_notes: '家长关注课程安排及文化课提分计划。'
}
const pending = {
  ...scheduled, id: 4, lead_id: 29, lead_no: 'KX202608270029', customer_name: '李女士',
  start_at: '2026-08-27T14:00:00+08:00', end_at: '2026-08-27T15:00:00+08:00',
  closer_name: '', closer_userid: '', status: 'pending_assignment'
}

async function prepareFixtures(page) {
  await mockAPI(page)
  writes.set(page, [])
  await page.route('**/api/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (!['GET', 'HEAD'].includes(request.method())) {
      writes.get(page).push(`${request.method()} ${url.pathname}`)
      return json(route, { message: 'Visual overlay tests must not submit changes' }, 405)
    }
    const pathname = url.pathname.replace(/^\/api/, '')
    if (pathname === '/user/info') return json(route, user)
    if (pathname === '/admin/users') return json(route, { bindings: staff, total: staff.length, total_pages: 1 })
    if (pathname === '/users/inviters') return json(route, { bindings: staff.filter(person => person.role === 'inviter') })
    if (pathname === '/users/closers') return json(route, { bindings: staff.filter(person => person.role === 'closer') })
    if (pathname === '/appointments') return json(route, { appointments: [pending, scheduled] })
    if (pathname === '/appointments/conflicts') return json(route, { conflicts: [] })
    if (pathname === '/closer-availability') return json(route, { availability: [{
      id: 2, closer_userid: 'fixture-closer', kind: 'working', capacity: 1, campus: '襄阳校区',
      start_at: '2026-08-27T09:00:00+08:00', end_at: '2026-08-27T18:00:00+08:00', reason: '常规接待时间'
    }] })
    return route.fallback()
  })
}

const popupWithHeading = (page, title) => page.locator('.van-popup:visible').filter({ has: page.getByRole('heading', { name: title, exact: true }) })

async function expectReachableSubmit(overlay, button) {
  await button.scrollIntoViewIfNeeded()
  await expect(button).toBeInViewport({ ratio: 1 })
  // Long forms may scroll inside a bounded dialog; return to its first fields
  // for a consistent review image after proving that the submit stays reachable.
  await overlay.evaluate(element => {
    element.scrollTop = 0
    for (const child of element.querySelectorAll('*')) {
      if (child.scrollTop) child.scrollTop = 0
    }
  })
}

async function captureOverlay(page, overlay, name, info) {
  await expect(overlay).toBeVisible()
  await page.waitForLoadState('networkidle')
  await page.evaluate(() => document.fonts.ready)
  await overlay.evaluate(async element => {
    await Promise.all(element.getAnimations({ subtree: true }).map(animation => animation.finished.catch(() => {})))
  })
  await expect(overlay).toBeInViewport({ ratio: 1 })
  expect(await overlay.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  await page.addScriptTag({ path: axePath })
  const violations = await page.evaluate(async () => (await window.axe.run(document, {
    runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] }
  })).violations.map(violation => ({
    id: violation.id,
    nodes: violation.nodes.map(node => ({ target: node.target, summary: node.failureSummary }))
  })))
  await info.attach(`${name}-accessibility`, { body: JSON.stringify(violations, null, 2), contentType: 'application/json' })
  expect(violations, 'Opened overlay must pass WCAG A/AA checks').toEqual([])
  const options = { animations: 'disabled', fullPage: false }
  // Review candidates before accepting them into the normal Playwright baseline.
  // The same tests compare approved snapshots when this variable is omitted.
  if (process.env.KX_VISUAL_CANDIDATE_DIR) {
    const directory = path.resolve(process.env.KX_VISUAL_CANDIDATE_DIR)
    await mkdir(directory, { recursive: true })
    const target = path.join(directory, path.basename(info.snapshotPath(name)))
    await page.screenshot({ ...options, path: target })
    await info.attach(name, { path: target, contentType: 'image/png' })
  } else {
    await expect(page).toHaveScreenshot(name, options)
  }
}

async function closeAndRestoreFocus(page, overlay, trigger, closeButton) {
  if (closeButton) await closeButton.click()
  else await page.keyboard.press('Escape')
  await expect(overlay).toBeHidden()
  await expect(trigger).toBeFocused()
}

for (const width of [375, 1440]) {
  test.describe(`Apple overlay coverage ${width}px`, () => {
    test.use({ viewport: { width, height: width === 375 ? 812 : 900 } })
    test.beforeEach(async ({ page }) => { await prepareFixtures(page) })
    test.afterEach(async ({ page }) => { expect(writes.get(page) || []).toEqual([]) })

    test('settings create user', async ({ page }, info) => {
      await page.goto('/admin/settings?section=users')
      const trigger = page.getByRole('button', { name: /新增用户$/ })
      await trigger.click()
      const popup = popupWithHeading(page, '新增用户')
      await expect(popup.getByLabel('登录账号', { exact: true })).toBeVisible()
      await expectReachableSubmit(popup, popup.getByRole('button', { name: '保存用户', exact: true }))
      await captureOverlay(page, popup, `settings-create-user-${width}.png`, info)
      await closeAndRestoreFocus(page, popup, trigger)
    })

    test('settings edit user', async ({ page }, info) => {
      await page.goto('/admin/settings?section=users')
      const trigger = page.getByRole('button', { name: '编辑用户 员工己', exact: true })
      await trigger.click()
      const popup = popupWithHeading(page, '编辑用户')
      await expect(popup.getByLabel('登录账号', { exact: true })).toHaveValue('inviter.mei')
      await expect(popup.getByLabel('登录密码', { exact: true })).toHaveAttribute('placeholder', '留空表示不修改')
      await expectReachableSubmit(popup, popup.getByRole('button', { name: '保存用户', exact: true }))
      await captureOverlay(page, popup, `settings-edit-user-${width}.png`, info)
      await closeAndRestoreFocus(page, popup, trigger)
    })

    test('settings channel account', async ({ page }, info) => {
      await page.goto('/admin/settings?section=channels')
      const trigger = page.getByRole('button', { name: '新增账户', exact: true })
      await trigger.click()
      const popup = popupWithHeading(page, '新增平台账户')
      await popup.getByRole('radio', { name: /抖音$/ }).click()
      await expect(popup.getByLabel('密钥', { exact: true })).toBeVisible()
      await expect(popup).toContainText('平台兼容模式')
      await popup.getByLabel('密钥', { exact: true }).fill('feiyu-secret')
      await expect(popup).toContainText('飞鱼 SHA256 签名')
      await popup.getByLabel('密钥', { exact: true }).fill('')
      await popup.getByRole('radio', { name: /小红书$/ }).click()
      await expect(popup.getByLabel('Token', { exact: true })).toBeVisible()
      await expectReachableSubmit(popup, popup.getByRole('button', { name: '保存平台账户', exact: true }))
      await captureOverlay(page, popup, `settings-channel-${width}.png`, info)
      await closeAndRestoreFocus(page, popup, trigger)
    })

    for (const scenario of [
      { name: 'assign', card: '.pending-card', button: '分配谈单师', heading: '分配谈单师', submit: '确认安排' },
      { name: 'reassign', card: '.scheduled-card', button: '换人', heading: '更换谈单师', submit: '确认安排' },
      { name: 'reschedule', card: '.scheduled-card', button: '改期', heading: '预约改期', submit: '确认改期' },
      { name: 'cancel', card: '.scheduled-card', button: '取消', heading: '取消预约', submit: '确认提交' },
      { name: 'no-show', card: '.scheduled-card', button: '爽约', heading: '登记客户爽约', submit: '确认提交' },
      { name: 'arrival', card: '.scheduled-card', button: '确认到访并交付', heading: '确认到访并交付', submit: '确认客户已到访' }
    ]) {
      test(`appointment ${scenario.name}`, async ({ page }, info) => {
        await page.goto('/appointments')
        await page.getByRole('button', { name: '日视图', exact: true }).click()
        const trigger = page.locator(scenario.card).getByRole('button', { name: scenario.button, exact: true })
        await trigger.click()
        const popup = popupWithHeading(page, scenario.heading)
        if (scenario.name === 'assign') {
          await popup.getByRole('radio', { name: '赵老师', exact: true }).click()
          await expect(popup.getByRole('button', { name: scenario.submit, exact: true })).toBeEnabled()
        }
        if (scenario.name === 'reassign') {
          await expect(popup.getByRole('radio', { name: '赵老师', exact: true })).toBeChecked()
          await expect(popup.getByRole('button', { name: scenario.submit, exact: true })).toBeEnabled()
        }
        await expect(popup.getByRole('button', { name: scenario.submit, exact: true })).toBeVisible()
        await captureOverlay(page, popup, `appointment-${scenario.name}-${width}.png`, info)
        await closeAndRestoreFocus(page, popup, trigger, popup.getByRole('button', { name: '关闭', exact: true }))
      })
    }

    test('appointment availability', async ({ page }, info) => {
      await page.goto('/appointments')
      const trigger = page.getByRole('button', { name: '可用时间', exact: true })
      await trigger.click()
      const popup = popupWithHeading(page, '设置谈单师时间')
      await expect(popup.getByLabel('开始时间', { exact: true })).toBeVisible()
      await expect(popup.getByText('当前范围内的时间设置', { exact: true })).toBeVisible()
      await expectReachableSubmit(popup, popup.getByRole('button', { name: '保存时间设置', exact: true }))
      await captureOverlay(page, popup, `appointment-availability-${width}.png`, info)
      await closeAndRestoreFocus(page, popup, trigger, popup.getByRole('button', { name: '关闭', exact: true }))
    })

    test('analysis preferences drawer', async ({ page }, info) => {
      await page.goto('/agent')
      const trigger = page.getByRole('button', { name: '设置', exact: true })
      await trigger.click()
      const popup = page.getByRole('dialog', { name: '分析设置', exact: true })
      await expect(popup.getByLabel('我的分析偏好')).toBeVisible()
      await captureOverlay(page, popup, `agent-preferences-${width}.png`, info)
      await closeAndRestoreFocus(page, popup, trigger, popup.getByRole('button', { name: '关闭分析设置', exact: true }))
    })

    test('analysis model settings drawer', async ({ page }, info) => {
      await page.goto('/agent')
      const trigger = page.getByRole('button', { name: '设置', exact: true })
      await trigger.click()
      const popup = page.getByRole('dialog', { name: '分析设置', exact: true })
      await popup.getByRole('button', { name: '模型与预算', exact: true }).click()
      await expect(popup.getByLabel('API 服务地址')).toBeVisible()
      await expectReachableSubmit(popup, popup.getByRole('button', { name: '保存 AI 配置', exact: true }))
      await captureOverlay(page, popup, `agent-model-settings-${width}.png`, info)
      await closeAndRestoreFocus(page, popup, trigger, popup.getByRole('button', { name: '关闭分析设置', exact: true }))
    })

    test('followup date and time picker', async ({ page }, info) => {
      await page.goto('/leads/28/followup')
      const trigger = page.getByLabel('下次跟进时间', { exact: true })
      await trigger.click()
      const popup = page.locator('.van-popup:visible').filter({ has: page.getByText('选择下次跟进时间', { exact: true }) })
      await expect(popup.getByRole('button', { name: '确认', exact: true })).toBeVisible()
      await captureOverlay(page, popup, `followup-datetime-${width}.png`, info)
      const columns = popup.locator('.van-picker-column')
      const nextHour = columns.nth(3).getByRole('button', { name: '11', exact: true })
      await nextHour.focus()
      await nextHour.press('Enter')
      await expect(nextHour).toHaveClass(/van-picker-column__item--selected/)
      const nextMinute = columns.nth(4).getByRole('button', { name: '31', exact: true })
      await nextMinute.focus()
      await nextMinute.press('Space')
      await expect(nextMinute).toHaveClass(/van-picker-column__item--selected/)
      await closeAndRestoreFocus(page, popup, trigger, popup.getByRole('button', { name: '取消', exact: true }))
      await expect(trigger).toContainText('选择下次跟进时间')
    })
  })
}
