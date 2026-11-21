import { expect } from '@playwright/test'
import { readFile } from 'node:fs/promises'

// Cut only the browser's real SSE transport after an authentic event arrives.
// No response body, model answer, event ID or backend state is fabricated.
export async function installSSEDisconnect(page) {
  await page.addInitScript(() => {
    const key = 'agent-browser-recovery-evidence'
    const read = () => JSON.parse(sessionStorage.getItem(key) || '{"requests":[],"cut":false}')
    const save = value => sessionStorage.setItem(key, JSON.stringify(value))
    const originalFetch = window.fetch.bind(window)
    window.fetch = async (input, init = {}) => {
      const url = new URL(typeof input === 'string' ? input : input.url, location.href)
      const match = url.pathname.match(/^\/api\/agent\/runs\/([^/]+)\/events$/)
      if (!match) return originalFetch(input, init)
      const state = read()
      state.requests.push({ run_id: match[1], after: Number(url.searchParams.get('after') || 0) })
      save(state)
      if (state.cut || state.armed) return originalFetch(input, init)
      state.armed = true; save(state)
      const controller = new AbortController()
      const signal = init.signal ? AbortSignal.any([init.signal, controller.signal]) : controller.signal
      const response = await originalFetch(input, { ...init, signal })
      const reader = response.clone().body.getReader()
      void (async () => {
        let pending = '', seq = 0
        const decoder = new TextDecoder()
        try {
          while (!seq) {
            const chunk = await reader.read()
            if (chunk.done) return
            pending += decoder.decode(chunk.value, { stream: true })
            const lines = pending.split(/\r?\n/); pending = lines.pop()
            for (const line of lines) {
              if (line.startsWith('data:')) {
                const event = JSON.parse(line.slice(5).trim())
                seq = Math.max(seq, Number(event.seq || 0))
              }
            }
          }
          // Let the application consume the same genuine event before cutting.
          await new Promise(resolve => setTimeout(resolve, 75))
          const updated = read()
          updated.cut = true; updated.run_id = match[1]; updated.cut_after_seq = seq
          save(updated)
          controller.abort()
        } catch { /* Expected transport abortion; evidence remains in sessionStorage. */ }
        finally { await reader.cancel().catch(() => {}) }
      })()
      return response
    }
  })
}

export async function verifySSERecovery(page, runId) {
  const state = () => page.evaluate(() => JSON.parse(sessionStorage.getItem('agent-browser-recovery-evidence') || '{}'))
  await expect.poll(async () => {
    const evidence = await state()
    return evidence.cut === true && evidence.run_id === runId && evidence.cut_after_seq > 0
      && evidence.requests.some(item => item.run_id === runId && item.after >= evidence.cut_after_seq)
  }, { timeout: 15_000, message: '真实 SSE 断流后仅按已接收游标重新订阅原运行' }).toBe(true)
  return state()
}

export async function replayAcceptedRequest(request, response, expectedRunId) {
  const original = response.request()
  const payload = original.postDataJSON()
  expect(payload.request_id, '重放服务端已接收的同一个幂等标识').toBeTruthy()
  const repeated = await request.post(original.url(), { data: payload })
  expect(repeated.status()).toBe(202)
  const run = await repeated.json()
  expect(run.id).toBe(expectedRunId)
  return { path: new URL(original.url()).pathname, request_id: payload.request_id, run_id: run.id }
}

export async function usageEvents(request, runId) {
  const response = await request.get(`/api/agent/runs/${runId}/events`)
  expect(response.status()).toBe(200)
  return (await response.text()).split(/\r?\n/).filter(line => line.startsWith('data:'))
    .map(line => JSON.parse(line.slice(5).trim())).filter(event => event.type === 'model_usage')
    .map(event => ({ seq: event.seq, ...event.data }))
}

export async function verifyCompletedReplay(request, response, runId) {
  const before = await usageEvents(request, runId)
  expect(before.length).toBeGreaterThan(0)
  const replay = await replayAcceptedRequest(request, response, runId)
  const after = await usageEvents(request, runId)
  expect(after, '终态幂等重放没有新增模型调用、预留或结算事件').toEqual(before)
  return { ...replay, usage_unchanged: true, model_call_count: after.length }
}

export async function verifyCancelledDraft(page, question) {
  const beforeAnswers = await page.getByRole('article', { name: 'AI 回答', exact: true }).count()
  await page.getByLabel('你的分析问题').fill(question)
  const accepted = page.waitForResponse(response => response.request().method() === 'POST' && /\/conversations\/[^/]+\/runs$/.test(new URL(response.url()).pathname))
  await page.getByRole('button', { name: '发送', exact: true }).click()
  const response = await accepted
  expect(response.status()).toBe(202)
  const run = await response.json()
  await expect(page.getByRole('button', { name: '停止生成', exact: true })).toBeVisible()
  let firstUsage
  try {
    firstUsage = await page.evaluate(async id => {
      const response = await fetch(`/api/agent/runs/${id}/events`, { signal: AbortSignal.timeout(45_000) })
      if (!response.ok) throw new Error('Cannot observe the running analysis')
      const reader = response.body.getReader(), decoder = new TextDecoder()
      let pending = ''
      try {
        while (true) {
          const chunk = await reader.read()
          if (chunk.done) throw new Error('Analysis ended before cancellation checkpoint')
          pending += decoder.decode(chunk.value, { stream: true })
          const lines = pending.split(/\r?\n/); pending = lines.pop()
          for (const line of lines) {
            if (!line.startsWith('data:')) continue
            const event = JSON.parse(line.slice(5).trim())
            if (event.type === 'model_usage') return { seq: event.seq, ...event.data }
          }
        }
      } finally { await reader.cancel() }
    }, run.id)
  } catch (error) {
    await page.request.post(`/api/agent/runs/${run.id}/cancel`, { data: {} })
    throw error
  }
  await page.getByRole('button', { name: '停止生成', exact: true }).click()
  let detail
  await expect.poll(async () => {
    detail = await (await page.request.get(`/api/agent/runs/${run.id}`)).json()
    return detail.run.status
  }, { timeout: 15_000 }).toBe('cancelled')
  const answer = detail.messages.find(item => item.role === 'assistant' && item.run_id === run.id)
  expect(answer.status).toBe('cancelled')
  expect(answer.content, '取消后只显示服务端停止说明，不发布模型草稿').toBe(detail.run.error)
  expect((answer.blocks || []).filter(block => block.type === 'markdown' || block.verification?.status === 'validated'), '取消不能发布未核验草稿').toEqual([])
  expect(detail.result_selection.status).toBe('unfinished')
  await expect(page.getByRole('button', { name: '停止生成', exact: true })).toHaveCount(0)
  const afterUsage = await usageEvents(page.request, run.id)
  await page.reload()
  await expect(page.getByLabel('你的分析问题')).toBeVisible()
  expect((await (await page.request.get(`/api/agent/runs/${run.id}`)).json()).run.status).toBe('cancelled')
  expect(await usageEvents(page.request, run.id), '停止后刷新不能继续模型调用或重新收费').toEqual(afterUsage)
  await expect(page.getByRole('article', { name: 'AI 回答', exact: true })).toHaveCount(beforeAnswers + 1)
  const article = page.getByRole('article', { name: 'AI 回答', exact: true }).last()
  const downloaded = page.waitForEvent('download')
  await article.getByRole('button', { name: '导出报告', exact: true }).click()
  const file = await downloaded
  expect(await file.failure()).toBeNull()
  const html = await readFile(await file.path(), 'utf8')
  expect(html, '取消导出只能保留未完成说明').toContain('本轮分析未完成')
  expect(html).not.toContain('<h3>结果 ')
  return { run_id: run.id, conversation_id: run.conversation_id, status: detail.run.status, final_message: answer, first_model_usage: firstUsage, model_usage: afterUsage, draft_published: false }
}
