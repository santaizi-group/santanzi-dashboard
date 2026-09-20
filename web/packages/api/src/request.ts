import Axios, { type AxiosRequestConfig } from 'axios'

let csrfToken = ''

export interface SantaiziAPIError extends Error {
  code?: string
  status?: number
  traceId?: string
  detail?: string
  fields?: Record<string, string[]>
}

export function setCSRFToken(token: string) {
  csrfToken = token
}

export const http = Axios.create({
  baseURL: '/',
  withCredentials: true,
  headers: { Accept: 'application/json' },
})

http.interceptors.request.use((config) => {
  const method = String(config.method || 'get').toUpperCase()
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method) && csrfToken) {
    config.headers.set('X-CSRF-Token', csrfToken)
  }
  return config
})

http.interceptors.response.use(
  (response) => response,
  (error) => {
    const problem = error?.response?.data
    if (problem && typeof problem === 'object') {
      const blocked = Number(problem.error_code) === 1010 || /Error 1010/i.test(String(problem.title || ''))
      const code = blocked ? 'cloudflare_blocked' : (problem.code ?? (problem.error_code != null ? String(problem.error_code) : undefined))
      error.message = problem.detail || problem.description || problem.title || problem.message || error.message
      error.detail = problem.detail || problem.description || error.message
      if (code != null && code !== '') error.code = String(code)
      error.status = problem.status
      error.traceId = problem.trace_id
      error.fields = problem.errors
    }
    return Promise.reject(error)
  },
)

export function santaiziRequest<T>(config: AxiosRequestConfig): Promise<T> {
  return http.request<T>(config).then((response) => response.data)
}
