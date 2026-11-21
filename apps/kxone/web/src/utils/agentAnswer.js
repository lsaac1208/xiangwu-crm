import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'

const markdown = new MarkdownIt({ html: false, linkify: true, breaks: true })
// A model-authored Markdown table cannot prove which saved result supplied its
// cells. Replace that entire block; AgentResult renders the authoritative rows.
markdown.core.ruler.after('block', 'bound_result_tables', state => {
  const tokens = []
  for (let index = 0; index < state.tokens.length; index++) {
    const token = state.tokens[index]
    if (token.type !== 'table_open') { tokens.push(token); continue }
    while (index < state.tokens.length && state.tokens[index].type !== 'table_close') index++
    const inline = new state.Token('inline', '', 0)
    inline.content = '数据表请查看下方查询依据。'
    inline.children = []
    tokens.push(new state.Token('paragraph_open', 'p', 1), inline, new state.Token('paragraph_close', 'p', -1))
  }
  state.tokens = tokens
})
export const renderAgentMarkdown = value => {
  const sanitized = DOMPurify.sanitize(markdown.render(String(value || '')), {
  ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 's', 'blockquote', 'pre', 'code', 'ul', 'ol', 'li', 'h1', 'h2', 'h3', 'h4', 'hr', 'a'],
  ALLOWED_ATTR: ['href', 'title'],
  FORBID_ATTR: ['style', 'id', 'name'],
  ALLOW_DATA_ATTR: false
  })
  const template = document.createElement('template')
  template.innerHTML = sanitized
  // Sanitization may remove an unsupported protocol while retaining its <a>.
  // Keep the label without link styling; only controlled source cards can
  // resolve record references, never a model-invented custom URL.
  template.content.querySelectorAll('a').forEach(element => {
    if (!element.getAttribute('href')?.trim()) element.replaceWith(...element.childNodes)
  })
  // Generated scrolling code blocks must also work with a keyboard.
  template.content.querySelectorAll('pre').forEach(element => element.setAttribute('tabindex', '0'))
  return template.innerHTML
}

export const safeResult = (block, results = {}) => {
  const id = block?.result_id
  // Inline model data is never accepted as an authoritative result.
  const result = id ? results[id] : null
  if (!result || String(result.id) !== String(id) || !Array.isArray(result.columns) || !Array.isArray(result.rows)) return null
  return result
}

// Only the persisted answer's range is taken from its preview metadata. Values
// always come from the separately authenticated saved-result endpoint/cache.
export const chartRange = block => {
  const preview = block?.result
  if (!preview) return { offset: 0, limit: 500 }
  const offset = preview.offset ?? 0
  if (preview.id !== block.result_id || !Array.isArray(preview.rows) || !Array.isArray(preview.columns) || !Number.isSafeInteger(offset) || offset < 0) throw new Error('图表保存范围不完整')
  return { offset, limit: Math.min(preview.rows.length, 500), preview }
}

export const fixedChartResult = (block, result) => {
  if (!safeResult(block, { [block?.result_id]: result })) return null
  const { offset, limit, preview } = chartRange(block)
  if (preview && (preview.snapshot_at !== result.snapshot_at || JSON.stringify(preview.columns) !== JSON.stringify(result.columns))) throw new Error('图表范围与保存结果不一致')
  const pageOffset = result.offset ?? 0
  const savedCount = result.materialized_rows > 0 ? result.materialized_rows : result.has_more === false ? pageOffset + result.rows.length : result.total_rows ?? result.rows.length
  const count = preview ? limit : Math.min(limit, savedCount)
  const start = offset - pageOffset
  if (!Number.isSafeInteger(count) || count < 0 || start < 0 || start + count > result.rows.length) return null
  return { ...result, offset, rows: result.rows.slice(start, start + count).map(row => [...row]), returned_rows: count }
}

export const resultCell = value => value == null ? '—' : typeof value === 'object' ? JSON.stringify(value) : String(value)

export const chartOption = (block, result, colors = []) => {
  if (!result || !['bar', 'line', 'pie', 'scatter'].includes(block.chart_type)) return null
  const xIndex = result.columns.indexOf(block.x_column)
  const columns = (block.y_columns || []).filter(column => result.columns.includes(column))
  if (xIndex < 0 || !columns.length) return null
  const labels = result.rows.map(row => resultCell(row[xIndex]))
  const number = value => value == null || value === '' ? null : Number.isFinite(Number(value)) ? Number(value) : null
  const series = columns.map(column => {
    const index = result.columns.indexOf(column)
    return {
      name: column, type: block.chart_type,
      ...(block.chart_type === 'pie' ? { radius: ['0%', '65%'], data: result.rows.map((row, i) => ({ name: labels[i], value: number(row[index]) })) }
        : { data: result.rows.map(row => block.chart_type === 'scatter' ? [number(row[xIndex]), number(row[index])] : number(row[index])) })
    }
  })
  return {
    animation: false, color: colors,
    aria: { enabled: true, label: { description: `${block.title || '数据图表'}，下方可展开相同数据的表格` } },
    legend: { type: 'scroll', bottom: 0 },
    grid: { left: 12, right: 18, top: 24, bottom: 56, containLabel: true },
    tooltip: { trigger: block.chart_type === 'pie' || block.chart_type === 'scatter' ? 'item' : 'axis', renderMode: 'richText', confine: true },
    ...(block.chart_type === 'pie' ? {} : {
      xAxis: { type: block.chart_type === 'scatter' ? 'value' : 'category', data: labels, axisLabel: { hideOverlap: true } },
      yAxis: { type: 'value' }
    }), series
  }
}
