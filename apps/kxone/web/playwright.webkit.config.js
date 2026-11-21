import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e', testMatch: ['repair-20261026.spec.js', 'webkit-smoke.spec.js'], workers: 1,
  outputDir: 'test-results/webkit', timeout: 30_000, reporter: 'list',
  use: { browserName: 'webkit', baseURL: 'http://127.0.0.1:4173', locale: 'zh-CN', timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce', screenshot: 'only-on-failure' },
  webServer: { command: 'npm run preview -- --host 127.0.0.1 --port 4173', port: 4173, reuseExistingServer: true }
})
