import api from './index'

export const createAppointment = (data) => api.post('/appointments', data)
export const getAppointments = (params) => api.get('/appointments', { params })
export const getAppointment = (id) => api.get(`/appointments/${id}`)
export const assignAppointmentCloser = (id, data) => api.post(`/appointments/${id}/assign-closer`, data)
export const rescheduleAppointment = (id, data) => api.post(`/appointments/${id}/reschedule`, data)
export const cancelAppointment = (id, data) => api.post(`/appointments/${id}/cancel`, data)
export const noShowAppointment = (id, data) => api.post(`/appointments/${id}/no-show`, data)
export const confirmAppointmentArrival = (id, data) => api.post(`/appointments/${id}/confirm-arrival`, data)
export const detectAppointmentConflicts = (params) => api.get('/appointments/conflicts', { params })
export const getCloserAvailability = (params) => api.get('/closer-availability', { params })
export const createCloserAvailability = (data) => api.post('/closer-availability', data)
export const deleteCloserAvailability = (id) => api.delete(`/closer-availability/${id}`)
