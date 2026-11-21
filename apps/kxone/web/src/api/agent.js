import api, { fetchWithSession } from './index'
import { consumeAgentEvents } from '@/utils/agentStream'

export const agentAPI = {
  conversations: (search = '', page = 1) => api.get('/agent/conversations', { params: { search, page, page_size: 50 } }),
  createConversation: () => api.post('/agent/conversations', {}),
  conversation: (id, before) => api.get(`/agent/conversations/${encodeURIComponent(id)}`, { params: before ? { before } : {} }),
  renameConversation: (id, title) => api.patch(`/agent/conversations/${encodeURIComponent(id)}`, { title }),
  deleteConversation: id => api.delete(`/agent/conversations/${encodeURIComponent(id)}`),
  createRun: (id, message, requestId) => api.post(`/agent/conversations/${encodeURIComponent(id)}/runs`, { message, request_id: requestId }),
  metrics: () => api.get('/agent/metrics'),
  createMetricRun: (id, metricId, params, requestId) => api.post(`/agent/conversations/${encodeURIComponent(id)}/metric-runs`, { metric_id: metricId, params, request_id: requestId }),
  run: id => api.get(`/agent/runs/${encodeURIComponent(id)}`),
  cancelRun: id => api.post(`/agent/runs/${encodeURIComponent(id)}/cancel`),
  result: (id, offset = 0, limit = 200) => api.get(`/agent/results/${encodeURIComponent(id)}`, { params: { offset, limit } }),
  settings: () => api.get('/agent/settings'),
  saveSettings: values => api.put('/agent/settings', values),
  testSettings: values => api.post('/agent/settings/test', values, { timeout: 45000 }),
  preferences: () => api.get('/agent/preferences'),
  savePreferences: content => api.put('/agent/preferences', { content }),
  deletePreferences: () => api.delete('/agent/preferences'),
  rules: () => api.get('/agent/rules'),
  proposeRule: content => api.post('/agent/rules', { content }),
  publishRule: id => api.post(`/agent/rules/${encodeURIComponent(id)}/publish`),
  rollbackRule: id => api.post(`/agent/rules/${encodeURIComponent(id)}/rollback`),
  stream: async (id, after, signal, onEvent) => {
    const response = await fetchWithSession(`/api/agent/runs/${encodeURIComponent(id)}/events?after=${after}`, { signal, headers: { Accept: 'text/event-stream' } })
    return consumeAgentEvents(response.body, signal, onEvent)
  },
  exportRun: async (id, format) => {
    const exported = await api.post(`/agent/runs/${encodeURIComponent(id)}/exports`, { format }, { timeout: 60000 })
    const response = await fetchWithSession(`/api/agent/exports/${encodeURIComponent(exported.id)}/download`)
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `CRM分析-${new Date().toISOString().slice(0, 10)}.${format}`
    anchor.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
}
