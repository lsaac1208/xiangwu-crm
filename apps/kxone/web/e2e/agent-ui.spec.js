import { test, expect } from '@playwright/test'
import { createRequire } from 'node:module'
import { json, mockAPI } from './crm-fixtures.js'
const axePath = createRequire(import.meta.url).resolve('axe-core/axe.min.js')

const now = '2026-08-27T10:30:00+08:00'
const result = { id: 'result-one', columns: ['来源', '数量'], rows: [['抖音', 12], ['小红书', 9]], total_rows: 2, returned_rows: 2, truncated: false, snapshot_at: now, query: 'SELECT source_channel AS 来源, COUNT(*) AS 数量 FROM leads WHERE source_channel IN (?,?) GROUP BY source_channel', query_params: ['抖音', '小红书'], sources: [{ type: 'lead', id: '28', lead_id: 28, label: '客户 C00028' }] }
const assistant = { id: 'assistant-one', role: 'assistant', content: '已核对本轮数据。', status: 'completed', run_id: 'run-one', created_at: now, blocks: [{ type: 'chart', title: '来源线索数量', result_id: result.id, chart_type: 'bar', x_column: '来源', y_columns: ['数量'] }, { type: 'table', title: '来源明细', result_id: result.id }] }

async function mockAgent(page, options = {}) {
  await mockAPI(page)
  const state = { creates: [], streams: [], conversation: null, run: null, messages: [], firstFailed: false }
  const answer = options.markdownOnly ? { ...assistant, blocks: [{ type: 'markdown', text: '| 来源 | 数量 |\n| --- | --- |\n| 假渠道 | 999999 |' }] } : assistant
  const exploratory = { ...result, id: 'process-result', columns: ['错误重复关联数'], rows: [[49]], total_rows: 1, returned_rows: 1, query: 'SELECT COUNT(*) FROM leads a JOIN leads b ON 1=1' }
  await page.route('**/api/agent/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname.replace('/api/agent', ''), method = request.method()
    if (path === '/conversations' && method === 'GET') return json(route, { conversations: state.conversation ? [state.conversation] : [], total: state.conversation ? 1 : 0 })
    if (path === '/conversations' && method === 'POST') { state.conversation = { id: 'conversation-one', title: '新对话', updated_at: now }; return json(route, state.conversation, 201) }
    if (path === '/conversations/conversation-one' && method === 'GET') return json(route, { conversation: state.conversation, messages: state.messages, runs: state.run ? [state.run] : [] })
    if (path === '/conversations/conversation-one' && method === 'PATCH') { state.conversation.title = request.postDataJSON().title; return json(route, state.conversation) }
    if (path === '/metrics') return options.metrics ? json(route, { fixed_metrics: true, metrics: [{ id: 'source_performance', title: '来源表现', description: '按来源统计', params: [], quick: true }, { id: 'owner_current', title: '负责人当前归属与净额', description: '当前归属', params: [], quick: true }, { id: 'followup_keyword', title: '跟进关键词检索', description: '关键词', params: ['keyword'], required: ['keyword'], quick: false }] }) : json(route, { message: 'not found' }, 404)
    if (path === '/settings' && options.aiOff) return json(route, { enabled: false, configured: true, key_configured: true, budget_micro: 300e6, spent_micro: 0, reserved_micro: 0, version: 1 })
    if (path === '/conversations/conversation-one/metric-runs') {
      const data = request.postDataJSON(); state.metricCreates = [...(state.metricCreates || []), data]
      state.run = { id: 'run-one', status: 'running', created_at: now, model: 'fixed-metrics' }
      state.messages = [{ id: 'user-one', role: 'user', content: '查看指标：来源表现（全部数据）', status: 'completed', run_id: 'run-one', created_at: now }]
      return json(route, state.run, 202)
    }
    if (path === '/conversations/conversation-one/runs') {
      const data = request.postDataJSON(); state.creates.push(data)
      if (options.failFirst && !state.firstFailed) { state.firstFailed = true; return json(route, { message: '测试网络失败，问题已保留' }, 503) }
      state.run = { id: 'run-one', status: 'running', created_at: now }
      state.messages = [{ id: 'user-one', role: 'user', content: data.message, status: 'completed', run_id: 'run-one', created_at: now }]
      return json(route, state.run, 202)
    }
    if (path === '/runs/run-one') return json(route, { run: state.run, messages: state.messages, results: state.run?.status === 'completed' ? options.exploratory ? [exploratory, result] : [result] : [], result_selection: { status: state.run?.status === 'completed' ? 'explicit' : 'unfinished', result_ids: state.run?.status === 'completed' ? [result.id] : [], process_result_ids: state.run?.status === 'completed' && options.exploratory ? [exploratory.id] : [] } })
    if (path === '/runs/run-one/events') {
      state.streams.push(Number(url.searchParams.get('after') || 0))
      if (options.slow || (options.disconnect && state.streams.length === 1)) return route.fulfill({ contentType: 'text/event-stream', body: 'event: update\ndata: {"seq":1,"type":"status","data":{"text":"查询数据中"}}\n\n' + (options.unverifiedDraft ? 'data: {"seq":2,"type":"delta","data":{"text":"全局覆盖四条线索，两组互不重叠"}}\n\n' : '') })
      state.run.status = 'completed'; state.run.snapshot_at = now; state.messages.push(answer)
      const events = [{ seq: 1, type: 'status', data: { text: '查询数据中' } }, { seq: 2, type: 'message', data: { message: answer } }, { seq: 3, type: 'done', data: { status: 'completed' } }]
      return route.fulfill({ contentType: 'text/event-stream', body: events.map(event => `event: update\ndata: ${JSON.stringify(event)}\n\n`).join('') })
    }
    if (path === '/runs/run-one/cancel') { state.run.status = 'cancelled'; return json(route, { ok: true }) }
    if (path === '/results/result-one') return json(route, result)
    if (path === '/runs/run-one/exports') return json(route, { id: 'export-one' }, 201)
    if (path === '/exports/export-one/download') return route.fulfill({ contentType: 'text/html', body: '<h1>已核对本轮数据。</h1>' })
    return route.fallback()
  })
  return state
}

test('unverified streamed draft stays hidden until the final message', async ({ page }) => {
  const state = await mockAgent(page, { slow: true, unverifiedDraft: true })
  await page.goto('/agent')
  await page.getByLabel('你的分析问题').fill('核对员工跟进数量')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect.poll(() => state.streams.length).toBeGreaterThan(0)
  await expect(page.getByRole('article', { name: '当前分析进度' })).toContainText(/正在核对回答|正在恢复接收/)
  await expect(page.getByText(/全局覆盖四条线索/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: '复制回答', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '导出 Excel', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '停止生成', exact: true }).click()
})

test('discarded exploratory results are separate from adopted answer evidence', async ({ page }, info) => {
  await page.setViewportSize({ width: 375, height: 900 }); await mockAgent(page, { exploratory: true })
  await page.goto('/agent')
  await page.getByLabel('你的分析问题').fill('核对来源数量')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByText('已核对本轮数据。', { exact: true })).toBeVisible()
  const evidence = page.getByRole('region', { name: '本次分析的数据与查询依据', exact: true })
  await evidence.getByText('数据与查询依据', { exact: true }).click()
  const adopted = evidence.locator('[aria-label="最终采用的查询结果"]')
  await expect(adopted.locator('tbody tr')).toHaveCount(2)
  await expect(adopted).toContainText('抖音')
  await expect(adopted).not.toContainText('49')
  const process = evidence.locator('.agent-query-process')
  await expect(process.locator('tbody')).not.toBeVisible()
  await process.getByText('查询过程（1 项，未作为结论导出）', { exact: true }).click()
  await expect(process.locator('tbody')).toContainText('49')
  await page.screenshot({ path: info.outputPath('agent-final-vs-process.png'), fullPage: true })
})

for (const width of [320, 1440]) {
  test(`Markdown-only answer still exposes its saved snapshot and query evidence ${width}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 })
    const state = await mockAgent(page, { markdownOnly: true })
    await page.goto('/agent')
    await page.getByLabel('你的分析问题').fill('查询全部来源数量，只用文字概括。')
    await page.getByRole('button', { name: '发送', exact: true }).click()
    await expect(page.getByText(assistant.content, { exact: true })).toBeVisible()
    await expect(page.getByText('数据表请查看下方查询依据。', { exact: true })).toBeVisible()
    await expect(page.locator('.agent-markdown table')).toHaveCount(0)
    await expect(page.getByText('999999', { exact: true })).toHaveCount(0)
    const evidence = page.getByRole('region', { name: '本次分析的数据与查询依据', exact: true })
    await expect(evidence.locator('time')).toHaveAttribute('datetime', now)
    await evidence.getByText('数据与查询依据', { exact: true }).click()
    await expect(evidence.getByRole('region', { name: '查询结果 1数据表', exact: true })).toContainText('小红书')
    const rows = await evidence.locator('tbody tr').evaluateAll(items => items.map(row => Array.from(row.querySelectorAll('td'), cell => cell.textContent)))
    expect(rows).toEqual([['抖音', '12'], ['小红书', '9']])
    await expect(evidence.getByLabel('查询依据', { exact: true })).toHaveText(result.query)
    expect(JSON.parse(await evidence.getByLabel('查询绑定参数', { exact: true }).textContent())).toEqual(result.query_params)
    // Internal details stay collapsed under “依据” until the reader opens them.
    await expect(evidence.getByLabel('查询绑定参数', { exact: true })).toBeHidden()
    await evidence.locator('details.agent-result-basis > summary').first().click()
    await evidence.getByLabel('查询绑定参数', { exact: true }).scrollIntoViewIfNeeded()
    await expect(evidence.getByLabel('查询绑定参数', { exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.screenshot({ path: info.outputPath(`markdown-evidence-${width}.png`), fullPage: true })
    await page.reload()
    await expect(evidence.locator('time')).toHaveAttribute('datetime', now)
    await evidence.getByText('数据与查询依据', { exact: true }).click()
    await expect(evidence.getByLabel('查询依据', { exact: true })).toHaveText(result.query)
    expect(JSON.parse(await evidence.getByLabel('查询绑定参数', { exact: true }).textContent())).toEqual(result.query_params)
    expect(state.creates).toHaveLength(1)
  })
}

for (const width of [320, 375, 768, 1440]) {
  test(`analysis conversation, chart, evidence and keyboard layout ${width}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 }); await mockAgent(page)
    await page.goto('/stats/sla'); await expect(page).toHaveURL(/\/agent$/)
    await expect(page.locator('.app-sidebar')).toHaveCount(0)
    await page.getByLabel('你的分析问题').fill('按来源比较全部历史线索')
    await page.getByRole('button', { name: '发送', exact: true }).click()
    await expect(page.getByText('已核对本轮数据。', { exact: true })).toBeVisible()
    await expect(page.getByRole('img', { name: '来源线索数量，下方可展开相同数据的表格' })).toBeVisible()
    await expect(page.getByRole('region', { name: '来源明细数据表' })).toContainText('小红书')
    await expect(page.getByLabel('你的分析问题')).toHaveValue('')
    // Source records are part of the collapsed basis; open it to follow them.
    for (const summary of await page.locator('.agent-answer > details.agent-evidence > summary, .agent-answer > .agent-result > details.agent-result-basis > summary').all()) await summary.click()
    await expect(page.getByRole('link', { name: '客户 C00028' }).first()).toHaveAttribute('href', /\/leads\/28\?return_to=/)
    const evidence = page.getByRole('region', { name: '本次分析的数据与查询依据', exact: true })
    await evidence.getByText('数据与查询依据', { exact: true }).click()
    await expect(evidence.locator('tbody tr')).toHaveCount(2)
    // Visually hidden table captions must remain inside the result scroller;
    // an unpositioned ancestor previously let them enlarge the document and
    // scroll the entire chat, including its composer, into an empty page.
    await page.locator('.agent-messages').evaluate(element => { element.scrollTop = 0 })
    expect(await page.evaluate(() => document.documentElement.scrollHeight <= innerHeight + 1)).toBe(true)
    await page.evaluate(() => window.scrollTo(0, 10000))
    expect(await page.evaluate(() => window.scrollY)).toBe(0)
    await page.locator('.agent-messages').evaluate(element => { element.scrollTop = element.scrollHeight })
    expect(await page.locator('.agent-messages').evaluate(element => element.scrollTop)).toBeGreaterThan(0)
    await expect(page.getByLabel('你的分析问题')).toBeInViewport()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.addScriptTag({ path: axePath })
    const violations = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations)
    expect(violations).toEqual([])
    await page.screenshot({ path: info.outputPath(`agent-${width}.png`), fullPage: true })
  })
}

test('a failed send preserves the draft and reuses its idempotency key', async ({ page }) => {
  const state = await mockAgent(page, { failFirst: true })
  await page.goto('/agent')
  const input = page.getByLabel('你的分析问题')
  await input.fill('比较本月来源表现')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(input).toHaveValue('比较本月来源表现')
  await expect(page.locator('.agent-inline-error').getByText('测试网络失败，问题已保留', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByText('已核对本轮数据。', { exact: true })).toBeVisible()
  expect(state.creates).toHaveLength(2)
  expect(state.creates[0].request_id).toBe(state.creates[1].request_id)
})

test('a disconnected answer resumes with an event cursor without creating another run', async ({ page }) => {
  const state = await mockAgent(page, { disconnect: true })
  await page.goto('/agent')
  await page.getByLabel('你的分析问题').fill('统计历史线索')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByText('已核对本轮数据。', { exact: true })).toBeVisible({ timeout: 15000 })
  expect(state.creates).toHaveLength(1)
  expect(state.streams).toEqual([0, 1])
  await expect(page.getByText('已核对本轮数据。', { exact: true })).toHaveCount(1)
})

test('IME composition does not submit and a running analysis can be stopped', async ({ page }) => {
  const state = await mockAgent(page, { slow: true })
  await page.goto('/agent')
  const input = page.getByLabel('你的分析问题')
  await input.fill('本月')
  await input.dispatchEvent('compositionstart')
  await input.press('Enter')
  expect(state.creates).toHaveLength(0)
  await input.dispatchEvent('compositionend')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await page.getByRole('button', { name: '停止生成', exact: true }).click()
  await expect(page.getByText('分析已停止，已有回答和结果已保留', { exact: true })).toBeVisible()
  expect(state.run.status).toBe('cancelled')
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeVisible()
})

test('saved answers export from their run and a page reload preserves unsent text', async ({ page }) => {
  await mockAgent(page)
  await page.goto('/agent')
  await page.getByLabel('你的分析问题').fill('来源表现')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByText('已核对本轮数据。', { exact: true })).toBeVisible()
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出报告', exact: true }).click()
  expect((await download).suggestedFilename()).toMatch(/\.html$/)
  await page.getByLabel('你的分析问题').fill('尚未发送的追问')
  await page.reload()
  await expect(page.getByLabel('你的分析问题')).toHaveValue('尚未发送的追问')
})

test('system settings accepts model config and tests only the explicit submitted inputs', async ({ page }) => {
  await mockAPI(page)
  let tests = 0, saved
  await page.route('**/api/agent/settings/test', route => { tests++; return json(route, { ok: true, latency_ms: 42, model: route.request().postDataJSON().model }) })
  await page.route('**/api/agent/settings', route => {
    if (route.request().method() !== 'PUT') return route.fallback()
    saved = route.request().postDataJSON()
    return json(route, { ...saved, configured: true, key_configured: true, version: 2 })
  })
  await page.goto('/admin/settings?section=ai')
  await page.getByLabel('API 服务地址', { exact: true }).fill('https://custom.example/v1')
  await page.getByLabel('模型名称', { exact: true }).fill('custom-model')
  await page.getByLabel('API Key', { exact: true }).fill('browser-test-key')
  expect(tests).toBe(0)
  await page.getByRole('button', { name: '测试连接', exact: true }).click()
  await expect(page.getByText('连接成功 · custom-model · 42 毫秒')).toBeVisible()
  await page.getByRole('button', { name: '保存 AI 配置', exact: true }).click()
  await expect(page.getByLabel('API Key', { exact: true })).toHaveValue('')
  expect(saved).toEqual(expect.objectContaining({ base_url: 'https://custom.example/v1', model: 'custom-model', api_key: 'browser-test-key', expected_version: 1, input_price_cny: 12, output_price_cny: 36 }))
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain('browser-test-key')
})

test('quick fixed metrics run without the model while AI analysis is off', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 860 })
  const state = await mockAgent(page, { metrics: true, aiOff: true })
  await page.goto('/agent')
  const quick = page.getByRole('navigation', { name: '常用指标' })
  await expect(quick).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: '常用指标直接查看数据' })).toBeVisible()
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeDisabled()
  await quick.getByRole('button', { name: '跟进关键词' }).click()
  await expect(page.getByText(/请先在输入框写入要检索的跟进关键词/)).toBeVisible()
  expect(state.metricCreates || []).toHaveLength(0)
  await quick.getByRole('button', { name: '来源表现' }).click()
  await expect.poll(() => (state.metricCreates || []).length).toBe(1)
  expect(state.metricCreates[0]).toMatchObject({ metric_id: 'source_performance', params: {} })
  expect(state.creates).toHaveLength(0)
  await expect(page.getByRole('region', { name: '来源明细数据表' })).toBeVisible()
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow).toBeLessThanOrEqual(0)
  await page.getByLabel('你的分析问题').fill('费用')
  await quick.getByRole('button', { name: '跟进关键词' }).click()
  await expect.poll(() => (state.metricCreates || []).length).toBe(2)
  expect(state.metricCreates[1]).toMatchObject({ metric_id: 'followup_keyword', params: { keyword: '费用' } })
  await page.addScriptTag({ path: axePath })
  const audit = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(item => item.id))
  expect(audit).toEqual([])
})
