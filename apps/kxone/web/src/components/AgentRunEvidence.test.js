import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import AgentRunEvidence from './AgentRunEvidence.vue'
import { agentAPI } from '@/api/agent'

vi.mock('@/api/agent', () => ({ agentAPI: { run: vi.fn(), result: vi.fn() } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ fullPath: '/agent/private-conversation' }) }))
const run = { id: 'saved-run', status: 'completed', snapshot_at: '2026-10-08T00:00:00Z' }
const result = { id: 'saved-result', columns: ['生命周期首跟秒数', '本次负责人首跟秒数'], rows: [[600, 1800]], total_rows: 1, returned_rows: 1, truncated: false, snapshot_at: run.snapshot_at, query: 'SELECT lifecycle_first_followup_seconds,owner_first_followup_seconds FROM leads WHERE id=?', query_params: [1], materialized_rows: 1, materialized_complete: true }
const render = props => mount(AgentRunEvidence, { props: { runId: run.id, run, ...props }, global: { stubs: { RouterLink: true } } })
const expand = async wrapper => {
  wrapper.find('details').element.open = true
  // Setting open emits the native toggle; a second synthetic toggle would
  // incorrectly simulate an immediate retry after a rejected response.
  await new Promise(resolve => setTimeout(resolve, 0))
  await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks()
  agentAPI.run.mockResolvedValue({ run, messages: [{ role: 'assistant', content: '首次响应分别为10分钟、30分钟。', blocks: [{ type: 'markdown', text: '首次响应分别为10分钟、30分钟。' }] }], results: [result], result_selection: { status: 'explicit', result_ids: [result.id], process_result_ids: [] } })
})

it('shows the saved snapshot and query values even when the model supplied only Markdown', async () => {
  const wrapper = render(); await flushPromises()
  expect(wrapper.find('time').attributes('datetime')).toBe(run.snapshot_at)
  expect(wrapper.find('time').text()).toContain('08:00:00')
  expect(agentAPI.run).not.toHaveBeenCalled()
  await expand(wrapper)
  expect(agentAPI.run).toHaveBeenCalledExactlyOnceWith(run.id)
  expect(wrapper.find('tbody').text()).toContain('600')
  expect(wrapper.find('tbody').text()).toContain('1800')
  expect(wrapper.find('[aria-label="查询依据"]').text()).toBe(result.query)
  expect(JSON.parse(wrapper.find('[aria-label="查询绑定参数"]').text())).toEqual([1])
  expect(agentAPI.result).not.toHaveBeenCalled()
  expect(wrapper.text()).toContain('打开客户记录会显示该客户的当前资料')
  wrapper.unmount()
})

it('fetches the immutable saved run for an older message with no conversation summary', async () => {
  const wrapper = render({ run: null }); await flushPromises()
  expect(agentAPI.run).toHaveBeenCalledExactlyOnceWith(run.id)
  expect(wrapper.find('time').attributes('datetime')).toBe(run.snapshot_at)
  await expand(wrapper)
  expect(agentAPI.run).toHaveBeenCalledTimes(1)
  expect(wrapper.find('tbody').text()).toContain('1800')
  wrapper.unmount()
})

it('rejects an unrelated run response and offers a retry without showing foreign results', async () => {
  agentAPI.run.mockResolvedValueOnce({ run: { ...run, id: 'other-private-run' }, results: [{ ...result, rows: [[999, 999]] }] })
  const wrapper = render(); await expand(wrapper)
  expect(wrapper.find('[role="alert"]').text()).toContain('依据不完整')
  expect(wrapper.find('tbody').exists()).toBe(false)
  await wrapper.find('button').trigger('click'); await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(wrapper.find('tbody').text()).toContain('600')
  wrapper.unmount()
})

it('discards an old response after switching to a different run', async () => {
  let finishOld
  agentAPI.run.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
  const wrapper = render({ run: null })
  const next = { ...run, id: 'new-run' }
  await wrapper.setProps({ runId: next.id, run: next })
  finishOld({ run, results: [result] }); await flushPromises()
  expect(wrapper.find('tbody').exists()).toBe(false)
  expect(wrapper.emitted('result')).toBeUndefined()
  agentAPI.run.mockResolvedValueOnce({ run: next, results: [] })
  await expand(wrapper)
  expect(wrapper.text()).toContain('本次没有保存数据查询结果')
  wrapper.unmount()
})

it('does not invent a snapshot for a failed run that never obtained one', async () => {
  agentAPI.run.mockResolvedValue({ run: { id: run.id, status: 'failed' }, results: [] })
  const wrapper = render({ run: null }); await flushPromises()
  expect(wrapper.find('time').exists()).toBe(false)
  expect(wrapper.text()).toContain('本次未建立数据快照')
  await expand(wrapper)
  expect(wrapper.text()).toContain('本次没有保存数据查询结果')
  wrapper.unmount()
})

it('separates an exploratory wrong count from the final corrected result', async () => {
  const process = { ...result, id: 'exploratory', columns: ['线索数'], rows: [[49]], query: 'SELECT COUNT(*) FROM leads a JOIN leads b ON 1=1' }
  const final = { ...result, columns: ['线索数'], rows: [[7]], query: 'SELECT COUNT(*) FROM leads' }
  agentAPI.run.mockResolvedValue({ run, results: [process, final], result_selection: { status: 'explicit', result_ids: [final.id], process_result_ids: [process.id] } })
  const wrapper = render(); await expand(wrapper)
  const adopted = wrapper.find('[aria-label="最终采用的查询结果"]')
  expect(adopted.find('tbody').text()).toBe('7')
  expect(adopted.text()).not.toContain('49')
  const queries = wrapper.find('.agent-query-process')
  expect(queries.element.open).toBe(false)
  expect(queries.find('summary').text()).toContain('未作为结论导出')
  queries.element.open = true; await flushPromises()
  expect(queries.find('tbody').text()).toBe('49')
  wrapper.unmount()
})

it('does not infer a final result for an old Markdown answer and rejects foreign classifications', async () => {
  agentAPI.run.mockResolvedValueOnce({ run, results: [result] })
  const wrapper = render(); await expand(wrapper)
  expect(wrapper.find('[aria-label="最终采用的查询结果"]').exists()).toBe(false)
  expect(wrapper.text()).toContain('历史回答未提供最终引用清单')
  expect(wrapper.find('.agent-query-process').element.open).toBe(false)
  wrapper.unmount()
  agentAPI.run.mockResolvedValueOnce({ run, results: [result], result_selection: { status: 'explicit', result_ids: ['another-private-result'], process_result_ids: [] } })
  const invalid = render(); await expand(invalid)
  expect(invalid.find('[role="alert"]').text()).toContain('结果采用范围与保存结果不一致')
  expect(invalid.find('tbody').exists()).toBe(false)
  invalid.unmount()
})
