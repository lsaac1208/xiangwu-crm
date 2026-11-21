import { defineConfig } from '@playwright/test'
import { readFileSync, mkdirSync, chmodSync, statSync } from 'node:fs'
import { isAbsolute, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// This configuration deliberately cannot run as part of ordinary CI. It sends
// paid requests to the configured cloud model using an isolated fixture server.
if (process.env.AGENT_LIVE_E2E !== '1') throw new Error('真实云验收须显式设置 AGENT_LIVE_E2E=1；不会自动调用模型。')
// Playwright may recreate output directories; inherited umask keeps recreated
// directories and screenshots private as well as the files written by the spec.
process.umask(0o077)
const fixtureFile = process.env.AGENT_LIVE_FIXTURE_FILE
if (!fixtureFile || !isAbsolute(fixtureFile)) throw new Error('AGENT_LIVE_FIXTURE_FILE 必须指向隔离服务生成的绝对路径。')
if ((statSync(fixtureFile).mode & 0o077) !== 0) throw new Error('隔离登录文件必须为 0600，避免测试凭证泄露。')
const fixture = JSON.parse(readFileSync(fixtureFile, 'utf8'))
const baseURL = process.env.AGENT_LIVE_BASE_URL || fixture.base_url
const url = new URL(baseURL)
if (url.protocol !== 'http:' || !['127.0.0.1', '[::1]'].includes(url.hostname) || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
  throw new Error('真实云验收只允许 loopback 隔离 CRM 地址，禁止指向生产 CRM。')
}
const outputDir = process.env.AGENT_LIVE_OUTPUT_DIR
const repositoryRoot = fileURLToPath(new URL('../../../', import.meta.url)).replace(/\/$/, '')
if (!outputDir || !isAbsolute(outputDir) || resolve(outputDir) === repositoryRoot || resolve(outputDir).startsWith(repositoryRoot + '/')) {
  throw new Error('AGENT_LIVE_OUTPUT_DIR 必须为项目目录之外的独立绝对路径。')
}
mkdirSync(outputDir, { recursive: true, mode: 0o700 })
chmodSync(outputDir, 0o700)

export default defineConfig({
  testDir: './e2e', testMatch: 'agent-live.spec.js', outputDir,
  timeout: 600_000, expect: { timeout: 15_000 },
  fullyParallel: false, workers: 1, retries: 0, forbidOnly: true,
  reporter: [['list']],
  use: {
    baseURL, channel: process.env.AGENT_LIVE_BROWSER_CHANNEL || 'chrome',
    locale: 'zh-CN', timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce',
    // Traces and videos can contain authentication headers or storage state.
    // Explicit screenshots and sanitized result evidence are saved by the test.
    screenshot: 'only-on-failure', trace: 'off', video: 'off', acceptDownloads: true
  }
})
