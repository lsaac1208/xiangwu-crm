import api from './index'

export const getLeadListPreferences = () => api.get('/me/lead-list-preferences')
export const updateLeadListPreferences = data => api.put('/me/lead-list-preferences', data)
export const getLeadViews = () => api.get('/me/lead-views')
export const createLeadView = data => api.post('/me/lead-views', data)
export const updateLeadView = (id, data) => api.patch(`/me/lead-views/${id}`, data)
export const deleteLeadView = id => api.delete(`/me/lead-views/${id}`)
