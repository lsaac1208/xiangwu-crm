import api from './index'

export const getLeadList = (params) => {
  return api.get('/leads', { params })
}

export const createLead = (data, requestKey) => {
  return api.post('/leads', data, { headers: { 'Idempotency-Key': requestKey } })
}

export const getLeadDetail = (id) => {
  return api.get(`/leads/${id}`)
}

export const deleteLead = (id, data) => api.delete(`/leads/${id}`, { data })

export const updateLeadProfile = (id, data) => api.patch(`/leads/${id}/profile`, data)

export const assignLead = (id, data) => {
  return api.post(`/leads/${id}/assign`, data)
}

export const claimLead = (id) => {
  return api.post(`/leads/${id}/claim`)
}

export const selfClaimLead = (id) => api.post(`/leads/${id}/self-claim`)

export const getRecommendSales = (id) => {
  return api.get(`/leads/${id}/recommend-sales`)
}

export const autoAssignLead = (id) => {
  return api.post(`/leads/${id}/auto-assign`)
}

export const reassignLead = (id, data) => {
  return api.post(`/leads/${id}/reassign`, data)
}

export const reassignNegotiationCloser = (id, data) => {
  return api.post(`/leads/${id}/reassign-closer`, data)
}

export const batchAssignLeads = data => api.post('/leads/batch-assign', data)
export const batchReassignLeads = data => api.post('/leads/batch-reassign', data)

export const convertLead = (id, data) => {
  return api.post(`/leads/${id}/convert`, data)
}

export const closeLead = (id, data) => {
  return api.post(`/leads/${id}/close`, data)
}

export const getPublicPool = (params) => api.get('/public-pool', { params })
export const claimPublicPoolLead = (id) => api.post(`/public-pool/${id}/claim`)
export const enterPublicPool = (id, data) => api.post(`/public-pool/${id}/enter`, data)

export const updateLeadDeal = (id, data) => api.patch(`/leads/${id}/deal`, data)
export const restoreInvalidLead = (id, data) => api.post(`/leads/${id}/restore-invalid`, data)
