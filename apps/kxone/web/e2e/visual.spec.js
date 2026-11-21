import { expect, test } from '@playwright/test'

import { mockAPI, json, user, poolLeads } from './crm-fixtures'

const pages = [
  ['login', '/login'],
  ['leads', '/leads'],
  ['lead-detail', '/leads/28'],
  ['settings', '/admin/settings?section=notifications'],
  ['appointments', '/appointments'],
  ['public-pool', '/public-pool'],
  ['agent', '/agent']
]

const supportingPages = [
  ['create-lead', '/leads/new'],
  ['add-followup', '/leads/28/followup'],
  ['create-appointment', '/leads/28/appointment'],
  ['change-password', '/account/password'],
  ['not-found', '/missing-page-for-layout-audit']
]

const layoutPages = [
  ...pages,
  ['create-lead', '/leads/new'],
  ['add-followup', '/leads/28/followup'],
  ['create-appointment', '/leads/28/appointment'],
  ['change-password', '/account/password'],
  ['not-found', '/missing-page-for-layout-audit']
]

async function unexpectedLayoutOverflow(page) {
  return page.evaluate(() => {
    const allowedScrollContainers = new Set([
      'lead-table-scroll',
      'pool-table-scroll',
      'day-grid--week',
      'settings-table-scroll',
      'lead-mobile-filter-labels',
      'detail-tabs', 'agent-table-scroll'
    ])
    const isAllowedScroller = element => {
      let current = element
      while (current && current !== document.documentElement) {
        if ([...current.classList].some(name => allowedScrollContainers.has(name))) return true
        current = current.parentElement
      }
      return false
    }
    const label = element => {
      const classes = [...element.classList].slice(0, 3).join('.')
      return `${element.tagName.toLowerCase()}${element.id ? `#${element.id}` : ''}${classes ? `.${classes}` : ''}`
    }
    const issues = []
    if (document.documentElement.scrollWidth > document.documentElement.clientWidth + 1) {
      issues.push(`document:${document.documentElement.scrollWidth}/${document.documentElement.clientWidth}`)
    }
    for (const element of document.querySelectorAll('main, section, header, nav, form, button, select, input, [class*="toolbar"], [class*="actions"]')) {
      const style = getComputedStyle(element)
      const rect = element.getBoundingClientRect()
      if (style.display === 'none' || style.visibility === 'hidden' || rect.width < 1 || rect.height < 1) continue
      if (isAllowedScroller(element)) continue
      if (rect.left < -1 || rect.right > window.innerWidth + 1) issues.push(`${label(element)}:viewport:${Math.round(rect.left)}-${Math.round(rect.right)}`)
      const canScroll = ['auto', 'scroll'].includes(style.overflowX)
      const intentionalClip = ['hidden', 'clip'].includes(style.overflowX) && ['ellipsis', 'clip'].includes(style.textOverflow)
      if (!canScroll && !intentionalClip && element.scrollWidth > element.clientWidth + 1) {
        issues.push(`${label(element)}:content:${element.scrollWidth}/${element.clientWidth}`)
      }
      if (['BUTTON', 'SELECT', 'INPUT'].includes(element.tagName) && element.scrollHeight > element.clientHeight + 1) {
        issues.push(`${label(element)}:vertical-content:${element.scrollHeight}/${element.clientHeight}`)
      }
    }
    return [...new Set(issues)].slice(0, 30)
  })
}

for (const viewport of [{ width: 320, height: 720 }, { width: 375, height: 812 }, { width: 1024, height: 768 }, { width: 1440, height: 900 }]) {
  test.describe(`${viewport.width}px visual baseline`, () => {
    test.use({ viewport })
    for (const [name, path] of pages) {
      test(name, async ({ page }) => {
        await mockAPI(page)
        await page.goto(path)
        await page.evaluate(() => document.fonts?.ready)
        await page.waitForLoadState('networkidle')
        await expect(page).toHaveScreenshot(`${name}-${viewport.width}.png`, { animations: 'disabled', fullPage: true })
      })
    }
  })
}

test('weak network keeps a visible loading state and recovers', async ({ page }) => {
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/user/info') return json(route, user)
    await new Promise(resolve => setTimeout(resolve, 600))
    if (path === '/api/public-pool') return json(route, { leads: poolLeads, total: 1, total_pages: 1 })
    return json(route, {})
  })
  await page.goto('/public-pool')
  await expect(page.getByText('正在加载公海线索')).toBeVisible()
  await expect(page.getByText('周同学').first()).toBeVisible()
})

test('lead list stays within CLS and LCP budgets', async ({ page }) => {
  await page.addInitScript(() => {
    window.__kxMetrics = { cls: 0, lcp: 0 }
    new PerformanceObserver(list => {
      for (const entry of list.getEntries()) {
        if (!entry.hadRecentInput) window.__kxMetrics.cls += entry.value
      }
    }).observe({ type: 'layout-shift', buffered: true })
    new PerformanceObserver(list => {
      const entries = list.getEntries()
      window.__kxMetrics.lcp = entries.at(-1)?.startTime || 0
    }).observe({ type: 'largest-contentful-paint', buffered: true })
  })
  await mockAPI(page)
  await page.goto('/leads')
  await expect(page.getByText('张若溪').first()).toBeVisible()
  await page.waitForTimeout(500)
  const metrics = await page.evaluate(() => window.__kxMetrics)
  expect(metrics.cls).toBeLessThanOrEqual(0.1)
  expect(metrics.lcp).toBeLessThanOrEqual(2500)
})

for (const viewport of [
  { width: 320, height: 720 },
  { width: 375, height: 812 },
  { width: 767, height: 900 },
  { width: 768, height: 900 },
  { width: 1023, height: 900 },
  { width: 1024, height: 900 },
  { width: 1280, height: 900 },
  { width: 1440, height: 900 },
  { width: 1920, height: 1080 }
]) {
  test(`key pages have no unexpected internal overflow at ${viewport.width}px`, async ({ page }) => {
    await page.setViewportSize(viewport)
    await mockAPI(page)
    for (const [, path] of layoutPages) {
      await page.goto(path)
      await page.evaluate(() => document.fonts?.ready)
      await expect.poll(() => unexpectedLayoutOverflow(page), { message: `${path} contains clipped or off-screen controls` }).toEqual([])
    }
  })
}

for (const width of [375, 1440]) {
  test.describe(`${width}px extended visual coverage`, () => {
    test.use({ viewport: { width, height: 900 } })
    for (const [name, path] of supportingPages) {
      test(name, async ({ page }) => {
        await mockAPI(page)
        await page.goto(path)
        await page.evaluate(() => document.fonts.ready)
        await page.waitForLoadState('networkidle')
        await expect(page).toHaveScreenshot(`${name}-${width}.png`, { animations: 'disabled', fullPage: true })
      })
    }
    test('selected leads and lifecycle overlays', async ({ page }) => {
      await mockAPI(page)
      await page.goto('/leads')
      await page.getByRole('checkbox', { name: '选择线索 KX202608270029', exact: true }).check()
      await expect(page.getByRole('region', { name: '批量操作' })).toBeVisible()
      await page.waitForLoadState('networkidle')
        await expect(page).toHaveScreenshot(`batch-${width}.png`, { animations: 'disabled', fullPage: true })
      await page.goto('/leads/28')
      if (width < 768) await page.getByRole('button', { name: '更多线索操作' }).click()
      else await page.locator('.more-actions-toggle').click()
      await page.getByRole('button', { name: '删除线索', exact: true }).click()
      await expect(page.locator('.delete-lead-popup')).toBeInViewport({ ratio: 1 })
      await page.waitForLoadState('networkidle')
        await expect(page).toHaveScreenshot(`delete-${width}.png`, { animations: 'disabled', fullPage: false })
      await page.route('**/api/admin/deleted-leads?**', route => json(route, { items: [{ id: 28, lead_no: 'KX202608270028', customer_name: '视觉测试客户', source_channel: '官网报名咨询', deleted_at: '2026-08-27T10:00:00+08:00', deleted_by: '测试管理员', reason: '隔离环境视觉验收记录' }], total: 1 }))
      await page.goto('/admin/settings?section=recycle')
      await expect(page.getByRole('button', { name: '恢复线索', exact: true })).toBeVisible()
      await page.waitForLoadState('networkidle')
        await expect(page).toHaveScreenshot(`recycle-${width}.png`, { animations: 'disabled', fullPage: true })
      await page.getByRole('button', { name: '恢复线索', exact: true }).click()
      await expect(page.getByRole('dialog', { name: '恢复线索', exact: true })).toBeInViewport({ ratio: 1 })
      await page.waitForLoadState('networkidle')
        await expect(page).toHaveScreenshot(`restore-${width}.png`, { animations: 'disabled', fullPage: false })
    })
  })
}
