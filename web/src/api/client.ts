const TOKEN_KEY = 'mg_token'

export function getToken(): string | null {
  return window.localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  window.localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken(): void {
  window.localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export interface Page<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface Channel {
  id: number
  name: string
  type: string
  base_url: string
  models: string[]
  model_mapping: Record<string, string>
  priority: number
  weight: number
  status: number
  api_key_masked: string
  created_at: string
  updated_at: string
}

export interface ChannelInput {
  name: string
  base_url: string
  api_key?: string
  models: string[]
  model_mapping?: Record<string, string>
  priority: number
  weight: number
  status: number
}

export interface ChannelTestResult {
  ok: boolean
  status_code: number
  latency_ms: number
  models: string[]
  error: string
}

export interface Token {
  id: number
  name: string
  key_prefix: string
  quota: number
  used_quota: number
  status: number
  created_at: string
  updated_at: string
  last_used_at: string
  key?: string
}

export interface LogEntry {
  id: number
  token_name: string
  channel_name: string
  model: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  status_code: number
  latency_ms: number
  stream: boolean
  error: string
  created_at: string
}

export interface DailyPoint {
  date: string
  requests: number
  tokens: number
}

export interface DashboardData {
  channels: { total: number; enabled: number }
  tokens: { total: number; enabled: number }
  requests: { total: number; today: number }
  usage: { total_tokens: number; today_tokens: number }
  daily: DailyPoint[]
  recent_logs: LogEntry[]
}

interface RequestOptions {
  method?: string
  body?: unknown
}

function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') {
      search.set(key, String(value))
    }
  }
  const text = search.toString()
  return text ? `?${text}` : ''
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {}
  const token = getToken()
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  let response: Response
  try {
    response = await fetch(path, {
      method: options.method ?? 'GET',
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    })
  } catch {
    throw new ApiError(0, '无法连接服务器，请检查后端是否启动')
  }
  if (response.status === 401 && path !== '/api/auth/login') {
    clearToken()
    if (!window.location.pathname.startsWith('/login')) {
      window.location.assign('/login')
    }
    throw new ApiError(401, '登录已失效，请重新登录')
  }
  if (!response.ok) {
    let message = `请求失败 (${response.status})`
    try {
      const data = (await response.json()) as { error?: string }
      if (data.error) {
        message = data.error
      }
    } catch {
      // keep the status-based message
    }
    throw new ApiError(response.status, message)
  }
  if (response.status === 204) {
    return undefined as T
  }
  return (await response.json()) as T
}

export const api = {
  login(username: string, password: string) {
    return request<{ token: string }>('/api/auth/login', {
      method: 'POST',
      body: { username, password },
    })
  },
  me() {
    return request<{ username: string }>('/api/auth/me')
  },
  changePassword(oldPassword: string, newPassword: string) {
    return request<{ ok: boolean }>('/api/auth/password', {
      method: 'PUT',
      body: { old_password: oldPassword, new_password: newPassword },
    })
  },
  dashboard() {
    return request<DashboardData>('/api/dashboard')
  },
  models() {
    return request<{ models: string[] }>('/api/models')
  },
  channels: {
    list(params: { page?: number; page_size?: number; search?: string }) {
      return request<Page<Channel>>(`/api/channels${query(params)}`)
    },
    create(payload: ChannelInput) {
      return request<Channel>('/api/channels', { method: 'POST', body: payload })
    },
    update(id: number, payload: Partial<ChannelInput>) {
      return request<Channel>(`/api/channels/${id}`, { method: 'PUT', body: payload })
    },
    test(id: number) {
      return request<ChannelTestResult>(`/api/channels/${id}/test`, { method: 'POST' })
    },
    remove(id: number) {
      return request<void>(`/api/channels/${id}`, { method: 'DELETE' })
    },
  },
  tokens: {
    list(params: { page?: number; page_size?: number }) {
      return request<Page<Token>>(`/api/tokens${query(params)}`)
    },
    create(payload: { name: string; quota: number }) {
      return request<Token>('/api/tokens', { method: 'POST', body: payload })
    },
    update(id: number, payload: { name?: string; quota?: number; status?: number }) {
      return request<Token>(`/api/tokens/${id}`, { method: 'PUT', body: payload })
    },
    remove(id: number) {
      return request<void>(`/api/tokens/${id}`, { method: 'DELETE' })
    },
  },
  logs: {
    list(params: { page?: number; page_size?: number; model?: string; status?: string }) {
      return request<Page<LogEntry>>(`/api/logs${query(params)}`)
    },
  },
}
