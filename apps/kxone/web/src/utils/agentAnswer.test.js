import { describe, expect, it } from 'vitest'
import { chartOption, fixedChartResult, renderAgentMarkdown, safeResult } from './agentAnswer'

describe('safe Agent answer rendering', () => {
  it('renders useful Markdown while rejecting executable HTML, images and links', () => {
    const html = renderAgentMarkdown('**结果**\n\n<script>window.stolen = true</script>\n\n[恶意](javascript:alert(1))\n\n![外部追踪](https://tracking.example/image)\n\n[文档](https://example.com/docs)')
    const container = document.createElement('div'); container.innerHTML = html
    expect(container.querySelector('strong')?.textContent).toBe('结果')
    expect(container.querySelector('script')).toBeNull()
    expect(container.querySelector('img')).toBeNull()
    expect([...container.querySelectorAll('a')].map(link => link.getAttribute('href'))).toEqual(['https://example.com/docs'])
  })
  it('rejects inline model data and a mismatching result identity', () => {
    const result = { id: 'real', columns: ['来源', '数量'], rows: [['抖音', 12]] }
    expect(safeResult({ result_id: 'real', result }, {})).toBeNull()
    expect(safeResult({ result_id: 'other' }, { other: result })).toBeNull()
    expect(safeResult({ result_id: 'real' }, { real: result })).toBe(result)
  })
  it('preserves unsupported evidence-link labels as ordinary text rather than fake clickable sources', () => {
    const html = renderAgentMarkdown('[打开统计表](result://real-id)\n\n[**客户#1 跟进记录**](record://lead_followups/1)\n\n[全部记录](records://search/real-id)\n\n[未知协议](madeup://id)\n\n[空链接]()\n\n[文档](https://example.com/docs) [页面](/agent) [章节](#section)')
    const container = document.createElement('div'); container.innerHTML = html
    expect([...container.querySelectorAll('a')].map(link => link.getAttribute('href'))).toEqual(['https://example.com/docs', '/agent', '#section'])
    for (const label of ['打开统计表', '客户#1 跟进记录', '全部记录', '未知协议', '空链接']) expect(container.textContent).toContain(label)
    expect(container.querySelector('strong')?.textContent).toBe('客户#1 跟进记录')
    expect(container.querySelector('a:not([href])')).toBeNull()
    expect(container.querySelector('a[href*="lead_followups"]')).toBeNull()
    expect(container.querySelector('a[href*="real-id"]')).toBeNull()
  })
  it('replaces unbound Markdown tables without exposing invented cells or breaking surrounding prose', () => {
    const html = renderAgentMarkdown('已核对。\n\n| 来源 | 数量 |\n| --- | ---: |\n| 假渠道 | 999999 |\n\n继续查看依据。\n\n> | 来源 | 数量 |\n> | --- | --- |\n> | 引用假渠道 | 888888 |')
    const container = document.createElement('div'); container.innerHTML = html
    expect(container.querySelector('table')).toBeNull()
    expect(container.textContent).not.toContain('999999')
    expect(container.textContent).not.toContain('888888')
    expect(container.textContent).not.toContain('假渠道')
    expect(container.textContent.match(/数据表请查看下方查询依据。/g)).toHaveLength(2)
    expect(container.textContent).toContain('已核对。')
    expect(container.textContent).toContain('继续查看依据。')
  })
  it('keeps literal code readable without interpreting it as an authoritative table', () => {
    const html = renderAgentMarkdown('```text\n| source | count |\n| --- | --- |\n| example | 123 |\n```')
    const container = document.createElement('div'); container.innerHTML = html
    expect(container.querySelector('table')).toBeNull()
    expect(container.querySelector('pre')?.getAttribute('tabindex')).toBe('0')
    expect(container.querySelector('code')?.textContent).toContain('| example | 123 |')
    expect(container.textContent).not.toContain('数据表请查看下方查询依据。')
  })
  it('constructs charts only from saved rows and recognized columns', () => {
    const result = { columns: ['来源', '数量'], rows: [['抖音', 12], ['未知', null], ['小红书', 'bad']] }
    const option = chartOption({ chart_type: 'bar', x_column: '来源', y_columns: ['数量', '假数值'] }, result)
    expect(option.series).toHaveLength(1)
    expect(option.series[0].data).toEqual([12, null, null])
    expect(option.tooltip.renderMode).toBe('richText')
    expect(chartOption({ chart_type: 'custom', x_column: '来源', y_columns: ['数量'] }, result)).toBeNull()
    expect(chartOption({ chart_type: 'bar', x_column: '不存在', y_columns: ['数量'] }, result)).toBeNull()
  })
  it('binds a fixed chart slice to authenticated saved values, including nonzero offsets', () => {
    const saved = { id: 'saved', snapshot_at: '2026-10-13T00:00:00Z', columns: ['label', 'count'], rows: [['A', 1], ['B', 2], ['C', 3]], offset: 0, total_rows: 3 }
    const block = { result_id: saved.id, result: { ...saved, offset: 1, rows: [['invented', 999]] } }
    expect(fixedChartResult(block, saved).rows).toEqual([['B', 2]])
    expect(fixedChartResult(block, saved).offset).toBe(1)
    expect(fixedChartResult(block, { ...saved, rows: saved.rows.slice(0, 1) })).toBeNull()
    expect(fixedChartResult(block, { ...saved, id: 'other' })).toBeNull()
    expect(() => fixedChartResult(block, { ...saved, snapshot_at: '2026-10-14T00:00:00Z' })).toThrow('不一致')
    expect(() => fixedChartResult({ ...block, result: { ...block.result, offset: -1 } }, saved)).toThrow('不完整')
    expect(fixedChartResult(block, { ...saved, offset: 1, rows: saved.rows.slice(1) }).rows).toEqual([['B', 2]])
  })
  it('matches the report legacy chart cap without depending on currently loaded table rows', () => {
    const saved = { id: 'saved', columns: ['x'], rows: Array.from({ length: 600 }, (_, i) => [i]), offset: 0, total_rows: 600, materialized_rows: 600 }
    expect(fixedChartResult({ result_id: saved.id }, { ...saved, rows: saved.rows.slice(0, 200) })).toBeNull()
    expect(fixedChartResult({ result_id: saved.id }, saved).rows).toHaveLength(500)
    expect(fixedChartResult({ result_id: saved.id }, { ...saved, rows: saved.rows.slice(0, 50), materialized_rows: 0, has_more: false }).rows).toHaveLength(50)
  })
})
