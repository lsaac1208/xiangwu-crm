import { test, expect } from '@playwright/test'
import { mockAPI } from './crm-fixtures.js'

for (const mode of ['modern', 'fallback', 'nomodule']) {
  test(`strict CSP supports ${mode} loading and embedded icons`, async ({ page }) => {
    await mockAPI(page)
    const errors = [], requests = [], violations = []
    await page.exposeFunction('recordCspViolation', event => violations.push(event))
    await page.addInitScript(() => {
      document.addEventListener('securitypolicyviolation', event => window.recordCspViolation({
        directive: event.violatedDirective, blocked: event.blockedURI
      }))
    })
    page.on('pageerror', error => errors.push(error.message))
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
    page.on('request', request => requests.push(request.url()))
    await page.route('**/*', async route => {
      if (route.request().resourceType() !== 'document') return route.fallback()
      const response = await route.fetch()
      let body = await response.text()
      if (mode === 'nomodule') body = body.replace(/<script type="module"[^>]*>[\s\S]*?<\/script>/g, '').replace(/ nomodule/g, '')
      await route.fulfill({ response, body, headers: {
        ...response.headers(),
        'content-security-policy': "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self' data:; img-src 'self' data: blob:; connect-src 'self'"
      } })
    })
    if (mode === 'fallback') {
      await page.route('**/browser-support-*.js?*', route => route.fulfill({
        contentType: 'text/javascript', body: 'throw Error("Simulated unsupported import.meta.resolve")'
      }))
    }
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible()
    await expect(page.getByRole('button', { name: '登录', exact: true })).toBeEnabled()
    expect(await page.locator('script:not([src])').count()).toBe(0)
    expect(requests.some(url => url.includes('polyfills-legacy'))).toBe(mode !== 'modern')
    expect(requests.some(url => url.includes('at.alicdn.com'))).toBe(false)
    expect(violations).toEqual([])
    expect(errors.filter(error => mode !== 'fallback' || !error.includes('Simulated unsupported import.meta.resolve'))).toEqual([])
    await page.goto('/leads/28')
    await expect(page.locator('h1').first()).toBeVisible()
    await page.evaluate(() => document.fonts.ready)
    expect(await page.evaluate(() => {
      const fonts = [...document.fonts].filter(font => font.family === 'vant-icon')
      return fonts.length > 0 && fonts.every(font => font.status === 'loaded')
    })).toBe(true)
    expect(violations).toEqual([])
  })
}
