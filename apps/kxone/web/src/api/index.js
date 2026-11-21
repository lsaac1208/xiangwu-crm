import axios from 'axios'
import { showFailToast } from 'vant'

const api = axios.create({
  baseURL: '/api',
  timeout: 10000,
  withCredentials: true
})

let refreshPromise = null
let sessionExpired = false
const expiredSessionError = new Error('session_expired')

const invalidSessionCodes = new Set(['invalid_refresh_token', 'session_revoked', 'user_disabled', 'invalid_session'])
const isSessionInvalid = error => error === expiredSessionError || error.response?.status === 401 ||
  (error.response?.status === 403 && invalidSessionCodes.has(error.response?.data?.error))
const temporaryRefreshMessage = '登录验证暂不可用，请稍后重试；填写内容已保留。'

export const refreshSession = () => {
  if (sessionExpired) return Promise.reject(expiredSessionError)
  if (!refreshPromise) {
    refreshPromise = axios.post('/api/auth/refresh', undefined, { withCredentials: true, timeout: 10000 })
      .catch(error => {
        // Only authoritative authentication failures terminate the shared session.
        if (isSessionInvalid(error)) sessionExpired = true
        throw error
      })
      .finally(() => { refreshPromise = null })
  }
  return refreshPromise
}

export const expireSession = () => {
  sessionExpired = true
  const redirect = window.location.pathname + window.location.search + window.location.hash
  if (window.location.pathname !== '/login') {
    window.location.assign(`/login?reason=session_expired&redirect=${encodeURIComponent(redirect)}`)
  }
}

// Streams and downloads share the same refresh lock as normal API requests.
export const fetchWithSession = async (url, options = {}) => {
  let response = await fetch(url, { ...options, credentials: 'same-origin' })
  if (response.status === 401) {
    try {
      await refreshSession()
      response = await fetch(url, { ...options, credentials: 'same-origin' })
    } catch (error) {
      if (isSessionInvalid(error)) {
        expireSession()
        throw new Error('登录状态已失效，请重新登录', { cause: error })
      }
      throw error
    }
    if (response.status === 401) expireSession()
  }
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    const error = new Error(body.message || body.error || `请求失败（${response.status}）`)
    error.status = response.status
    throw error
  }
  return response
}

api.interceptors.response.use(
  response => {
    if (response.config?.url === '/auth/login' && (!response.data?.code || response.data.code === 0)) sessionExpired = false
    const body = response.data
    if (body && typeof body === 'object' && 'code' in body) {
      if (body.code === 0) return body.data
      return Promise.reject(new Error(body.message || '请求失败'))
    }
    return body
  },
  async error => {
    const request = error.config || {}
    const isAuthEndpoint = request.url?.includes('/auth/')
    if (error.response?.status === 401 && !isAuthEndpoint) {
      if (!request.__refreshRetried) {
        request.__refreshRetried = true
        try {
          await refreshSession()
          return api(request)
        } catch (refreshError) {
          if (isSessionInvalid(refreshError)) expireSession()
          else showFailToast(temporaryRefreshMessage)
          return Promise.reject(refreshError)
        }
      }
      expireSession()
    }
    if (!isAuthEndpoint) {
      const code = error.response?.data?.error
      const friendlyMessages = {
        account_version_conflict: '账号资料已变化，输入已保留，请核对最新资料',
        session_revoked: '账号或安全设置已变更，请重新登录',
        lead_version_conflict: '线索资料已发生变化，请重新打开后再修改',
        invalid_lead_profile: '请检查客户资料与联系方式',
        forbidden: '当前账号无权执行此操作',
        group_robot_health_failed: '暂时无法获取通知投递状态，请稍后重试',
        group_robot_health_unavailable: '当前暂时无法获取通知投递状态'
      }
      const message = friendlyMessages[code] || error.response?.data?.message || code || error.message || '网络错误'
      showFailToast(message)
    }
    return Promise.reject(error)
  }
)

export default api
