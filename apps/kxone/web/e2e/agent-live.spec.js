import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { readFile, writeFile, chmod } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { JSDOM } from 'jsdom'
import { installSSEDisconnect, verifySSERecovery, replayAcceptedRequest, verifyCompletedReplay, verifyCancelledDraft } from './agent-run-checks.js'

// No fixture model responses: answers use the real configured provider and
// isolated business data. A browser-only transport abort tests SSE recovery;
// it never changes real response bytes, tool results or model answers.
if (process.env.AGENT_LIVE_E2E !== '1') throw new Error('真实云用例必须经显式 AGENT_LIVE_E2E=1 启用。')
const live = JSON.parse(readFileSync(process.env.AGENT_LIVE_FIXTURE_FILE, 'utf8'))
const expectProviderSwitch = process.env.AGENT_LIVE_EXPECT_PROVIDER_SWITCH === '1'
if (process.env.AGENT_LIVE_EXPECT_PROVIDER_SWITCH && !expectProviderSwitch) throw new Error('备用切换断言须显式设置 AGENT_LIVE_EXPECT_PROVIDER_SWITCH=1。')
if (expectProviderSwitch && (!live.fallback_model || live.provider_target === 'fallback' || live.fallback_model === live.model)) {
  throw new Error('自动切换验收须从主模型开始，并配置不同的真实备用模型。')
}
const faultInjection = live.fault_injection ? {
  kind: live.fault_injection.kind,
  scope: live.fault_injection.scope,
  synthetic_primary_failure: live.fault_injection.synthetic_primary_failure,
  real_fallback: live.fault_injection.real_fallback
} : null
if (faultInjection && (faultInjection.kind !== 'primary_http_503'
  || faultInjection.scope !== 'all_primary_calls_in_isolated_fixture'
  || faultInjection.synthetic_primary_failure !== true || faultInjection.real_fallback !== true)) {
  throw new Error('不认识隔离故障注入声明，禁止将模拟故障冒充真实供应商故障。')
}
const fixtureBytes = readFileSync(new URL('../../docs/Agent-Evaluation-Fixture.json', import.meta.url))
const fixture = JSON.parse(fixtureBytes)
const evaluation = JSON.parse(readFileSync(new URL('../../docs/Agent-Evaluation-Cases.json', import.meta.url), 'utf8'))
const firstQuestion = (evaluation.cases || evaluation).find(item => item.id === 'A02').question + '请同时提供按来源的柱状图和可核对的数据表，图表使用同一查询结果。'
const followupQuestion = '沿用刚才全部历史线索的范围，只看好学校。请按当前负责人分别列出线索数量、扣除退款后的净成交金额（人民币元），用可核对的数据表展示；再查阅这些线索的跟进记录，概括客户顾虑，附可打开的记录依据。不要把当前负责人当作历史操作人。'
const terminal = new Set(['completed', 'failed', 'cancelled', 'interrupted', 'budget_exceeded'])
const sha256 = data => createHash('sha256').update(data).digest('hex')
const displayCell = value => value == null ? '' : typeof value === 'object' ? JSON.stringify(value) : String(value)
const displayUICell = value => value == null ? '—' : displayCell(value)

async function saveJSON(info, name, value) {
  const path = info.outputPath(name)
  await writeFile(path, JSON.stringify(value, null, 2), { mode: 0o600 })
  return path
}
async function getJSON(request, path) {
  const response = await request.get(path)
  expect(response.status(), `读取 ${path}`).toBe(200)
  return response.json()
}
async function runEventEvidence(request, runId) {
  const response = await request.get(`/api/agent/runs/${runId}/events`)
  expect(response.status(), '读取原运行事件不会创建新分析').toBe(200)
  const fields = {
    provider_switch: ['from_model', 'model', 'reason'],
    context_checkpoint: ['segment', 'reason', 'result_count'],
    context_usage: ['rules_bytes', 'history_and_question_bytes', 'tool_bytes', 'assistant_bytes', 'reasoning_bytes', 'request_bytes', 'segment'],
    model_usage: ['configured_model', 'reported_model', 'response_id', 'input_tokens', 'output_tokens', 'has_usage', 'reservation_id', 'reservation_micro', 'price_version', 'settle_result', 'call_failed', 'provider_http_status', 'failure_kind', 'estimated_actual_micro'],
    snapshot_ready: ['source_read_ms', 'source_rows'],
    provider_privacy_rejection: [],
    done: ['status']
  }
  return (await response.text()).split(/\r?\n/).filter(line => line.startsWith('data:')).map(line => JSON.parse(line.slice(5).trim()))
    .filter(event => fields[event.type] && event.data && typeof event.data === 'object')
    .map(event => {
      const data = Object.fromEntries(fields[event.type]
        .filter(key => ['string', 'number', 'boolean'].includes(typeof event.data[key])).map(key => [key, event.data[key]]))
      if (event.type === 'provider_privacy_rejection') data.findings = (Array.isArray(event.data.findings) ? event.data.findings : []).slice(0, 20)
        .filter(item => ['content', 'reasoning', 'tool_arguments'].includes(item?.field)
          && ['known_identity', 'known_contact', 'credential', 'phone', 'wechat', 'email', 'id_card', 'unknown_name', 'oversized', 'sanitization', 'guard_unavailable'].includes(item?.rule)
          && ['count', 'bytes'].every(key => Number.isInteger(item[key]) && item[key] >= 0))
        .map(item => Object.fromEntries(['field', 'rule', 'count', 'bytes'].map(key => [key, item[key]])))
      return { seq: event.seq, type: event.type, data }
    })
}
function verifyProviderSwitch(detail, events) {
  const switches = events.filter(event => event.type === 'provider_switch')
  const usage = events.filter(event => event.type === 'model_usage')
  expect(switches, '同一原运行只自动切换一次').toHaveLength(1)
  expect(switches[0].data).toEqual({ from_model: live.model, model: live.fallback_model, reason: 'primary_unavailable' })
  expect(detail.run.model).toBe(live.fallback_model)
  expect(usage.length, '主备共用每轮八次模型调用上限').toBeLessThanOrEqual(8)
  const primary = usage.filter(event => event.data.configured_model === live.model)
  const backup = usage.filter(event => event.data.configured_model === live.fallback_model)
  expect(primary).toHaveLength(1)
  expect(primary[0].seq).toBeLessThan(switches[0].seq)
  expect(primary[0].data.call_failed).toBe(true)
  expect(backup.length, '必须实际执行备用模型调用').toBeGreaterThan(0)
  for (const event of backup) {
    expect(event.seq).toBeGreaterThan(switches[0].seq)
    expect(event.data.has_usage).toBe(true)
    expect(event.data.settle_result).toBe('settled')
  }
  if (faultInjection) {
    expect(primary[0].data.provider_http_status).toBe(503)
    expect(primary[0].data.has_usage).toBe(false)
    expect(primary[0].data.settle_result).toBe('unknown')
    expect(primary[0].data.reservation_micro, '未知主调用费用保持预留').toBeGreaterThan(0)
  }
  expect(new Set(usage.map(event => event.data.reservation_id)).size, '每次主备调用的账本身份唯一').toBe(usage.length)
  const accounted = usage.reduce((sum, event) => sum + (event.data.has_usage === true && event.data.settle_result === 'settled'
    ? event.data.estimated_actual_micro : event.data.reservation_micro), 0)
  expect(Number.isFinite(accounted)).toBe(true)
  expect(accounted, '主备共用每轮三元上限，未知费用不能归零').toBeLessThanOrEqual(3_000_000)
  expect(Date.parse(detail.run.finished_at) - Date.parse(detail.run.created_at), '切换不重置原运行截止时间').toBeLessThanOrEqual(180_000)
}
async function signIn(context, baseURL, token) {
  expect(typeof token).toBe('string')
  expect(token.length).toBeGreaterThan(30)
  await context.addCookies([
    { name: 'kxone_access_token', value: token, url: baseURL, httpOnly: true, sameSite: 'Lax' },
    { name: 'kxone_session', value: '1', url: baseURL, sameSite: 'Lax' }
  ])
}
function assistantBlocks(detail) {
  return detail.messages.filter(message => message.role === 'assistant').flatMap(message => message.blocks || [])
}
function finalResults(detail) {
  const selection = detail.result_selection
  expect(selection?.status, '新分析必须明确声明最终采用结果').toBe('explicit')
  expect(Array.isArray(selection.result_ids)).toBe(true)
  expect(new Set(selection.result_ids).size).toBe(selection.result_ids.length)
  return selection.result_ids.map(id => {
    const result = detail.results.find(item => item.id === id)
    expect(result, `最终引用 ${id} 必须属于本轮保存结果`).toBeTruthy()
    return result
  })
}
async function ask(page, info, question, label, evidence, reloadDuringRun = false) {
  const started = Date.now()
  await page.getByLabel('你的分析问题').fill(question)
  const accepted = page.waitForResponse(response => response.request().method() === 'POST' && /\/conversations\/[^/]+\/runs$/.test(new URL(response.url()).pathname))
  await page.getByRole('button', { name: '发送', exact: true }).click()
  const response = await accepted
  expect(response.status(), '服务端确认接收问题').toBe(202)
  const created = await response.json()
  expect(created.id).toBeTruthy()
  const runEvidence = { id: created.id, status: created.status || 'accepted', question, elapsed_ms: 0 }
  evidence.runs.push(runEvidence)
  // Keep the accepted run ID even if the tunnel fails while waiting. Recovery
  // must inspect this run instead of automatically repeating a paid question.
  await saveJSON(info, `${label}-accepted.json`, { question, accepted_at: new Date().toISOString(), run_id: created.id, conversation_id: created.conversation_id })
  let detail, events, collectionComplete = false
  try {
    runEvidence.accepted_replay = await replayAcceptedRequest(page.request, response, created.id)
    if (reloadDuringRun) {
      runEvidence.sse_recovery = await verifySSERecovery(page, created.id)
      detail = await getJSON(page.request, `/api/agent/runs/${created.id}`)
      runEvidence.status_before_reload = detail.run.status
      expect(['queued', 'running'], '在真实分析运行期间刷新').toContain(detail.run.status)
      await expect(page).toHaveURL(new RegExp(`/agent/${created.conversation_id}$`))
      await page.reload()
      await expect(page.getByLabel('你的分析问题')).toBeVisible()
      const resumed = await getJSON(page.request, `/api/agent/conversations/${created.conversation_id}`)
      expect(resumed.runs.map(run => run.id)).toEqual([created.id])
      runEvidence.reload_recovered_original_run = true
    }
    await expect.poll(async () => {
      detail = await getJSON(page.request, `/api/agent/runs/${created.id}`)
      return terminal.has(detail.run.status) && detail.messages.some(message => message.role === 'assistant' && message.run_id === created.id && message.status === detail.run.status)
    }, { timeout: 200_000, intervals: [1000, 2000, 3000], message: '等待真实云分析及最终消息完成，不重新创建运行' }).toBe(true)
    collectionComplete = true
    events = await runEventEvidence(page.request, created.id)
    runEvidence.operational_events = events
    runEvidence.events_complete = events.some(event => event.type === 'done' && event.data.status === detail.run.status)
    await saveJSON(info, `${label}-events.json`, { run_id: created.id, fault_injection: faultInjection, events })
  } finally {
    Object.assign(runEvidence, { status: detail?.run?.status || created.status, elapsed_ms: Date.now() - started, collection_complete: collectionComplete })
    await saveJSON(info, `${label}-run.json`, { question, run_id: created.id, elapsed_ms: Date.now() - started, collection_complete: collectionComplete, ...detail })
  }
  expect(detail.run.status, `${label}：${detail.run.error || '没有错误详情'}`).toBe('completed')
  runEvidence.completed_replay = await verifyCompletedReplay(page.request, response, created.id)
  expect(runEvidence.events_complete, '完整事件证据须包含本次终态').toBe(true)
  if (expectProviderSwitch) {
    verifyProviderSwitch(detail, events)
    runEvidence.provider_switch_verified = true
  }
  expect([live.model, live.fallback_model].filter(Boolean)).toContain(detail.run.model)
  expect(detail.run.snapshot_at).toBeTruthy()
  expect(detail.results.length).toBeGreaterThan(0)
  expect(finalResults(detail).length, '本用例最终答案须采用查询结果').toBeGreaterThan(0)
  await expect(page.getByRole('button', { name: '停止生成', exact: true })).toHaveCount(0)
  const finalMessage = detail.messages.find(message => message.role === 'assistant' && message.run_id === created.id)
  expect(finalMessage.status).toBe('completed')
  const rendered = page.getByRole('article', { name: 'AI 回答', exact: true }).last()
  await expect(rendered.getByRole('button', { name: '导出 Excel', exact: true })).toBeVisible()
  await expect(rendered.locator('.agent-markdown table'), '模型 Markdown 不能绕过结果 ID 绑定').toHaveCount(0)
  await expect(rendered.locator('.agent-markdown a:not([href]), .agent-markdown a[href=""]'), '不支持的模型链接只能显示为普通文字').toHaveCount(0)
  const ids = new Set(detail.results.map(result => result.id))
  for (const block of assistantBlocks(detail).filter(block => ['chart', 'table'].includes(block.type))) {
    expect(ids.has(block.result_id), '表格和图表只引用本轮真实结果').toBe(true)
  }
  const exposed = JSON.stringify(detail)
  for (const fragment of fixture.independent_expected.forbidden_provider_payload_fragments) expect(exposed).not.toContain(fragment)
  return { detail, article: rendered, elapsed_ms: Date.now() - started }
}
function sourceCountResult(detail) {
  const wanted = fixture.independent_expected.sources
  for (const result of finalResults(detail)) {
    if (result.rows.length !== Object.keys(wanted).length) continue
    const sourceIndex = result.columns.findIndex((_, column) => result.rows.every(row => Object.hasOwn(wanted, row[column])) && new Set(result.rows.map(row => row[column])).size === Object.keys(wanted).length)
    if (sourceIndex < 0) continue
    const countIndex = result.columns.findIndex((_, column) => column !== sourceIndex && result.rows.every(row => typeof row[column] === 'number' && row[column] === wanted[row[sourceIndex]]))
    if (countIndex >= 0) return result
  }
  throw new Error('没有找到覆盖全部五种来源、逐行数量与独立 fixture 一致的真实结果；不能用回答文本含数字代替。')
}
function ownerResult(detail) {
  const leads = fixture.tables.leads.filter(lead => lead.source_channel === '好学校')
  const expected = new Map()
  for (const lead of leads) {
    const current = expected.get(lead.current_owner_user_id) || { count: 0, net: 0 }
    current.count++
    current.net += lead.deal_amount - lead.refund_amount
    expected.set(lead.current_owner_user_id, current)
  }
  const byName = new Map(fixture.tables.crm_wecom_user_bindings.map(person => [person.employee_name, person.wecom_userid]))
  for (const result of finalResults(detail)) {
    if (result.rows.length !== expected.size) continue
    const ownerIndex = result.columns.findIndex((_, index) => result.rows.every(row => expected.has(byName.get(row[index]) || row[index])))
    const countIndex = result.columns.findIndex(column => /线索数|数量|count|total/i.test(column))
    const netIndex = result.columns.findIndex(column => /净|net/i.test(column))
    if (ownerIndex < 0 || countIndex < 0 || netIndex < 0) continue
    const owners = result.rows.map(row => byName.get(row[ownerIndex]) || row[ownerIndex])
    if (new Set(owners).size !== expected.size) continue
    const actual = result.rows.map((row, index) => ({ owner: owners[index], count: row[countIndex], net: row[netIndex] })).sort((a, b) => a.owner.localeCompare(b.owner))
    const wanted = Array.from(expected, ([owner, value]) => ({ owner, ...value })).sort((a, b) => a.owner.localeCompare(b.owner))
    expect(actual, '好学校来源、当前负责人及净成交额（元）逐行独立对账').toEqual(wanted)
    return result
  }
  throw new Error('未找到好学校按当前负责人分组的真实结果；不得用全来源人员汇总代替连续追问。')
}
async function verifyRenderedResult(article, detail, result) {
  // A model may choose prose only. The deterministic evidence panel must still
  // expose the exact saved query and cells; API-only correctness cannot pass.
  const evidence = article.getByRole('region', { name: '本次分析的数据与查询依据', exact: true })
  await expect(evidence.locator('time')).toHaveAttribute('datetime', detail.run.snapshot_at)
  const evidenceDetails = evidence.locator('details').first()
  if (await evidenceDetails.getAttribute('open') === null) await evidence.getByText('数据与查询依据', { exact: true }).click()
  const section = evidence.locator(`[aria-label="最终采用的查询结果"] .agent-result[data-result-id="${result.id}"]`)
  await expect(section).toBeVisible()
  const details = section.locator('details.agent-result-basis')
  if (await details.getAttribute('open') === null) await details.locator('summary').click()
  const actual = await section.locator('tbody tr').evaluateAll(rows => rows.map(row => Array.from(row.querySelectorAll('td'), cell => cell.textContent)))
  expect(actual).toEqual(result.rows.map(row => row.map(displayUICell)))
  await expect(section.getByLabel('查询依据', { exact: true })).toHaveText(result.query)
  expect(Array.isArray(result.query_params), '新分析必须保存已执行的脱敏绑定参数').toBe(true)
  await section.getByLabel('查询绑定参数', { exact: true }).scrollIntoViewIfNeeded()
  await expect(section.getByLabel('查询绑定参数', { exact: true })).toBeVisible()
  expect(JSON.parse(await section.getByLabel('查询绑定参数', { exact: true }).textContent())).toEqual(result.query_params)
  if (result.coverage) await expect(section).toContainText(result.coverage)
  expect(result.truncated).toBe(false)
  expect(result.total_rows).toBe(result.rows.length)
  expect(result.materialized_complete).toBe(true)
}

// Python's standard ZIP/XML reader is independent of Excelize and never opens
// formulas, macros, network resources or an Excel application.
const parseWorkbook = String.raw`
import json,sys,zipfile,xml.etree.ElementTree as E,posixpath
ns={'s':'http://schemas.openxmlformats.org/spreadsheetml/2006/main','r':'http://schemas.openxmlformats.org/officeDocument/2006/relationships'}
with zipfile.ZipFile(sys.argv[1]) as z:
  assert sum(x.file_size for x in z.infolist()) < 50000000, 'workbook too large'
  def xml(name): return E.fromstring(z.read(name))
  shared=[]
  if 'xl/sharedStrings.xml' in z.namelist():
    shared=[''.join(x.itertext()) for x in xml('xl/sharedStrings.xml').findall('s:si',ns)]
  rels={x.attrib['Id']:x.attrib['Target'] for x in xml('xl/_rels/workbook.xml.rels')}
  out={}
  for sheet in xml('xl/workbook.xml').findall('s:sheets/s:sheet',ns):
    target=rels[sheet.attrib['{'+ns['r']+'}id']]
    path=target.lstrip('/') if target.startswith('/') else posixpath.normpath('xl/'+target)
    rows=[]
    for row in xml(path).findall('s:sheetData/s:row',ns):
      cells=[]
      for cell in row.findall('s:c',ns):
        assert cell.find('s:f',ns) is None, 'unexpected formula'
        letters=''.join(c for c in cell.attrib['r'] if c.isalpha()); col=0
        for c in letters: col=col*26+ord(c)-64
        while len(cells)<col: cells.append('')
        value=cell.find('s:v',ns); text='' if value is None else value.text or ''
        if cell.attrib.get('t')=='s': text=shared[int(text)]
        elif cell.attrib.get('t')=='inlineStr': text=''.join(cell.find('s:is',ns).itertext())
        cells[col-1]=text
      rows.append(cells)
    out[sheet.attrib['name']]=rows
  print(json.dumps(out,ensure_ascii=False))
`
async function verifyExports(page, article, detail, info, label) {
  const exports = []
  const adopted = finalResults(detail)
  for (const [format, button] of [['xlsx', '导出 Excel'], ['html', '导出报告']]) {
    const downloaded = page.waitForEvent('download')
    const created = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/agent/runs/${detail.run.id}/exports`)
    await article.getByRole('button', { name: button, exact: true }).click()
    const response = await created
    expect(response.status()).toBe(201)
    const index = await response.json()
    const file = await downloaded
    expect(await file.failure()).toBeNull()
    const path = info.outputPath(`${label}.${format}`)
    await file.saveAs(path)
    await chmod(path, 0o600)
    const bytes = await readFile(path)
    expect(sha256(bytes)).toBe(index.sha256)
    exports.push({ id: index.id, format, sha256: index.sha256 })
    if (format === 'xlsx') {
      const workbook = JSON.parse(execFileSync(process.env.AGENT_LIVE_PYTHON || 'python3', ['-c', parseWorkbook, path], { encoding: 'utf8', maxBuffer: 10 * 1024 * 1024 }))
      expect(workbook['口径说明'].some(row => row[0] === '运行 ID' && row[1] === detail.run.id)).toBe(true)
      expect(workbook['回答内容']).toBeTruthy()
      expect(Object.keys(workbook).filter(name => /^结果\d+$/.test(name))).toHaveLength(adopted.length)
      for (const [index, result] of adopted.entries()) {
        const expected = [result.columns, ...result.rows].map(row => row.map(displayCell))
        const name = `结果${String(index + 1).padStart(2, '0')}`
        expect(workbook[name].map(row => Array.from({ length: result.columns.length }, (_, i) => row[i] || '')), `Excel ${name} 与已保存结果逐格一致`).toEqual(expected)
        expect(workbook['口径说明'].some(row => row[0] === `结果 ${result.id} / 查询` && row[1] === result.query)).toBe(true)
        const bindings = workbook['口径说明'].find(row => row[0] === `结果 ${result.id} / 绑定参数（JSON）`)
        expect(bindings, 'Excel 保留本次执行的绑定参数').toBeTruthy()
        expect(JSON.parse(bindings[1])).toEqual(result.query_params)
      }
    } else {
      const document = new JSDOM(bytes.toString('utf8')).window.document
      expect(document.querySelector('script')).toBeNull()
      if (assistantBlocks(detail).some(block => block.type === 'chart')) expect(document.querySelector('svg'), '真实图表同时进入保存结果生成的报告').not.toBeNull()
      expect(document.querySelector('.meta').textContent).toContain(detail.run.id)
      expect(Array.from(document.querySelectorAll('h3')).filter(node => node.textContent.startsWith('结果 '))).toHaveLength(adopted.length)
      for (const result of adopted) {
        const heading = Array.from(document.querySelectorAll('h3')).find(node => node.textContent === `结果 ${result.id}`)
        expect(heading).toBeTruthy()
        const section = heading.closest('section')
        expect(Array.from(section.querySelectorAll('thead th'), cell => cell.textContent)).toEqual(result.columns)
        const rows = Array.from(section.querySelectorAll('tbody tr'), row => Array.from(row.querySelectorAll('td'), cell => cell.textContent))
        expect(rows).toEqual(result.rows.length ? result.rows.map(row => row.map(displayCell)) : [['没有匹配记录']])
        expect(section.querySelector('pre[aria-label="查询依据"]')?.textContent).toBe(result.query)
        expect(JSON.parse(section.querySelector('pre[aria-label="查询绑定参数"]')?.textContent)).toEqual(result.query_params)
      }
    }
  }
  await saveJSON(info, `${label}-export-index.json`, exports)
  return exports
}

const dailyMode = process.env.AGENT_LIVE_DAILY === '1'

// Everyday chat on the daily-v1 dataset (short placeholder contacts included):
// a direct server-template answer first, follow-ups keep the period, and no
// red error is ever shown for a common question.
test('日常口语：上周几条、追问来源与负责人、寒暄，直接回答且无红色错误', async ({ page, context, baseURL }, info) => {
  test.skip(!dailyMode, '仅在 AGENT_LIVE_DAILY=1 的 daily-v1 隔离服务上运行')
  await signIn(context, baseURL, live.token)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/agent')
  const evidence = { started_at: new Date().toISOString(), model: live.model, turns: [] }
  const turns = [
    ['上周共有几条线索进来？', /^上周（.+）一共进来 5 条新线索。/],
    ['那哪个来源最多？', /^上周（.+）按来源看，「好学校」最多，共 2 条/],
    ['按负责人拆开', /^上周（.+）按当前负责人拆开：.*员工乙 3 条/],
    ['你好', /^你好！/]
  ]
  for (const [index, [question, expected]] of turns.entries()) {
    const started = Date.now()
    await page.getByLabel('你的分析问题').fill(question)
    const accepted = page.waitForResponse(response => response.request().method() === 'POST' && /\/conversations\/[^/]+\/runs$/.test(new URL(response.url()).pathname))
    await page.getByRole('button', { name: '发送', exact: true }).click()
    const created = await (await accepted).json()
    let detail
    await expect.poll(async () => {
      detail = await getJSON(page.request, `/api/agent/runs/${created.id}`)
      return terminal.has(detail.run.status)
    }, { timeout: 120_000, intervals: [1000, 2000] }).toBe(true)
    const answer = detail.messages.find(message => message.role === 'assistant' && message.run_id === created.id)
    evidence.turns.push({ question, run_id: created.id, status: detail.run.status, model: detail.run.model, elapsed_ms: Date.now() - started, first_line: (answer?.content || '').split('\n')[0] })
    await saveJSON(info, 'daily-evidence.json', evidence)
    expect(detail.run.status, `${question}：${detail.run.error || ''}`).toBe('completed')
    expect(answer.content).toMatch(expected)
    const rendered = page.getByRole('article', { name: 'AI 回答', exact: true }).nth(index)
    await expect(rendered).toContainText(answer.content.split('\n')[0].slice(0, 12))
    await expect(page.locator('.agent-inline-error'), '常见问题不得显示红色错误').toHaveCount(0)
    if (index < 3) {
      // Default view: one natural sentence plus a compact table; every
      // internal detail (coverage, SQL, bindings, snapshot) stays collapsed.
      const card = rendered.locator('.agent-result').first()
      await expect(card).toBeVisible()
      await expect(card.locator('table')).toBeVisible()
      await expect(card.locator('details.agent-result-basis')).not.toHaveAttribute('open', /.*/)
      await expect(card.getByLabel('查询依据', { exact: true })).toBeHidden()
      await expect(card.getByText('分析范围：', { exact: false })).toBeHidden()
      expect(answer.content).not.toContain('- 指标：')
      if (index === 0) await page.screenshot({ path: info.outputPath('daily-first-answer.png') })
    }
    const exposed = JSON.stringify(detail)
    for (const fragment of fixture.independent_expected.forbidden_provider_payload_fragments) expect(exposed).not.toContain(fragment)
  }
  await page.screenshot({ path: info.outputPath('daily-chat.png'), fullPage: true })
  evidence.passed = true
  await saveJSON(info, 'daily-evidence.json', evidence)
})

test('真实云模型完成来源分析、好学校连续追问、证据及双格式导出，另一账号不可读取', async ({ page, context, browser, baseURL }, info) => {
  test.skip(dailyMode, 'daily-v1 模式只跑日常口语用例')
  expect(sha256(fixtureBytes), '真实服务必须使用同一业务 fixture').toBe(live.fixture_hash)
  await signIn(context, baseURL, live.token)
  await installSSEDisconnect(page)
  const identity = await getJSON(page.request, '/api/user/info')
  expect(identity.role).toBe(live.account.role)
  expect(identity.username).toBe(live.account.username)
  const settingsBefore = await getJSON(page.request, '/api/agent/settings')
  expect(settingsBefore.enabled).toBe(true)
  expect(settingsBefore.configured).toBe(true)
  const evidence = {
    started_at: new Date().toISOString(), fixture_hash: live.fixture_hash, model: live.model,
    questions: [firstQuestion, followupQuestion], runs: [], run_posts: 0,
    usage_scope: '隔离服务月账本前后快照；按 run_id 与服务端调用记录核对，若并行测试不得将差值当成本用例费用。',
    usage_before: settingsBefore.usage, passed: false
  }
  evidence.expected_provider_switch = expectProviderSwitch
  evidence.fault_injection = faultInjection
  evidence.provider_failure_basis = faultInjection ? '隔离夹具合成主模型503，备用回答实际调用云模型；不是线上供应商故障。' : '没有配置隔离故障注入；以已记录供应商返回为准。'
  page.on('request', request => {
    if (request.method() === 'POST' && /\/conversations\/[^/]+\/runs$/.test(new URL(request.url()).pathname)) evidence.run_posts++
  })
  try {
    await page.setViewportSize({ width: 1440, height: 1000 })
    await page.goto('/stats/sla')
    await expect(page).toHaveURL(/\/agent$/)
    const first = await ask(page, info, firstQuestion, '01-sources', evidence, true)
    expect(evidence.run_posts, '刷新只恢复订阅，第一轮只创建一次任务').toBe(1)
    const sourceResult = sourceCountResult(first.detail)
    const charts = assistantBlocks(first.detail).filter(block => block.type === 'chart' && block.result_id === sourceResult.id)
    expect(charts, '真实模型须明确给出绑定来源结果的柱状图，只有表格不通过').toHaveLength(1)
    expect(charts[0].chart_type).toBe('bar')
    expect(sourceResult.columns).toContain(charts[0].x_column)
    expect(charts[0].y_columns).toHaveLength(1)
    const x = sourceResult.columns.indexOf(charts[0].x_column), y = sourceResult.columns.indexOf(charts[0].y_columns[0])
    expect(y).toBeGreaterThanOrEqual(0)
    for (const row of sourceResult.rows) expect(row[y]).toBe(fixture.independent_expected.sources[row[x]])
    await expect(first.article.locator(`.agent-result[data-result-id="${sourceResult.id}"] .agent-chart[role="img"]`)).toBeVisible()
    await expect(first.article.locator(`.agent-result[data-result-id="${sourceResult.id}"] .agent-chart canvas`).first()).toBeVisible()
    evidence.runs[0].chart_verified_result_id = sourceResult.id
    await verifyRenderedResult(first.article, first.detail, sourceResult)
    evidence.runs[0].ui_verified_result_ids = [sourceResult.id]
    const exports = await verifyExports(page, first.article, first.detail, info, '01-sources')
    await page.screenshot({ path: info.outputPath('01-real-cloud-desktop-1440.png'), fullPage: true })

    const second = await ask(page, info, followupQuestion, '02-good-school', evidence)
    expect(evidence.run_posts, '两个问题各只创建一次任务').toBe(2)
    const grouped = ownerResult(second.detail)
    await verifyRenderedResult(second.article, second.detail, grouped)
    evidence.runs[1].ui_verified_result_ids = [grouped.id]
    const sources = [...assistantBlocks(second.detail), ...finalResults(second.detail)].flatMap(block => block.sources || [])
    expect(sources.some(source => [1, 2].includes(source.lead_id)), '定性结论需要真实好学校记录依据').toBe(true)
    for (const source of sources) {
      const table = fixture.tables[source.type]
      expect(table?.some(row => row.id === Number(source.id) && (row.lead_id || row.id) === source.lead_id), `真实来源 ${source.type}:${source.id}`).toBe(true)
    }
    const secondExports = await verifyExports(page, second.article, second.detail, info, '02-good-school')
    const conversationURL = page.url()
    const conversationId = second.detail.run.conversation_id
    for (const width of [1440, 768, 320, 375]) {
      await page.setViewportSize({ width, height: width >= 768 ? 1000 : 812 })
      await expect(page.getByLabel('你的分析问题')).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollHeight <= innerHeight + 1), `${width}px 不产生整页空白滚动`).toBe(true)
      await page.evaluate(() => window.scrollTo(0, 10000))
      expect(await page.evaluate(() => window.scrollY), '对话仅在内部消息区域滚动').toBe(0)
      await expect(page.getByLabel('你的分析问题')).toBeInViewport()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${width}px 不横向溢出`).toBe(true)
      await page.screenshot({ path: info.outputPath(`02-real-cloud-width-${width}.png`), fullPage: true })
    }
    const recordResult = finalResults(second.detail).find(result => result.sources?.some(source => source.type === 'lead_followups' && [1, 2].includes(source.lead_id)))
    expect(recordResult, '顾虑分析必须实际查阅好学校的跟进记录，并可在依据区核对').toBeTruthy()
    await verifyRenderedResult(second.article, second.detail, recordResult)
    evidence.runs[1].ui_verified_result_ids.push(recordResult.id)
    const linkedSource = recordResult.sources.find(source => source.type === 'lead_followups' && [1, 2].includes(source.lead_id))
    const linkedLead = fixture.tables.leads.find(lead => lead.id === linkedSource.lead_id)
    // The answer's own basis stays collapsed; the evidence panel opened above
    // shows the same record link.
    const evidenceLink = second.article.locator(`a[href^="/leads/${linkedSource.lead_id}?"]:visible`).first()
    const leadResponsePromise = page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === `/api/leads/${linkedSource.lead_id}`)
    await evidenceLink.click()
    await expect(page).toHaveURL(/\/leads\/[12]\?return_to=/)
    const leadResponse = await leadResponsePromise
    expect(leadResponse.status(), '证据记录必须通过本地授权接口成功读取').toBe(200)
    const body = await leadResponse.json()
    const actualLead = body.lead || body
    expect(actualLead.id).toBe(linkedLead.id)
    expect(actualLead.lead_no).toBe(linkedLead.lead_no)
    await expect(page.getByRole('heading', { name: linkedLead.customer_name, exact: true })).toBeVisible()
    await page.goBack()
    await expect(page).toHaveURL(conversationURL)
    await expect(page.getByRole('article', { name: '你的问题', exact: true })).toHaveCount(2)
    await page.reload()
    await expect(page.getByRole('article', { name: 'AI 回答', exact: true })).toHaveCount(2)
    const resumed = await getJSON(page.request, `/api/agent/conversations/${conversationId}`)
    expect(resumed.runs.map(run => run.id).sort()).toEqual(evidence.runs.map(run => run.id).sort())
    expect(evidence.run_posts, '下钻返回和再次刷新没有创建付费任务').toBe(2)
    expect((await getJSON(page.request, `/api/agent/runs/${first.detail.run.id}`)).results).toEqual(first.detail.results)

    const other = await browser.newContext({ baseURL, viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' })
    try {
      await signIn(other, baseURL, live.second_token)
      const otherIdentity = await getJSON(other.request, '/api/user/info')
      expect(otherIdentity.role).toBe(live.second_account.role)
      expect(otherIdentity.username).toBe(live.second_account.username)
      expect(otherIdentity.username).not.toBe(identity.username)
      evidence.private_probe_role = otherIdentity.role
      const privatePaths = [
        `/api/agent/conversations/${conversationId}`,
        ...[first.detail, second.detail].flatMap(detail => [
          `/api/agent/runs/${detail.run.id}`, `/api/agent/runs/${detail.run.id}/events`,
          ...detail.results.map(result => `/api/agent/results/${result.id}`)
        ]),
        ...[...exports, ...secondExports].map(item => `/api/agent/exports/${item.id}/download`)
      ]
      evidence.private_access = []
      for (const path of privatePaths) {
        const response = await other.request.get(path)
        evidence.private_access.push({ path, status: response.status() })
        expect(response.status(), '另一隔离账号不能读取私人资源').toBe(403)
      }
      const otherPage = await other.newPage()
      const historyResponsePromise = otherPage.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === '/api/agent/conversations')
      await otherPage.goto('/agent')
      const historyResponse = await historyResponsePromise
      expect(historyResponse.status()).toBe(200)
      const otherHistory = await historyResponse.json()
      expect(otherHistory.conversations.some(conversation => conversation.id === conversationId), '另一账号的已返回历史中不能出现私人会话').toBe(false)
      await expect(otherPage.locator(`a[href="/agent/${conversationId}"]`)).toHaveCount(0)
      await otherPage.screenshot({ path: info.outputPath('03-other-account-private-history.png'), fullPage: true })
    } finally { await other.close() }
    evidence.cancellation = await verifyCancelledDraft(page, '请分别查询来源分布、实际跟进操作人和预约记录，再综合分析当前业务数据。')
    expect(evidence.run_posts, '两项完成分析加一项明确停止的分析；重放与刷新均不创建新任务').toBe(3)
    await saveJSON(info, '03-cancelled-run.json', evidence.cancellation)
    evidence.passed = true
  } finally {
    evidence.finished_at = new Date().toISOString()
    try {
      evidence.usage_after = (await getJSON(page.request, '/api/agent/settings')).usage
    } catch {
      evidence.usage_after_unavailable = true
      evidence.usage_after_note = '最终账本读取未成功；请按已保存运行 ID 核对服务端账本，不视为零费用。'
    }
    await saveJSON(info, 'real-cloud-browser-evidence.json', evidence)
  }
})
