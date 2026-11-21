import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './e2e', testMatch: ['repair-20261026.spec.js'], outputDir: 'test-results/repair-20261026',
  workers: 1, timeout: 30000, reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:4173', locale: 'zh-CN', timezoneId: 'Asia/Shanghai', screenshot: 'only-on-failure' },
  projects: [{ name: 'chrome', use: { browserName: 'chromium', channel: 'chrome' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
  webServer: { command: 'npm run preview -- --host 127.0.0.1 --port 4173', port: 4173, reuseExistingServer: false }
})
