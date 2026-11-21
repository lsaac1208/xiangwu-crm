import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import AgentResult from './AgentResult.vue'
import { agentAPI } from '@/api/agent'
vi.mock('@/api/agent', () => ({ agentAPI: { result: vi.fn() } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ fullPath: '/agent/conversation-one' }) }))
const first = { id: 'persisted-result', columns: ['记录'], rows: Array.from({ length: 50 }, (_, index) => [index + 1]), total_rows: 100, returned_rows: 50, truncated: true }
beforeEach(() => { vi.clearAllMocks() })
const render = (result = first) => mount(AgentResult, { props: { block: { type: 'table', title: '历史明细', result_id: first.id }, results: { [first.id]: result } }, global: { stubs: { RouterLink: true } } })
it('loads additional rows from the saved result and preserves local display pagination', async () => {
  agentAPI.result.mockResolvedValue({ ...first, rows: Array.from({ length: 50 }, (_, index) => [index + 51]), returned_rows: 50, truncated: false })
  const wrapper = render(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '读取更多明细').trigger('click'); await flushPromises()
  expect(agentAPI.result).toHaveBeenCalledWith(first.id, 50, 200)
  expect(wrapper.text()).not.toContain('当前已读取')
  await wrapper.findAll('button').find(button => button.text() === '下一页').trigger('click')
  expect(wrapper.find('tbody').text()).toContain('100')
  expect(wrapper.emitted('result')[0][0].rows).toHaveLength(100)
  wrapper.unmount()
})
it('rejects a mismatching result page while preserving already shown records', async () => {
  agentAPI.result.mockResolvedValue({ ...first, id: 'foreign-result' })
  const wrapper = render(); await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '读取更多明细').trigger('click'); await flushPromises()
  expect(wrapper.find('[role="alert"]').text()).toBe('更多明细读取失败，请重试')
  expect(wrapper.find('tbody').text()).toContain('50')
  expect(wrapper.emitted('result')).toBeUndefined()
  wrapper.unmount()
})
it('shows the saved binding values as inert JSON alongside the original SQL', async () => {
  const result = { ...first, query: 'SELECT record FROM analysis_records WHERE lead_id=? AND text LIKE ?', query_params: [1, '%费用%', null, false, '<img src=x onerror=alert(1)>'] }
  const wrapper = render(result); await flushPromises()
  expect(wrapper.find('[aria-label="查询依据"]').text()).toBe(result.query)
  expect(JSON.parse(wrapper.find('[aria-label="查询绑定参数"]').text())).toEqual(result.query_params)
  expect(wrapper.find('img').exists()).toBe(false)
  expect(agentAPI.result).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('distinguishes an unparameterized query from historical bindings that were never saved', async () => {
  const result = { ...first, query: 'SELECT record FROM analysis_records', query_params: [] }
  const wrapper = render(result); await flushPromises()
  expect(wrapper.find('[aria-label="查询绑定参数"]').text()).toBe('[]')
  await wrapper.setProps({ results: { [first.id]: { ...first, query: 'SELECT record FROM analysis_records WHERE lead_id=?' } } })
  expect(wrapper.find('[aria-label="查询绑定参数"]').text()).toBe('未保存：该结果缺少绑定参数，含占位符的筛选值无法核对。')
  wrapper.unmount()
})

it('keeps the answer chart on the report slice while additional table pages remain readable', async () => {
  const rows = Array.from({ length: 300 }, (_, index) => [`类别${index + 1}`, index + 1])
  const saved = { id: 'chart-saved', columns: ['类别', '数量'], rows: rows.slice(0, 200), total_rows: 300, returned_rows: 200, offset: 0, snapshot_at: '2026-10-13T00:00:00Z', materialized_rows: 300, materialized_complete: true, truncated: true }
  const preview = { ...saved, rows: rows.slice(0, 10), returned_rows: 10 }
  const block = { type: 'chart', title: '固定范围', result_id: saved.id, result: preview, chart_type: 'bar', x_column: '类别', y_columns: ['数量'] }
  agentAPI.result.mockResolvedValue({ ...saved, rows: rows.slice(200), offset: 200, returned_rows: 100, truncated: false })
  const wrapper = mount(AgentResult, { props: { block, results: { [saved.id]: saved } }, global: { stubs: { RouterLink: true, AgentChart: { name: 'AgentChart', props: ['option'], template: '<div />' } } } })
  await flushPromises()
  expect(wrapper.findComponent({ name: 'AgentChart' }).props('option').series[0].data).toEqual(rows.slice(0, 10).map(row => row[1]))
  await wrapper.findAll('button').find(button => button.text() === '读取更多明细').trigger('click'); await flushPromises()
  expect(wrapper.emitted('result')[0][0].rows).toHaveLength(300)
  expect(wrapper.findComponent({ name: 'AgentChart' }).props('option').series[0].data).toHaveLength(10)
  expect(wrapper.text()).toContain('图表固定显示第1至10行')
  expect(wrapper.text()).toContain('共 300 行')
  wrapper.unmount()
})

it('reads a fixed chart slice outside the table preview without replacing the table page', async () => {
  const rows = Array.from({ length: 300 }, (_, index) => [`类别${index + 1}`, index + 1])
  const saved = { id: 'chart-offset', columns: ['类别', '数量'], rows: rows.slice(0, 200), total_rows: 300, offset: 0, snapshot_at: '2026-10-13T00:00:00Z', materialized_rows: 300, truncated: true }
  const preview = { ...saved, rows: [['untrusted inline value', 999999]], offset: 210 }
  const block = { type: 'chart', result_id: saved.id, result: preview, chart_type: 'bar', x_column: '类别', y_columns: ['数量'] }
  agentAPI.result.mockResolvedValue({ ...saved, rows: [rows[210]], offset: 210 })
  const wrapper = mount(AgentResult, { props: { block, results: { [saved.id]: saved } }, global: { stubs: { RouterLink: true, AgentChart: { name: 'AgentChart', props: ['option'], template: '<div />' } } } })
  await flushPromises()
  expect(agentAPI.result).toHaveBeenCalledWith(saved.id, 210, 1)
  expect(wrapper.findComponent({ name: 'AgentChart' }).props('option').series[0].data).toEqual([211])
  expect(wrapper.text()).toContain('图表固定显示第211至211行')
  expect(wrapper.find('tbody').text()).toContain('类别1')
  expect(wrapper.find('tbody').text()).not.toContain('类别211')
  expect(wrapper.emitted('result')).toBeUndefined()
  wrapper.unmount()
})

it('shows the table by default and keeps coverage, SQL and bindings in a collapsed basis', async () => {
  const result = { ...first, rows: [[3]], total_rows: 1, returned_rows: 1, truncated: false, coverage: '同一脱敏快照', query: 'SELECT COUNT(*) FROM leads l WHERE l.created_at_date_shanghai>=?', query_params: ['2026-10-14'] }
  const wrapper = render(result); await flushPromises()
  const basis = wrapper.find('details.agent-result-basis')
  expect(basis.exists()).toBe(true)
  expect(basis.attributes('open')).toBeUndefined()
  expect(basis.find('summary').text()).toBe('依据')
  expect(basis.text()).toContain('分析范围：同一脱敏快照')
  expect(basis.find('[aria-label="查询依据"]').exists()).toBe(true)
  expect(wrapper.find('.agent-result-data').element.tagName).toBe('DIV')
  expect(wrapper.find('.agent-result-data tbody').text()).toBe('3')
  wrapper.unmount()
})
