import api from './index'

export const getSalesUsers = () => {
  return api.get('/users/inviters')
}

export const getCloserUsers = () => api.get('/users/closers')
export const getLeadOwnerFilters = () => api.get('/users/lead-owner-filters')
