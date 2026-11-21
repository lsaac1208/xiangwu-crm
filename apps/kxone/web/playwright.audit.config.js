import { defineConfig } from '@playwright/test'
export default defineConfig({
  // Keep the retired AI suite in source for its future redesign, outside the current product acceptance scope.
  testDir: './e2e', outputDir: 'audit-results', testMatch: ['repair-20261026.spec.js', 'ui-audit.spec.js', 'ui-workflows.spec.js', 'csp-assets.spec.js', 'apple-navigation.spec.js', 'mobile-refinement.spec.js'], timeout: 120000, workers: 1,
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'audit-report' }]],
  use: { channel: 'chrome', baseURL: 'http://127.0.0.1:4173', locale: 'zh-CN', timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce', screenshot: 'only-on-failure', trace: 'retain-on-failure' },
  webServer: [
    { command: 'npm run preview -- --host 127.0.0.1 --port 4173', port: 4173, reuseExistingServer: true }
  ]
})
