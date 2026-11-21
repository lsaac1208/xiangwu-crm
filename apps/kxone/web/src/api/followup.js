import api from './index'

export const createFollowup = (leadId, data, requestKey) => {
  return api.post(`/leads/${leadId}/followups`, data, { headers: { 'Idempotency-Key': requestKey } })
}
