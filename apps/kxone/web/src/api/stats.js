import api from './index'

export const getSLAStats = (params) => {
  return api.get('/stats/sla', { params })
}

export const getDashboardStats = (params) => {
  return api.get('/stats/dashboard', { params })
}

export const getManagementStats = params => api.get('/stats/management', { params })
export const getFollowupSupervision = () => api.get('/stats/supervision')
