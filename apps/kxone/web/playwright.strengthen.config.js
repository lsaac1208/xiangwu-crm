import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './e2e', testMatch: ['workflow-enhancements.spec.js'], outputDir: 'test-results/strengthen',
  workers: 1, timeout: 30000, reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:4193', locale: 'zh-CN', timezoneId: 'Asia/Shanghai', screenshot: 'only-on-failure' },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: { command: 'npm run preview -- --host 127.0.0.1 --port 4193 --strictPort', port: 4193, reuseExistingServer: false }
})
