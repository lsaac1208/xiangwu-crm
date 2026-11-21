import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e', outputDir: 'regression-results',
  // The AI conversation UI is withdrawn until its redesign; statistics replaces its active acceptance suite.
  testMatch: ['followup-reliability.spec.js', 'repair-safety.spec.js', 'lead-corrections.spec.js', 'manager-transfers.spec.js', 'lead-sources.spec.js', 'account-security.spec.js', 'lead-hardening.spec.js', 'appointment-hardening.spec.js', 'saved-views.spec.js', 'lead-recycle.spec.js', 'statistics.spec.js'],
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:4181', channel: 'chrome', locale: 'zh-CN', timezoneId: 'Asia/Shanghai',
    reducedMotion: 'reduce', screenshot: 'only-on-failure', trace: 'retain-on-failure'
  },
  webServer: {
    command: 'cd .. && GOTOOLCHAIN=go1.26.10 KXONE_BROWSER_REGRESSION=1 go test ./cmd/server -run ^TestBrowserRegressionServer$ -v -timeout=15m',
    url: 'http://127.0.0.1:4181/test-session', timeout: 120_000
  }
})
